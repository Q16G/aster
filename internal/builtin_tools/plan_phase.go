package builtin_tools

import (
	"fmt"
	"strings"
)

// CloneAnalysisTopics 深拷贝 phase 清单，供快照隔离与状态写入使用。
func CloneAnalysisTopics(in []*AnalysisTopic) []*AnalysisTopic {
	if len(in) == 0 {
		return nil
	}
	out := make([]*AnalysisTopic, 0, len(in))
	for _, phase := range in {
		if phase == nil {
			continue
		}
		clone := *phase
		clone.DependsOn = CloneStringSlice(phase.DependsOn)
		out = append(out, &clone)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CloneTopicAssessments 深拷贝 phase 评估清单。
func CloneTopicAssessments(in []*TopicAssessment) []*TopicAssessment {
	if len(in) == 0 {
		return nil
	}
	out := make([]*TopicAssessment, 0, len(in))
	for _, a := range in {
		if a == nil {
			continue
		}
		clone := *a
		clone.IncompleteItems = CloneStringSlice(a.IncompleteItems)
		clone.DepthGaps = CloneStringSlice(a.DepthGaps)
		clone.NewSurfaces = CloneStringSlice(a.NewSurfaces)
		out = append(out, &clone)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeAnalysisTopics 归一化 planner 提交的 phase 清单：trim、id 必填且唯一、
// status 合法（缺省 pending）、depends_on 引用闭包且无环。forbidSynthetic 为 true
// 时拒绝 planner 主动使用保留 id（SyntheticTopicID），防止与 runtime 兜底挂靠碰撞。
func NormalizeAnalysisTopics(phases []*AnalysisTopic, forbidSynthetic bool) ([]*AnalysisTopic, error) {
	if len(phases) == 0 {
		return nil, nil
	}

	out := make([]*AnalysisTopic, 0, len(phases))
	byID := make(map[string]*AnalysisTopic, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		id := canonicalizePlanIDToken(phase.ID)
		if id == "" {
			return nil, fmt.Errorf("phases[].id is required")
		}
		if forbidSynthetic && id == SyntheticTopicID {
			return nil, fmt.Errorf("phase id %q is reserved for runtime", SyntheticTopicID)
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("duplicate phase id %q", id)
		}

		status := AnalysisTopicStatus(strings.TrimSpace(string(phase.Status)))
		switch status {
		case "":
			status = AnalysisTopicPending
		case AnalysisTopicPending, AnalysisTopicCompleted, AnalysisTopicBlocked:
		default:
			return nil, fmt.Errorf("invalid phase status %q for phase %q", status, id)
		}

		norm := &AnalysisTopic{
			ID:     id,
			Name:   strings.TrimSpace(phase.Name),
			Status: status,
		}
		for _, dep := range phase.DependsOn {
			dep = canonicalizePlanIDToken(dep)
			if dep == "" || dep == id {
				continue
			}
			norm.DependsOn = append(norm.DependsOn, dep)
		}
		out = append(out, norm)
		byID[id] = norm
	}
	if len(out) == 0 {
		return nil, nil
	}

	for _, phase := range out {
		for _, dep := range phase.DependsOn {
			if _, ok := byID[dep]; !ok {
				return nil, fmt.Errorf("unknown dependency %q for phase %q", dep, phase.ID)
			}
		}
	}
	if err := validateTopicDependencyGraph(out, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func validateTopicDependencyGraph(phases []*AnalysisTopic, byID map[string]*AnalysisTopic) error {
	visiting := make(map[string]bool, len(phases))
	visited := make(map[string]bool, len(phases))
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("phase dependencies contain cycle at %s", id)
		}
		visiting[id] = true
		if phase := byID[id]; phase != nil {
			for _, dep := range phase.DependsOn {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for _, phase := range phases {
		if err := visit(phase.ID); err != nil {
			return err
		}
	}
	return nil
}

func planTopicsByID(phases []*AnalysisTopic) map[string]*AnalysisTopic {
	out := make(map[string]*AnalysisTopic, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		if id := strings.TrimSpace(phase.ID); id != "" {
			out[id] = phase
		}
	}
	return out
}

// phaseUnlocked 判断 phase 是否已解锁：depends_on 引用的 phase 全部 terminal
// （completed/blocked 均视同 terminal，偏放行）。引用悬空的依赖视为已满足。
func phaseUnlocked(phase *AnalysisTopic, byID map[string]*AnalysisTopic) bool {
	if phase == nil {
		return true
	}
	for _, dep := range phase.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if ref, ok := byID[dep]; ok && !ref.Terminal() {
			return false
		}
	}
	return true
}

// planItemTopicAdmits 判断 step 的 phase 门是否放行：
//   - phases 为空（无 phase 上下文的调用方）或 step 未挂 phase / 挂靠悬空 → 放行
//     （悬空由 submit 时校验拦截，runtime 不二次卡死）；
//   - phase 已 terminal → 不放行（completed lane 不再释放 step；blocked lane 的
//     pending step 由 SkipStepsOfBlockedTopics 收敛）；
//   - 其余按 phase 解锁判定。
func planItemTopicAdmits(item *PlanItem, byID map[string]*AnalysisTopic) bool {
	if item == nil {
		return false
	}
	if len(byID) == 0 {
		return true
	}
	topicID := strings.TrimSpace(item.TopicID)
	if topicID == "" {
		return true
	}
	phase, ok := byID[topicID]
	if !ok {
		return true
	}
	if phase.Terminal() {
		return false
	}
	return phaseUnlocked(phase, byID)
}

// ReadyFrontierPlanStepIDs 返回当前 frontier：所有 step depends_on 已满足、
// 且所属 phase 已解锁的 pending step ID，顺序按 plan 原序。
// 是 ReadyRunnablePlanStepIDs 的超集判定（多一道 phase 门）。
func ReadyFrontierPlanStepIDs(plan []*PlanItem, phases []*AnalysisTopic) []string {
	if len(plan) == 0 {
		return nil
	}
	byID := planTopicsByID(phases)
	completed := planReadyDependencies(plan)
	out := make([]string, 0, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		if item.Status != PlanStepPending {
			continue
		}
		if !planItemDependenciesSatisfied(item, completed) {
			continue
		}
		if !planItemTopicAdmits(item, byID) {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NextFrontierPlanStepID 返回 frontier 中的首个 step ID（主路径用），无 ready 项返回空串。
func NextFrontierPlanStepID(plan []*PlanItem, phases []*AnalysisTopic) string {
	if ids := ReadyFrontierPlanStepIDs(plan, phases); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ReadyFrontierPlanStepIDsScoped 在全局 frontier 基础上再按 topicID 过滤（Part C：per-topic
// 局部 replan 当回只释放本 topic 的就绪 step）。topicID=="" 时等价全局 ReadyFrontierPlanStepIDs。
// topic→steps 的推导全部由代码完成（按 PlanItem.TopicID 过滤），不经 AI。
func ReadyFrontierPlanStepIDsScoped(plan []*PlanItem, phases []*AnalysisTopic, topicID string) []string {
	ids := ReadyFrontierPlanStepIDs(plan, phases)
	topicID = strings.TrimSpace(topicID)
	if topicID == "" || len(ids) == 0 {
		return ids
	}
	topicByStep := make(map[string]string, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		topicByStep[strings.TrimSpace(item.ID)] = strings.TrimSpace(item.TopicID)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if topicByStep[id] == topicID {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NextFrontierPlanStepIDScoped 返回 topic 收窄 frontier 中的首个 step ID，无则空串。
func NextFrontierPlanStepIDScoped(plan []*PlanItem, phases []*AnalysisTopic, topicID string) string {
	if ids := ReadyFrontierPlanStepIDsScoped(plan, phases, topicID); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ActiveTopics 返回本轮处于活跃态的 phase：未 terminal 且已解锁（depends_on 全 terminal）。
// step_replan 对这批 phase 逐个输出 phase_assessments。
func ActiveTopics(phases []*AnalysisTopic) []*AnalysisTopic {
	if len(phases) == 0 {
		return nil
	}
	byID := planTopicsByID(phases)
	var out []*AnalysisTopic
	for _, phase := range phases {
		if phase == nil || phase.Terminal() {
			continue
		}
		if phaseUnlocked(phase, byID) {
			out = append(out, phase)
		}
	}
	return out
}

// TopicHasPendingStep 判断某 phase 下是否仍有 pending step（供 completed-with-pending 守卫）。
func TopicHasPendingStep(plan []*PlanItem, topicID string) bool {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return false
	}
	for _, item := range plan {
		if item == nil {
			continue
		}
		if item.Status == PlanStepPending && strings.TrimSpace(item.TopicID) == topicID {
			return true
		}
	}
	return false
}

// TopicQuiesced 判断某 phase（topic）是否到达「局部静默点」：已释放 ≥1 个 step 且其全部 step
// 均为 terminal（completed/failed/skipped，无 pending、无 in_progress）。0-step phase（G_topic
// 占位、尚未释放 step）返回 false——由 planner 先释放首批 step，不触发空 review（0-step 守卫）。
// 第四阶段将以此把全局 frontier barrier 改为每 topic 局部触发 step_replan（当前仍未接线）。
func TopicQuiesced(plan []*PlanItem, topicID string) bool {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return false
	}
	count := 0
	for _, item := range plan {
		if item == nil || strings.TrimSpace(item.TopicID) != topicID {
			continue
		}
		count++
		switch item.Status {
		case PlanStepCompleted, PlanStepFailed, PlanStepSkipped:
		default:
			return false // pending / in_progress → 未静默
		}
	}
	return count > 0
}

// QuiescedActiveTopics 返回本轮已达局部静默点、可触发单 topic review 的 active phase 集：
// 在 ActiveTopics（非终态 + 前置 topic 全终态）基础上，进一步筛出 TopicQuiesced 为真者。
// 供第四阶段路由把「等全局 frontier 空」改为「某 topic 静默即 review 该 topic」。
func QuiescedActiveTopics(plan []*PlanItem, phases []*AnalysisTopic) []*AnalysisTopic {
	active := ActiveTopics(phases)
	if len(active) == 0 {
		return nil
	}
	var out []*AnalysisTopic
	for _, phase := range active {
		if phase != nil && TopicQuiesced(plan, phase.ID) {
			out = append(out, phase)
		}
	}
	return out
}

// AllTopicsSettled 判断所有 phase 是否已收束（completed/blocked）。
// 空清单视为已收束（无 phase 上下文时不阻塞收尾）。
func AllTopicsSettled(phases []*AnalysisTopic) bool {
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		if !phase.Terminal() {
			return false
		}
	}
	return true
}

// SynthesizeTopicsIfMissing 保证 plan 与 phases 的挂靠闭合：
//   - plan 为空时原样返回；
//   - phases 为空时合成单个 synthetic phase 并把全部 item 挂上去；
//   - item 的 phase_id 缺失或悬空时挂到 synthetic phase（不存在则追加）。
//
// 原地修正 plan item 的 TopicID；返回的 phases 为拷贝后的新切片，不共享入参底座。
// fallbackName 用作 synthetic phase 的展示名（取首行），为空时退化为 id。
func SynthesizeTopicsIfMissing(plan []*PlanItem, phases []*AnalysisTopic, fallbackName string) []*AnalysisTopic {
	out := CloneAnalysisTopics(phases)
	if len(plan) == 0 {
		return out
	}

	byID := planTopicsByID(out)
	var synthetic *AnalysisTopic
	if existing, ok := byID[SyntheticTopicID]; ok {
		synthetic = existing
	}

	ensureSynthetic := func() *AnalysisTopic {
		if synthetic != nil {
			return synthetic
		}
		synthetic = &AnalysisTopic{
			ID:     SyntheticTopicID,
			Name:   syntheticTopicName(fallbackName),
			Status: AnalysisTopicPending,
		}
		out = append(out, synthetic)
		byID[SyntheticTopicID] = synthetic
		return synthetic
	}

	for _, item := range plan {
		if item == nil {
			continue
		}
		topicID := strings.TrimSpace(item.TopicID)
		if topicID != "" {
			if _, ok := byID[topicID]; ok {
				item.TopicID = topicID
				continue
			}
		}
		item.TopicID = ensureSynthetic().ID
	}
	return out
}

func syntheticTopicName(fallbackName string) string {
	name := strings.TrimSpace(fallbackName)
	if idx := strings.IndexByte(name, '\n'); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	if name == "" {
		return SyntheticTopicID
	}
	return name
}

// SkipStepsOfBlockedTopics 把 blocked phase 下的 pending step 标记为 skipped，
// 并经 PropagateSkippedPlanSteps 把跨 phase 的下游依赖一并传递收敛。
// 返回是否有 step 状态被改变。
func SkipStepsOfBlockedTopics(plan []*PlanItem, phases []*AnalysisTopic) (changed bool) {
	if len(plan) == 0 || len(phases) == 0 {
		return false
	}
	blocked := make(map[string]struct{}, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		if phase.Status == AnalysisTopicBlocked {
			if id := strings.TrimSpace(phase.ID); id != "" {
				blocked[id] = struct{}{}
			}
		}
	}
	if len(blocked) == 0 {
		return false
	}
	for _, item := range plan {
		if item == nil || item.Status != PlanStepPending {
			continue
		}
		if _, ok := blocked[strings.TrimSpace(item.TopicID)]; ok {
			item.Status = PlanStepSkipped
			changed = true
		}
	}
	if PropagateSkippedPlanSteps(plan) {
		changed = true
	}
	return changed
}

// SkipStepsOfBlockedTopicScoped 是 SkipStepsOfBlockedTopics 的 per-topic 局部 review 裁剪版：
// 只把「属 topicID 且该 phase 为 blocked」的 pending step 收敛为 skipped，且【不做】跨 phase
// 下游传播（不调 PropagateSkippedPlanSteps）。跨 topic 下游 skip 的正确时机是全局 reducer
// （barrier 已 await 全部 peer、全盘状态定型），此处延后不丢失、只避免局部 review 越权替
// 正在写活盘的他 topic 决策（M2）。返回是否有 step 状态被改变。
func SkipStepsOfBlockedTopicScoped(plan []*PlanItem, phases []*AnalysisTopic, topicID string) (changed bool) {
	topicID = strings.TrimSpace(topicID)
	if len(plan) == 0 || topicID == "" {
		return false
	}
	blocked := false
	for _, phase := range phases {
		if phase != nil && phase.Status == AnalysisTopicBlocked && strings.TrimSpace(phase.ID) == topicID {
			blocked = true
			break
		}
	}
	if !blocked {
		return false
	}
	for _, item := range plan {
		if item == nil || item.Status != PlanStepPending {
			continue
		}
		if strings.TrimSpace(item.TopicID) == topicID {
			item.Status = PlanStepSkipped
			changed = true
		}
	}
	return changed
}
