package builtin_tools

import (
	"fmt"
	"strings"
)

// ClonePlanPhases 深拷贝 phase 清单，供快照隔离与状态写入使用。
func ClonePlanPhases(in []*PlanPhase) []*PlanPhase {
	if len(in) == 0 {
		return nil
	}
	out := make([]*PlanPhase, 0, len(in))
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

// ClonePhaseAssessments 深拷贝 phase 评估清单。
func ClonePhaseAssessments(in []*PhaseAssessment) []*PhaseAssessment {
	if len(in) == 0 {
		return nil
	}
	out := make([]*PhaseAssessment, 0, len(in))
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

// NormalizePlanPhases 归一化 planner 提交的 phase 清单：trim、id 必填且唯一、
// status 合法（缺省 pending）、depends_on 引用闭包且无环。forbidSynthetic 为 true
// 时拒绝 planner 主动使用保留 id（SyntheticPhaseID），防止与 runtime 兜底挂靠碰撞。
func NormalizePlanPhases(phases []*PlanPhase, forbidSynthetic bool) ([]*PlanPhase, error) {
	if len(phases) == 0 {
		return nil, nil
	}

	out := make([]*PlanPhase, 0, len(phases))
	byID := make(map[string]*PlanPhase, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		id := canonicalizePlanIDToken(phase.ID)
		if id == "" {
			return nil, fmt.Errorf("phases[].id is required")
		}
		if forbidSynthetic && id == SyntheticPhaseID {
			return nil, fmt.Errorf("phase id %q is reserved for runtime", SyntheticPhaseID)
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("duplicate phase id %q", id)
		}

		status := PlanPhaseStatus(strings.TrimSpace(string(phase.Status)))
		switch status {
		case "":
			status = PlanPhasePending
		case PlanPhasePending, PlanPhaseCompleted, PlanPhaseBlocked:
		default:
			return nil, fmt.Errorf("invalid phase status %q for phase %q", status, id)
		}

		norm := &PlanPhase{
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
	if err := validatePhaseDependencyGraph(out, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func validatePhaseDependencyGraph(phases []*PlanPhase, byID map[string]*PlanPhase) error {
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

func planPhasesByID(phases []*PlanPhase) map[string]*PlanPhase {
	out := make(map[string]*PlanPhase, len(phases))
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
func phaseUnlocked(phase *PlanPhase, byID map[string]*PlanPhase) bool {
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

// planItemPhaseAdmits 判断 step 的 phase 门是否放行：
//   - phases 为空（无 phase 上下文的调用方）或 step 未挂 phase / 挂靠悬空 → 放行
//     （悬空由 submit 时校验拦截，runtime 不二次卡死）；
//   - phase 已 terminal → 不放行（completed lane 不再释放 step；blocked lane 的
//     pending step 由 SkipStepsOfBlockedPhases 收敛）；
//   - 其余按 phase 解锁判定。
func planItemPhaseAdmits(item *PlanItem, byID map[string]*PlanPhase) bool {
	if item == nil {
		return false
	}
	if len(byID) == 0 {
		return true
	}
	phaseID := strings.TrimSpace(item.PhaseID)
	if phaseID == "" {
		return true
	}
	phase, ok := byID[phaseID]
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
func ReadyFrontierPlanStepIDs(plan []*PlanItem, phases []*PlanPhase) []string {
	if len(plan) == 0 {
		return nil
	}
	byID := planPhasesByID(phases)
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
		if !planItemPhaseAdmits(item, byID) {
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
func NextFrontierPlanStepID(plan []*PlanItem, phases []*PlanPhase) string {
	if ids := ReadyFrontierPlanStepIDs(plan, phases); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ActivePhases 返回本轮处于活跃态的 phase：未 terminal 且已解锁（depends_on 全 terminal）。
// step_replan 对这批 phase 逐个输出 phase_assessments。
func ActivePhases(phases []*PlanPhase) []*PlanPhase {
	if len(phases) == 0 {
		return nil
	}
	byID := planPhasesByID(phases)
	var out []*PlanPhase
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

// PhaseHasPendingStep 判断某 phase 下是否仍有 pending step（供 completed-with-pending 守卫）。
func PhaseHasPendingStep(plan []*PlanItem, phaseID string) bool {
	phaseID = strings.TrimSpace(phaseID)
	if phaseID == "" {
		return false
	}
	for _, item := range plan {
		if item == nil {
			continue
		}
		if item.Status == PlanStepPending && strings.TrimSpace(item.PhaseID) == phaseID {
			return true
		}
	}
	return false
}

// AllPhasesSettled 判断所有 phase 是否已收束（completed/blocked）。
// 空清单视为已收束（无 phase 上下文时不阻塞收尾）。
func AllPhasesSettled(phases []*PlanPhase) bool {
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

// SynthesizePhasesIfMissing 保证 plan 与 phases 的挂靠闭合：
//   - plan 为空时原样返回；
//   - phases 为空时合成单个 synthetic phase 并把全部 item 挂上去；
//   - item 的 phase_id 缺失或悬空时挂到 synthetic phase（不存在则追加）。
//
// 原地修正 plan item 的 PhaseID；返回的 phases 为拷贝后的新切片，不共享入参底座。
// fallbackName 用作 synthetic phase 的展示名（取首行），为空时退化为 id。
func SynthesizePhasesIfMissing(plan []*PlanItem, phases []*PlanPhase, fallbackName string) []*PlanPhase {
	out := ClonePlanPhases(phases)
	if len(plan) == 0 {
		return out
	}

	byID := planPhasesByID(out)
	var synthetic *PlanPhase
	if existing, ok := byID[SyntheticPhaseID]; ok {
		synthetic = existing
	}

	ensureSynthetic := func() *PlanPhase {
		if synthetic != nil {
			return synthetic
		}
		synthetic = &PlanPhase{
			ID:     SyntheticPhaseID,
			Name:   syntheticPhaseName(fallbackName),
			Status: PlanPhasePending,
		}
		out = append(out, synthetic)
		byID[SyntheticPhaseID] = synthetic
		return synthetic
	}

	for _, item := range plan {
		if item == nil {
			continue
		}
		phaseID := strings.TrimSpace(item.PhaseID)
		if phaseID != "" {
			if _, ok := byID[phaseID]; ok {
				item.PhaseID = phaseID
				continue
			}
		}
		item.PhaseID = ensureSynthetic().ID
	}
	return out
}

func syntheticPhaseName(fallbackName string) string {
	name := strings.TrimSpace(fallbackName)
	if idx := strings.IndexByte(name, '\n'); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	if name == "" {
		return SyntheticPhaseID
	}
	return name
}

// SkipStepsOfBlockedPhases 把 blocked phase 下的 pending step 标记为 skipped，
// 并经 PropagateSkippedPlanSteps 把跨 phase 的下游依赖一并传递收敛。
// 返回是否有 step 状态被改变。
func SkipStepsOfBlockedPhases(plan []*PlanItem, phases []*PlanPhase) (changed bool) {
	if len(plan) == 0 || len(phases) == 0 {
		return false
	}
	blocked := make(map[string]struct{}, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		if phase.Status == PlanPhaseBlocked {
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
		if _, ok := blocked[strings.TrimSpace(item.PhaseID)]; ok {
			item.Status = PlanStepSkipped
			changed = true
		}
	}
	if PropagateSkippedPlanSteps(plan) {
		changed = true
	}
	return changed
}
