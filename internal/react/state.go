package react

import (
	"aster/internal/builtin_tools"
	"strings"
	"sync"
	"time"
)

// StateTracker 状态追踪器
type StateTracker struct {
	mu    sync.RWMutex
	state *builtin_tools.StateSnapshot
}

// NewStateTracker 创建状态追踪器
func NewStateTracker() *StateTracker {
	return &StateTracker{
		state: &builtin_tools.StateSnapshot{
			Phase:     builtin_tools.AgentPhasePlan,
			Status:    builtin_tools.TaskStatusPreparing,
			UpdatedAt: time.Now(),
		},
	}
}

// Snapshot 返回当前状态的隔离快照，调用方的修改不会影响 StateTracker 内部状态。
func (t *StateTracker) Snapshot() builtin_tools.StateSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return isolateSnapshot(t.state)
}

// isolateSnapshot 对指针/切片字段做深拷贝，返回独立于原始对象的快照。
func isolateSnapshot(src *builtin_tools.StateSnapshot) builtin_tools.StateSnapshot {
	out := *src

	if len(src.InputTimeline) > 0 {
		out.InputTimeline = make([]*builtin_tools.TimelineInput, len(src.InputTimeline))
		for i, item := range src.InputTimeline {
			if item != nil {
				clone := *item
				out.InputTimeline[i] = &clone
			}
		}
	}

	if len(src.Plan) > 0 {
		out.Plan = make([]*builtin_tools.PlanItem, len(src.Plan))
		for i, item := range src.Plan {
			if item != nil {
				clone := *item
				if len(item.DependsOn) > 0 {
					clone.DependsOn = make([]string, len(item.DependsOn))
					copy(clone.DependsOn, item.DependsOn)
				}
				clone.ResolvedDependsOn = nil
				out.Plan[i] = &clone
			}
		}
		builtin_tools.HydratePlanRelations(out.Plan)
	}

	if len(src.StepOutcomes) > 0 {
		out.StepOutcomes = make([]*builtin_tools.StepOutcome, len(src.StepOutcomes))
		for i, item := range src.StepOutcomes {
			if item != nil {
				clone := *item
				clone.References = copyStrings(item.References)
				clone.KeyFacts = copyStrings(item.KeyFacts)
				clone.OpenQuestions = copyStrings(item.OpenQuestions)
				out.StepOutcomes[i] = &clone
			}
		}
	}

	if src.FinalAnswer != nil {
		clone := *src.FinalAnswer
		clone.References = copyStrings(src.FinalAnswer.References)
		out.FinalAnswer = &clone
	}
	out.ReplanContext = builtin_tools.CloneReplanContext(src.ReplanContext)
	out.ExternalInterrupt = builtin_tools.CloneExternalInterrupt(src.ExternalInterrupt)
	out.ActiveSkillNames = normalizeSkillNames(src.ActiveSkillNames)

	out.Warnings = copyStrings(src.Warnings)
	out.UnresolvedAxes = builtin_tools.CloneReplanAxes(src.UnresolvedAxes)

	return out
}

func copyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// normalizeReplanAxes 规范化三轴未决盘点：nil 入参返回 nil（不覆盖 sticky 状态）；
// 非 nil 入参恒返回非 nil 容器（即使三轴全空），以支持终态写空做复位。
func normalizeReplanAxes(in *builtin_tools.ReplanAxes) *builtin_tools.ReplanAxes {
	if in == nil {
		return nil
	}
	return &builtin_tools.ReplanAxes{
		IncompleteItems: builtin_tools.NormalizeAxisItems(in.IncompleteItems),
		DepthGaps:       builtin_tools.NormalizeAxisItems(in.DepthGaps),
		NewSurfaces:     builtin_tools.NormalizeAxisItems(in.NewSurfaces),
	}
}

func normalizeSkillNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SetIteration 设置迭代次数
func (t *StateTracker) SetIteration(iter int) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Iteration = iter
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) SetPhase(phase builtin_tools.AgentPhase) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	if strings.TrimSpace(string(phase)) == "" {
		phase = builtin_tools.AgentPhaseStep
	}
	t.state.Phase = phase
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) SetCurrentGoal(goal string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.CurrentGoal = strings.TrimSpace(goal)
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) AppendInputTimeline(content string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	content = strings.TrimSpace(content)
	if content == "" {
		t.touchLocked()
		return *t.state
	}
	t.state.InputTimeline = append(t.state.InputTimeline, &builtin_tools.TimelineInput{
		Content:   content,
		CreatedAt: time.Now(),
	})
	t.state.CurrentGoal = content
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) AppendInputTimelineWithoutGoal(content string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	content = strings.TrimSpace(content)
	if content == "" {
		t.touchLocked()
		return *t.state
	}
	t.state.InputTimeline = append(t.state.InputTimeline, &builtin_tools.TimelineInput{
		Content:   content,
		CreatedAt: time.Now(),
	})
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) Replace(snapshot builtin_tools.StateSnapshot) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	builtin_tools.HydratePlanRelations(snapshot.Plan)
	if snapshot.PlanVersion <= 0 && len(snapshot.Plan) > 0 {
		snapshot.PlanVersion = 1
	}
	if strings.TrimSpace(string(snapshot.Phase)) == "" {
		snapshot.Phase = builtin_tools.AgentPhasePlan
	}
	snapshot.ActiveSkillNames = normalizeSkillNames(snapshot.ActiveSkillNames)
	snapshot.UpdatedAt = time.Now()
	t.state = &snapshot
	return *t.state
}

func (t *StateTracker) AddActiveSkillNames(names []string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.ActiveSkillNames = normalizeSkillNames(append(copyStrings(t.state.ActiveSkillNames), names...))
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) RemoveActiveSkillNames(names []string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	removeSet := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		removeSet[name] = struct{}{}
	}
	if len(removeSet) == 0 {
		t.touchLocked()
		return *t.state
	}

	next := make([]string, 0, len(t.state.ActiveSkillNames))
	for _, raw := range t.state.ActiveSkillNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := removeSet[name]; exists {
			continue
		}
		next = append(next, name)
	}
	t.state.ActiveSkillNames = normalizeSkillNames(next)
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) AddActiveMCPServers(names []string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.ActiveMCPServers = normalizeSkillNames(append(copyStrings(t.state.ActiveMCPServers), names...))
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) RemoveActiveMCPServers(names []string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	removeSet := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		removeSet[name] = struct{}{}
	}
	if len(removeSet) == 0 {
		t.touchLocked()
		return *t.state
	}

	next := make([]string, 0, len(t.state.ActiveMCPServers))
	for _, raw := range t.state.ActiveMCPServers {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := removeSet[name]; exists {
			continue
		}
		next = append(next, name)
	}
	t.state.ActiveMCPServers = normalizeSkillNames(next)
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) EnsureCurrentStep() builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureCurrentStepLocked()
	t.syncGoalToCurrentStepLocked()
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) syncGoalToCurrentStepLocked() {
	step := (builtin_tools.StateSnapshot{Plan: t.state.Plan, CurrentStepID: t.state.CurrentStepID}).CurrentStep()
	if step == nil {
		return
	}
	stepText := strings.TrimSpace(step.Step)
	if stepText != "" {
		t.state.CurrentGoal = stepText
	}
}

func (t *StateTracker) MarkCurrentStepInProgress() builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	step := builtin_tools.StateSnapshot{Plan: t.state.Plan, CurrentStepID: t.state.CurrentStepID}.CurrentStep()
	if step == nil {
		t.touchLocked()
		return *t.state
	}

	switch step.Status {
	case "", builtin_tools.PlanStepPending:
		step.Status = builtin_tools.PlanStepInProgress
	default:
		// Do not override terminal or already running states.
	}

	t.touchLocked()
	return *t.state
}

func (t *StateTracker) SetFinalAnswer(content string, source string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	content = strings.TrimSpace(content)
	source = strings.TrimSpace(source)
	if content == "" {
		t.state.FinalAnswer = nil
		t.touchLocked()
		return *t.state
	}
	t.state.FinalAnswer = &builtin_tools.FinalAnswer{
		Content:   content,
		Source:    source,
		CreatedAt: time.Now(),
	}
	t.state.Phase = builtin_tools.AgentPhaseFinalAnswer
	t.touchLocked()
	return *t.state
}

func (t *StateTracker) SetExternalInterrupt(info *builtin_tools.ExternalInterrupt) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.ExternalInterrupt = builtin_tools.CloneExternalInterrupt(info)
	t.touchLocked()
	return *t.state
}

// UpdatePlan 更新计划
func (t *StateTracker) UpdatePlan(plan []*builtin_tools.PlanItem, explanation string, needsPlanning bool) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	builtin_tools.HydratePlanRelations(plan)
	t.state.Plan = plan
	t.state.NeedsPlanning = needsPlanning
	t.state.PlanVersion++
	t.state.Phase = builtin_tools.AgentPhaseStep
	t.state.Status = builtin_tools.TaskStatusRunning
	t.state.Progress = builtin_tools.PlanProgress(plan)
	t.state.ReplanContext = nil
	t.state.ExternalInterrupt = nil
	t.state.CurrentStepID = strings.TrimSpace(t.state.CurrentStepID)
	if current := (builtin_tools.StateSnapshot{Plan: plan, CurrentStepID: t.state.CurrentStepID}).CurrentStep(); current != nil {
		t.state.CurrentStepID = strings.TrimSpace(current.ID)
	} else {
		t.state.CurrentStepID = ""
	}
	t.syncGoalToCurrentStepLocked()
	t.touchLocked()
	return *t.state
}

// SetGoalUnderstanding 记录 planner 对原始输入的结构化理解，供下游 step_replan 锚定原始意图。
// 空字符串不覆盖已有值，避免重规划回合误清空首次理解。
func (t *StateTracker) SetGoalUnderstanding(understanding string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	if u := strings.TrimSpace(understanding); u != "" {
		t.state.GoalUnderstanding = u
		t.touchLocked()
	}
	return *t.state
}

func (t *StateTracker) UpdateCurrentStep(update builtin_tools.CurrentStepUpdate) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	update.Summary = strings.TrimSpace(update.Summary)
	update.DisplayResult = strings.TrimSpace(update.DisplayResult)
	update.Result = strings.TrimSpace(update.Result)
	update.Error = strings.TrimSpace(update.Error)
	update.References = normalizeReferences(update.References)

	step := builtin_tools.StateSnapshot{Plan: t.state.Plan, CurrentStepID: t.state.CurrentStepID}.CurrentStep()
	if step == nil {
		t.touchLocked()
		return *t.state
	}

	step.Status = update.Status
	if update.Status == builtin_tools.PlanStepFailed {
		// 失败传播：依赖该失败节点的后续 step 统一标记为 skipped，避免永久 pending。
		_ = builtin_tools.PropagateSkippedPlanSteps(t.state.Plan)
	}
	// 保留 current_step_id，供 step_summary phase 针对刚完成的 step 生成总结；
	// summary 完成后再由 runtime 选择下一步。
	t.upsertStepOutcomeLocked(step, update)
	t.state.Progress = builtin_tools.PlanProgress(t.state.Plan)
	t.state.Phase = builtin_tools.AgentPhaseStepReplan
	t.touchLocked()
	return *t.state
}

// UpdateTaskStatus 更新任务状态
func (t *StateTracker) UpdateTaskStatus(update builtin_tools.TaskStatusUpdate) builtin_tools.StateSnapshot {
	update.Task = strings.TrimSpace(update.Task)
	update.Message = strings.TrimSpace(update.Message)
	update.Result = strings.TrimSpace(update.Result)
	update.Error = strings.TrimSpace(update.Error)

	progressProvided := update.Progress >= 0
	progress := update.Progress
	if progressProvided {
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if strings.TrimSpace(string(update.Status)) != "" {
		t.state.Status = update.Status
	}
	if update.Message != "" {
		t.state.StatusSummary = update.Message
	}
	if progressProvided {
		t.state.Progress = progress
	} else if update.Status == builtin_tools.TaskStatusCompleted {
		t.state.Progress = 100
	}
	if update.Result != "" {
		t.state.FinalAnswer = &builtin_tools.FinalAnswer{
			Content:   update.Result,
			Source:    "update_task_status",
			CreatedAt: time.Now(),
		}
	}
	if update.Error != "" {
		t.state.Error = update.Error
	}
	switch update.Status {
	case builtin_tools.TaskStatusCompleted:
		t.state.Phase = builtin_tools.AgentPhaseFinalAnswer
	case builtin_tools.TaskStatusFailed, builtin_tools.TaskStatusCanceled:
		t.state.CurrentStepID = ""
	}
	if update.Status == builtin_tools.TaskStatusCompleted || update.Status == builtin_tools.TaskStatusFailed || update.Status == builtin_tools.TaskStatusCanceled {
		t.state.ReplanContext = nil
	}

	t.touchLocked()
	return *t.state
}

func (t *StateTracker) ApplyStepReplan(stepID string, update stepReplanUpdate) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		t.touchLocked()
		return *t.state
	}

	update.ArtifactDir = strings.TrimSpace(update.ArtifactDir)
	update.ResultFile = strings.TrimSpace(update.ResultFile)
	update.TimelineFile = strings.TrimSpace(update.TimelineFile)
	update.CoverageFile = strings.TrimSpace(update.CoverageFile)
	update.ContextKey = strings.TrimSpace(update.ContextKey)
	update.Namespace = strings.TrimSpace(update.Namespace)
	update.CurrentGoal = strings.TrimSpace(update.CurrentGoal)
	update.References = normalizeReferences(update.References)
	update.Warnings = normalizeReferences(update.Warnings)
	update.ReplanContext = builtin_tools.CloneReplanContext(update.ReplanContext)

	var backfilled *builtin_tools.StepOutcome
	for _, outcome := range t.state.StepOutcomes {
		if outcome == nil {
			continue
		}
		if strings.TrimSpace(outcome.StepID) != stepID {
			continue
		}
		outcome.ArtifactDir = update.ArtifactDir
		outcome.ResultFile = update.ResultFile
		outcome.TimelineFile = update.TimelineFile
		outcome.CoverageFile = update.CoverageFile
		outcome.ContextKey = update.ContextKey
		outcome.Namespace = update.Namespace
		outcome.PlanVersion = update.PlanVersion
		outcome.TranscriptBlobRef = update.TranscriptBlobRef
		outcome.InheritedContextKeys = builtin_tools.CloneStringSlice(update.InheritedContextKeys)
		outcome.InheritedRefIDs = builtin_tools.CloneStringSlice(update.InheritedRefIDs)
		outcome.References = normalizeReferences(append(outcome.References, update.References...))
		outcome.UpdatedAt = time.Now()
		backfilled = outcome
		break
	}

	// 终态烘焙：把 outcome 产出写回 plan_item，使 plan 真相源（planner.jsonl）携带完整产出与指针。
	if item := (builtin_tools.StateSnapshot{Plan: t.state.Plan, CurrentStepID: stepID}).CurrentStep(); item != nil {
		item.BakeOutcome(backfilled)
		// step 过程文件指针不经 outcome（StepOutcome 无该字段），由 runtime 探测后直填。
		if sf := strings.TrimSpace(update.StepFile); sf != "" {
			item.StepFile = sf
		}
	}

	// step replan 完成后释放 current_step_id，下一轮由 EnsureCurrentStep 选择下一步
	if strings.TrimSpace(t.state.CurrentStepID) == stepID {
		t.state.CurrentStepID = ""
	}

	// 直达 Step 重编排：在烘焙 completed plan_item 之后原子替换整个 plan 与 PlanVersion，
	// 等价于 UpdatePlan 但保留前一阶段刚写回 outcome 的烘焙字段。NewPlan 仅由 phase_step_replan
	// 在 should_replan=true 时传入，已包含合并后的 completed / in_progress / 新 pending。
	if len(update.NewPlan) > 0 {
		builtin_tools.HydratePlanRelations(update.NewPlan)
		t.state.Plan = update.NewPlan
		t.state.PlanVersion++
		t.state.NeedsPlanning = true
	}

	if update.CurrentGoal != "" {
		t.state.CurrentGoal = update.CurrentGoal
	}
	if update.Warnings != nil {
		t.state.Warnings = normalizeReferences(append(t.state.Warnings, update.Warnings...))
	}
	// step_replan 不再写 UnresolvedAxes（三轴输出已删）；该字段仅由 final_answer 自身评估写入。
	t.state.ReplanContext = builtin_tools.CloneReplanContext(update.ReplanContext)
	t.state.Phase = update.NextPhase
	if update.NextPhase == builtin_tools.AgentPhasePlan {
		t.state.Error = ""
	}
	t.state.Progress = builtin_tools.PlanProgress(t.state.Plan)
	t.touchLocked()
	return *t.state
}

type stepReplanUpdate struct {
	ArtifactDir  string
	ResultFile   string
	TimelineFile string
	StepFile     string
	CoverageFile string
	ContextKey   string
	References   []string

	Namespace            string
	PlanVersion          int
	TranscriptBlobRef    string
	InheritedContextKeys []string
	InheritedRefIDs      []string

	CurrentGoal   string
	Warnings      []string
	ReplanContext *builtin_tools.ReplanContext
	// NewPlan 仅由 phase_step_replan 在 should_replan=true 时传入，承载直接重编排后的
	// 完整 plan（已含 completed / in_progress / 新 pending 的 merge 结果）。非空时原子
	// 替换 state.Plan、PlanVersion++，等价于 UpdatePlan 但发生在烘焙之后。
	NewPlan []*builtin_tools.PlanItem

	NextPhase builtin_tools.AgentPhase
}

func cloneStringSliceOrNil(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type finalAnswerPhaseUpdate struct {
	NextPhase builtin_tools.AgentPhase
	Status    builtin_tools.TaskStatus

	StatusSummary string
	Error         string

	FinalAnswerContent    string
	FinalAnswerSource     string
	FinalAnswerReferences []string

	NextGoal          string
	Warnings          []string
	UnresolvedAxes    *builtin_tools.ReplanAxes
	ReplanContext     *builtin_tools.ReplanContext
	ExternalInterrupt *builtin_tools.ExternalInterrupt
}

func (t *StateTracker) ApplyFinalAnswerPhaseUpdate(update finalAnswerPhaseUpdate) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	update.NextGoal = strings.TrimSpace(update.NextGoal)
	update.StatusSummary = strings.TrimSpace(update.StatusSummary)
	update.Error = strings.TrimSpace(update.Error)
	update.FinalAnswerContent = strings.TrimSpace(update.FinalAnswerContent)
	update.FinalAnswerSource = strings.TrimSpace(update.FinalAnswerSource)
	update.FinalAnswerReferences = normalizeReferences(update.FinalAnswerReferences)
	update.UnresolvedAxes = normalizeReplanAxes(update.UnresolvedAxes)
	update.ReplanContext = builtin_tools.CloneReplanContext(update.ReplanContext)
	update.ExternalInterrupt = builtin_tools.CloneExternalInterrupt(update.ExternalInterrupt)

	if strings.TrimSpace(string(update.Status)) != "" {
		t.state.Status = update.Status
	}
	if strings.TrimSpace(string(update.NextPhase)) != "" {
		t.state.Phase = update.NextPhase
	} else {
		t.state.Phase = builtin_tools.AgentPhaseFinalAnswer
	}

	if update.NextGoal != "" {
		t.state.CurrentGoal = update.NextGoal
	}

	if update.UnresolvedAxes != nil {
		t.state.UnresolvedAxes = builtin_tools.CloneReplanAxes(update.UnresolvedAxes)
	}
	if update.Warnings != nil {
		t.state.Warnings = normalizeReferences(append(t.state.Warnings, update.Warnings...))
	}
	t.state.ReplanContext = builtin_tools.CloneReplanContext(update.ReplanContext)
	if update.ExternalInterrupt != nil {
		t.state.ExternalInterrupt = builtin_tools.CloneExternalInterrupt(update.ExternalInterrupt)
	} else if t.state.Phase == builtin_tools.AgentPhasePlan {
		t.state.ExternalInterrupt = nil
	}

	if update.StatusSummary != "" {
		t.state.StatusSummary = update.StatusSummary
	}
	if update.Error != "" {
		t.state.Error = update.Error
	} else if t.state.Phase == builtin_tools.AgentPhasePlan {
		// 回流到 plan 时清空 runtime error（避免旧错误污染下一轮）
		t.state.Error = ""
	}

	if update.FinalAnswerContent != "" {
		source := update.FinalAnswerSource
		if source == "" {
			source = "final_answer"
		}
		t.state.FinalAnswer = &builtin_tools.FinalAnswer{
			Content:    update.FinalAnswerContent,
			Source:     source,
			CreatedAt:  time.Now(),
			References: update.FinalAnswerReferences,
		}
		// status_summary 默认复用最终答案文本（方便 UI 快速展示）
		t.state.StatusSummary = update.FinalAnswerContent
	} else if t.state.Phase == builtin_tools.AgentPhasePlan {
		t.state.FinalAnswer = nil
	}

	if t.state.Phase == builtin_tools.AgentPhasePlan {
		t.state.CurrentStepID = ""
	} else if t.state.Phase == builtin_tools.AgentPhaseFinalAnswer {
		t.state.ReplanContext = nil
	}

	switch t.state.Status {
	case builtin_tools.TaskStatusCompleted:
		t.state.Progress = 100
	case builtin_tools.TaskStatusFailed, builtin_tools.TaskStatusCanceled:
		t.state.CurrentStepID = ""
	default:
		t.state.Progress = builtin_tools.PlanProgress(t.state.Plan)
	}

	t.touchLocked()
	return *t.state
}

func (t *StateTracker) Finalize(status builtin_tools.TaskStatus, content string, source string, errText string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	content = strings.TrimSpace(content)
	source = strings.TrimSpace(source)
	errText = strings.TrimSpace(errText)

	t.state.Status = status
	t.state.Phase = builtin_tools.AgentPhaseFinalAnswer
	if content != "" {
		t.state.FinalAnswer = &builtin_tools.FinalAnswer{
			Content:   content,
			Source:    source,
			CreatedAt: time.Now(),
		}
		// status_summary 默认复用最终答案文本（方便 UI 快速展示）
		t.state.StatusSummary = content
	}
	if errText != "" {
		t.state.Error = errText
	}
	switch status {
	case builtin_tools.TaskStatusCompleted:
		t.state.Progress = 100
	case builtin_tools.TaskStatusFailed, builtin_tools.TaskStatusCanceled:
		t.state.CurrentStepID = ""
	}
	if status == builtin_tools.TaskStatusCompleted || status == builtin_tools.TaskStatusFailed || status == builtin_tools.TaskStatusCanceled {
		t.state.ReplanContext = nil
	}

	t.touchLocked()
	return *t.state
}

func (t *StateTracker) EnterFinalAnswer(status builtin_tools.TaskStatus, errText string) builtin_tools.StateSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	errText = strings.TrimSpace(errText)
	t.state.Status = status
	t.state.Error = errText
	t.state.Phase = builtin_tools.AgentPhaseFinalAnswer
	if status == builtin_tools.TaskStatusCompleted {
		t.state.Progress = 100
	}
	t.state.ReplanContext = nil
	t.touchLocked()
	return *t.state
}

// Reset 重置状态
func (t *StateTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = &builtin_tools.StateSnapshot{
		Phase:     builtin_tools.AgentPhasePlan,
		Status:    builtin_tools.TaskStatusPreparing,
		UpdatedAt: time.Now(),
	}
}

// SoftReset 保留 outcomes 和 timeline 上下文，清空执行状态（Plan、CurrentStepID、FinalAnswer 等）。
func (t *StateTracker) SoftReset(outcomes []*builtin_tools.StepOutcome, timeline []*builtin_tools.TimelineInput) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = &builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhasePlan,
		Status:        builtin_tools.TaskStatusPreparing,
		StepOutcomes:  outcomes,
		InputTimeline: timeline,
		UpdatedAt:     time.Now(),
	}
}

// SoftResetFrom 从持久化 snapshot 继承续写上下文（goal_understanding / CurrentGoal /
// Plan / PlanVersion / CurrentStepID / UnresolvedAxes / Active*），同时把相位与瞬态执行
// 字段重置到"待规划"起点。用于跨会话 carry/replan 恢复，避免丢弃原意图与原计划导致漂移。
// outcomes/timeline 由调用方（经 reducer 压缩后）显式传入。
func (t *StateTracker) SoftResetFrom(
	st builtin_tools.StateSnapshot,
	outcomes []*builtin_tools.StepOutcome,
	timeline []*builtin_tools.TimelineInput,
) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = &builtin_tools.StateSnapshot{
		Phase:             builtin_tools.AgentPhasePlan,
		Status:            builtin_tools.TaskStatusPreparing,
		GoalUnderstanding: strings.TrimSpace(st.GoalUnderstanding),
		CurrentGoal:       strings.TrimSpace(st.CurrentGoal),
		CurrentStepID:     strings.TrimSpace(st.CurrentStepID),
		Plan:              st.Plan,
		PlanVersion:       st.PlanVersion,
		UnresolvedAxes:    st.UnresolvedAxes,
		ActiveSkillNames:  st.ActiveSkillNames,
		ActiveMCPServers:  st.ActiveMCPServers,
		StepOutcomes:      outcomes,
		InputTimeline:     timeline,
		UpdatedAt:         time.Now(),
	}
}

// SetSimpleTask 标记/复位简单单步任务（step 完成后跳过 step_replan 直达 final_answer）。
// 每次 plan 提交都重设：重规划提交（simple 缺省 false）自然复位直通。
func (t *StateTracker) SetSimpleTask(simple bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.SimpleTask = simple
	t.touchLocked()
}

// ReplaceStepOutcomes 原子替换 state 中的 StepOutcomes（用于 reducer 写回压缩结果）。
func (t *StateTracker) ReplaceStepOutcomes(outcomes []*builtin_tools.StepOutcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != nil {
		t.state.StepOutcomes = outcomes
		t.state.UpdatedAt = time.Now()
	}
}

// SetReplanContext 原子设置 ReplanContext（不触发 outcome 更新等副作用）。
func (t *StateTracker) SetReplanContext(ctx *builtin_tools.ReplanContext) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.ReplanContext = builtin_tools.CloneReplanContext(ctx)
	t.touchLocked()
}

func (t *StateTracker) ensureCurrentStepLocked() {
	if strings.TrimSpace(t.state.CurrentStepID) != "" {
		if (builtin_tools.StateSnapshot{Plan: t.state.Plan, CurrentStepID: t.state.CurrentStepID}).CurrentStep() != nil {
			return
		}
		t.state.CurrentStepID = ""
	}
	nextID := builtin_tools.NextRunnablePlanStepID(t.state.Plan)
	if nextID != "" {
		t.state.CurrentStepID = nextID
	}
}

func (t *StateTracker) touchLocked() {
	t.state.UpdatedAt = time.Now()
}

func (t *StateTracker) SetStepOutcomeAttemptID(stepID, attemptID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stepID = strings.TrimSpace(stepID)
	attemptID = strings.TrimSpace(attemptID)
	if stepID == "" || attemptID == "" {
		return
	}
	for _, outcome := range t.state.StepOutcomes {
		if outcome != nil && strings.TrimSpace(outcome.StepID) == stepID {
			outcome.AttemptID = attemptID
			return
		}
	}
}

func (t *StateTracker) upsertStepOutcomeLocked(step *builtin_tools.PlanItem, update builtin_tools.CurrentStepUpdate) {
	if step == nil {
		return
	}
	stepID := strings.TrimSpace(step.ID)
	status := builtin_tools.StepOutcomeCompleted
	if update.Status == builtin_tools.PlanStepFailed {
		status = builtin_tools.StepOutcomeFailed
	}

	for _, outcome := range t.state.StepOutcomes {
		if outcome == nil {
			continue
		}
		if strings.TrimSpace(outcome.StepID) != stepID {
			continue
		}
		outcome.Status = status
		outcome.Summary = update.Summary
		outcome.DisplayResult = update.DisplayResult
		outcome.Result = update.Result
		outcome.Error = update.Error
		outcome.References = normalizeReferences(append(outcome.References, update.References...))
		outcome.StatusSummary = update.StatusSummary
		outcome.ShortSummary = update.ShortSummary
		outcome.LongSummary = update.LongSummary
		outcome.KeyFacts = update.KeyFacts
		outcome.CoverageChecklist = update.CoverageChecklist
		outcome.UpdatedAt = time.Now()
		return
	}

	t.state.StepOutcomes = append(t.state.StepOutcomes, &builtin_tools.StepOutcome{
		StepID:          stepID,
		Status:          status,
		Summary:         update.Summary,
		DisplayResult:   update.DisplayResult,
		Result:          update.Result,
		Error:           update.Error,
		References:      update.References,
		StatusSummary:     update.StatusSummary,
		ShortSummary:      update.ShortSummary,
		LongSummary:       update.LongSummary,
		KeyFacts:          update.KeyFacts,
		CoverageChecklist: update.CoverageChecklist,
		UpdatedAt:         time.Now(),
	})
}

func normalizeReferences(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
