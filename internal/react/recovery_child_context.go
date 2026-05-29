package react

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
	"aster/internal/runtimelog"
)

// recoveryChildView 描述一个「中断点未综合」子 agent 的现场指针。
// completed/failed 只给路径指针（ArtifactRootDir/LatestFinalFile），不内联 final_assessment.json 全文，
// 避免注入块无界膨胀；running（=被父 turn 中断）给 instruction + status=interrupted，仅告知，由父决定是否重派。
type recoveryChildView struct {
	AgentID         string `json:"agent_id"`
	Status          string `json:"status"`
	ParentStepKey   string `json:"parent_step_key,omitempty"`
	ArtifactRootDir string `json:"artifact_root_dir,omitempty"`
	LatestFinalFile string `json:"latest_final_file,omitempty"`
	Instruction     string `json:"instruction,omitempty"`
}

type recoveryContextView struct {
	Note                string              `json:"note"`
	ChildAgents         []recoveryChildView `json:"child_agents"`
	InterruptedStepDirs []string            `json:"interrupted_step_shared_dirs,omitempty"`
}

const recoveryContextNote = "本次为恢复回合：以下子 agent 隶属的 step 在中断前未被综合进 step_outcomes，其结果对你不可见。请按路径用文件工具读取产出后续跑或综合；status=interrupted 的子 agent 已随父中断停止，由你决定是否重新 sub_agent。"

// maybeBuildRecoveryChildContextJSON 是 runPlanPhase 的 gate 入口：仅恢复回合（resumeChildRecovery）
// 才尝试构建注入块，且判定后立即清标记，保证只在恢复后第一次 plan 出现。正常 replan 永不触发。
func (a *Agent) maybeBuildRecoveryChildContextJSON(snapshot builtin_tools.StateSnapshot) string {
	if a == nil || !a.resumeChildRecovery {
		return ""
	}
	a.resumeChildRecovery = false
	return a.buildRecoveryChildContextJSON(snapshot)
}

// buildRecoveryChildContextJSON 在恢复回合构建「中断点子 agent 现场 + 中断 step 盘上产出路径」的注入块。
//
// 仅当存在 child_agent，其 ParentStepKey 不对应任何「已完成的 step_outcome」时才返回非空——
// 即上一个 step 未跑完（或被 replan 改名/替换），其 sub_agent 结果从未折叠进 step_outcomes。
// 用「ParentStepKey 是否有已完成 step_outcome」判定，不用「ParentStepKey == 当前 plan step id」匹配，
// 因为真实数据里 ParentStepKey 可能与当前 plan step id 对不上（中途 replan 改名）。
//
// 命中返回 prettyJSON；未命中（或无 workspace runtime）返回空字符串。
func (a *Agent) buildRecoveryChildContextJSON(snapshot builtin_tools.StateSnapshot) string {
	if a == nil || a.workspaceRuntime == nil {
		return ""
	}
	state, err := a.workspaceRuntime.LoadWorkspaceState()
	if err != nil || state == nil || len(state.ChildAgents) == 0 {
		return ""
	}

	completedSteps := make(map[string]struct{})
	for _, outcome := range snapshot.StepOutcomes {
		if outcome == nil || outcome.Status != builtin_tools.StepOutcomeCompleted {
			continue
		}
		if id := strings.TrimSpace(outcome.StepID); id != "" {
			completedSteps[id] = struct{}{}
		}
	}

	names := make([]string, 0, len(state.ChildAgents))
	for name := range state.ChildAgents {
		names = append(names, name)
	}
	sort.Strings(names)

	views := make([]recoveryChildView, 0, len(names))
	stepDirSet := make(map[string]struct{})
	for _, name := range names {
		ptr := state.ChildAgents[name]
		if ptr == nil {
			continue
		}
		parentStep := strings.TrimSpace(ptr.ParentStepKey)
		// gate: 该 step 已综合（有已完成 step_outcome）则跳过——正常 replan 已折叠其结果。
		if parentStep != "" {
			if _, done := completedSteps[parentStep]; done {
				continue
			}
		}

		view := recoveryChildView{
			AgentID:         strings.TrimSpace(name),
			ParentStepKey:   parentStep,
			ArtifactRootDir: strings.TrimSpace(ptr.ArtifactRootDir),
			LatestFinalFile: strings.TrimSpace(ptr.LatestFinalFile),
		}
		switch strings.TrimSpace(ptr.Status) {
		case "running":
			view.Status = "interrupted"
			view.Instruction = a.loadChildInstruction(ptr.ArtifactRootDir)
		case "failed":
			view.Status = "failed"
		default:
			view.Status = "completed"
		}
		views = append(views, view)

		if parentStep != "" {
			stepDirSet[parentStep] = struct{}{}
		}
	}

	if len(views) == 0 {
		return ""
	}

	// 被中断的 in_progress step 也补上（其 shared 产出可能未写 step_context record）。
	for _, item := range snapshot.Plan {
		if item == nil || item.Status != builtin_tools.PlanStepInProgress {
			continue
		}
		if id := strings.TrimSpace(item.ID); id != "" {
			if _, done := completedSteps[id]; !done {
				stepDirSet[id] = struct{}{}
			}
		}
	}

	sharedDir := strings.TrimSpace(a.workspaceRuntime.SharedDir())
	stepDirs := make([]string, 0, len(stepDirSet))
	if sharedDir != "" {
		stepIDs := make([]string, 0, len(stepDirSet))
		for id := range stepDirSet {
			stepIDs = append(stepIDs, id)
		}
		sort.Strings(stepIDs)
		for _, id := range stepIDs {
			dir := filepath.Join(sharedDir, id)
			if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
				stepDirs = append(stepDirs, dir)
			}
		}
	}

	return prettyJSON(recoveryContextView{
		Note:                recoveryContextNote,
		ChildAgents:         views,
		InterruptedStepDirs: stepDirs,
	})
}

// loadChildInstruction 从子自身 V2 snapshot 的 input_timeline[0] 读取派生指令；失败返回空。
func (a *Agent) loadChildInstruction(childRootDir string) string {
	childRootDir = strings.TrimSpace(childRootDir)
	sessionID := strings.TrimSpace(a.workspaceSessionID)
	if childRootDir == "" || sessionID == "" {
		return ""
	}
	store, err := persistv2.Open(childRootDir, sessionID)
	if err != nil {
		return ""
	}
	snap, err := store.LoadSnapshot()
	if err != nil || snap == nil {
		return ""
	}
	ref := strings.TrimSpace(snap.RuntimeStateBlobRef)
	if ref == "" {
		return ""
	}
	raw, err := store.ReadBlob(ref)
	if err != nil || len(raw) == 0 {
		runtimelog.LogJSON("warn", map[string]any{"msg": "loadChildInstruction: read runtime_state blob", "error": errString(err)})
		return ""
	}
	var st builtin_tools.StateSnapshot
	if err := json.Unmarshal(raw, &st); err != nil {
		return ""
	}
	for _, item := range st.InputTimeline {
		if item == nil {
			continue
		}
		if c := strings.TrimSpace(item.Content); c != "" {
			return c
		}
	}
	return strings.TrimSpace(st.CurrentGoal)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
