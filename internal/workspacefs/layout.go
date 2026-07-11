// Package workspacefs 是 workspace 文件系统布局的单一真相源（叶子包，零非 stdlib 依赖）。
//
// Layout 收口全部路径知识：rel（slash 形式）为原语，Abs = filepath.Join(Root, FromSlash(rel))。
// 每个路径提供 XxxRel() / Xxx()（Abs）双形态。Layout 是纯值类型，不做任何 IO；
// 路径穿越防护属 IO 层职责（Store，M4），Layout 对非法入参（空串）统一返回空串，
// 与现存 helper 的防呆口径一致。
//
// 设计依据：docs/workspace-fs-layout.md（已评审）。
package workspacefs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Layout 描述一个 workspace 的路径布局。
// Namespace 为归一后语义：""（含入参 "root"）表示顶层；非空表示子 agent 命名空间。
type Layout struct {
	Root      string // workspace 根目录绝对路径
	Namespace string // 归一后的命名空间，"" = 顶层
}

// New 构造 Layout。namespace 归一规则：trim 空白与斜杠后，""|"root" → ""（顶层）。
func New(root, namespace string) Layout {
	return Layout{
		Root:      strings.TrimSpace(root),
		Namespace: normalizeNamespace(namespace),
	}
}

// normalizeNamespace 统一 namespace 语义：顶层的两种历史写法（"" 与 "root"）归一为 ""。
// 非顶层 namespace 仅做 trim（与现存 sanitize 的 best-effort 口径一致，
// 调用方应传入已消毒的相对 namespace）。
func normalizeNamespace(namespace string) string {
	namespace = filepath.ToSlash(strings.TrimSpace(namespace))
	namespace = strings.Trim(namespace, "/")
	if namespace == "root" {
		return ""
	}
	return namespace
}

// abs 是 rel → Abs 的唯一换算点。rel 或 Root 为空时返回空串。
func (l Layout) abs(rel string) string {
	if rel == "" || l.Root == "" {
		return ""
	}
	return filepath.Join(l.Root, filepath.FromSlash(rel))
}

// ---- workspace/ 区：runtime 元数据 ----

func (l Layout) PlannerJournalRel() string { return "workspace/planner.jsonl" }
func (l Layout) PlannerJournal() string    { return l.abs(l.PlannerJournalRel()) }

func (l Layout) StepContextsRel() string { return "workspace/step_contexts.jsonl" }
func (l Layout) StepContexts() string    { return l.abs(l.StepContextsRel()) }

func (l Layout) StateJSONRel() string { return "workspace/state.json" }
func (l Layout) StateJSON() string    { return l.abs(l.StateJSONRel()) }

func (l Layout) ReferencesRel() string { return "workspace/references.jsonl" }
func (l Layout) References() string    { return l.abs(l.ReferencesRel()) }

// ---- workspace/sessions/<id>/ 区：persistv2 event-sourcing ----

func (l Layout) SessionDirRel(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return "workspace/sessions/" + sessionID
}
func (l Layout) SessionDir(sessionID string) string { return l.abs(l.SessionDirRel(sessionID)) }

func (l Layout) SessionEventsRel(sessionID string) string {
	return joinRel(l.SessionDirRel(sessionID), "events.jsonl")
}
func (l Layout) SessionEvents(sessionID string) string { return l.abs(l.SessionEventsRel(sessionID)) }

func (l Layout) SessionSnapshotRel(sessionID string) string {
	return joinRel(l.SessionDirRel(sessionID), "snapshot.json")
}
func (l Layout) SessionSnapshot(sessionID string) string {
	return l.abs(l.SessionSnapshotRel(sessionID))
}

func (l Layout) SessionBlobsDirRel(sessionID string) string {
	return joinRel(l.SessionDirRel(sessionID), "blobs")
}
func (l Layout) SessionBlobsDir(sessionID string) string {
	return l.abs(l.SessionBlobsDirRel(sessionID))
}

// SessionBlobRel 接受裸 hex 或带 "sha256:" 前缀的 blob ref。
func (l Layout) SessionBlobRel(sessionID, ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "sha256:")
	if ref == "" {
		return ""
	}
	return joinRel(l.SessionBlobsDirRel(sessionID), ref)
}
func (l Layout) SessionBlob(sessionID, ref string) string {
	return l.abs(l.SessionBlobRel(sessionID, ref))
}

func (l Layout) StepAttemptDirRel(sessionID, stepID, attemptID string) string {
	stepID = strings.TrimSpace(stepID)
	attemptID = strings.TrimSpace(attemptID)
	if stepID == "" || attemptID == "" {
		return ""
	}
	return joinRel(l.SessionDirRel(sessionID), "steps/"+stepID+"/attempts/"+attemptID)
}
func (l Layout) StepAttemptDir(sessionID, stepID, attemptID string) string {
	return l.abs(l.StepAttemptDirRel(sessionID, stepID, attemptID))
}

func (l Layout) StepAttemptResultRel(sessionID, stepID, attemptID string) string {
	return joinRel(l.StepAttemptDirRel(sessionID, stepID, attemptID), "result.json")
}
func (l Layout) StepAttemptResult(sessionID, stepID, attemptID string) string {
	return l.abs(l.StepAttemptResultRel(sessionID, stepID, attemptID))
}

// ---- shared/ 区：AI 协作文本与 step 过程文件 ----

func (l Layout) SharedDirRel() string { return "shared" }
func (l Layout) SharedDir() string    { return l.abs(l.SharedDirRel()) }

func (l Layout) TaskContextRel() string { return "shared/task_context.md" }
func (l Layout) TaskContext() string    { return l.abs(l.TaskContextRel()) }

func (l Layout) OpenItemsRel() string { return "shared/open_items.md" }
func (l Layout) OpenItems() string    { return l.abs(l.OpenItemsRel()) }

func (l Layout) StepFileRel(stepID string) string {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return ""
	}
	return "shared/step_" + stepID + ".md"
}
func (l Layout) StepFile(stepID string) string { return l.abs(l.StepFileRel(stepID)) }

// LegacyStepFileRel 是老 session 的 step 过程文件布局（shared/<stepID>/step.md），只读兼容。
func (l Layout) LegacyStepFileRel(stepID string) string {
	return joinRel(l.StepDirRel(stepID), "step.md")
}
func (l Layout) LegacyStepFile(stepID string) string { return l.abs(l.LegacyStepFileRel(stepID)) }

func (l Layout) StepDirRel(stepID string) string {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return ""
	}
	return "shared/" + stepID
}
func (l Layout) StepDir(stepID string) string { return l.abs(l.StepDirRel(stepID)) }

func (l Layout) StepTimelineRel(stepID string) string {
	return joinRel(l.StepDirRel(stepID), "timeline.jsonl")
}
func (l Layout) StepTimeline(stepID string) string { return l.abs(l.StepTimelineRel(stepID)) }

func (l Layout) StepCoverageRel(stepID string) string {
	return joinRel(l.StepDirRel(stepID), "coverage.json")
}
func (l Layout) StepCoverage(stepID string) string { return l.abs(l.StepCoverageRel(stepID)) }

// ---- artifacts/ 区：版本化制品（归一语义：顶层 = artifacts/）----

func (l Layout) ArtifactsRootRel() string {
	if l.Namespace == "" {
		return "artifacts"
	}
	return "artifacts/" + l.Namespace
}
func (l Layout) ArtifactsRoot() string { return l.abs(l.ArtifactsRootRel()) }

func (l Layout) PlanCurrentRel() string { return l.ArtifactsRootRel() + "/plan/current.json" }
func (l Layout) PlanCurrent() string    { return l.abs(l.PlanCurrentRel()) }

// PlanHistoryRel 对 planVersion<=0 归一为 1（与旧 artifactWriter.planHistoryFileRel 防呆一致）。
func (l Layout) PlanHistoryRel(planVersion int) string {
	if planVersion <= 0 {
		planVersion = 1
	}
	return fmt.Sprintf("%s/plan/history/%d.json", l.ArtifactsRootRel(), planVersion)
}
func (l Layout) PlanHistory(planVersion int) string { return l.abs(l.PlanHistoryRel(planVersion)) }

func (l Layout) FinalRootRel() string { return l.ArtifactsRootRel() + "/final" }
func (l Layout) FinalRoot() string    { return l.abs(l.FinalRootRel()) }

// FinalDirRel 对 finalSeq<=0 返回空串（与旧 artifactWriter.finalDirRel 防呆一致）。
func (l Layout) FinalDirRel(finalSeq int) string {
	if finalSeq <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/final/%d", l.ArtifactsRootRel(), finalSeq)
}
func (l Layout) FinalDir(finalSeq int) string { return l.abs(l.FinalDirRel(finalSeq)) }

func (l Layout) FinalAnswerRel(finalSeq int) string {
	return joinRel(l.FinalDirRel(finalSeq), "final_answer.md")
}
func (l Layout) FinalAnswer(finalSeq int) string { return l.abs(l.FinalAnswerRel(finalSeq)) }

func (l Layout) FinalAssessmentRel(finalSeq int) string {
	return joinRel(l.FinalDirRel(finalSeq), "final_assessment.json")
}
func (l Layout) FinalAssessment(finalSeq int) string { return l.abs(l.FinalAssessmentRel(finalSeq)) }

// ---- 其他 ----

func (l Layout) SubAgentDirRel(childName string) string {
	childName = strings.TrimSpace(childName)
	if childName == "" {
		return ""
	}
	return "sub_agents/" + childName
}
func (l Layout) SubAgentDir(childName string) string { return l.abs(l.SubAgentDirRel(childName)) }

func (l Layout) AsyncResultRel() string { return "async_result.json" }
func (l Layout) AsyncResult() string    { return l.abs(l.AsyncResultRel()) }

func (l Layout) ToolOutputDirRel() string { return "tool-output" }
func (l Layout) ToolOutputDir() string    { return l.abs(l.ToolOutputDirRel()) }

// SubAgentLayout 派生子 agent 的 Layout：以 sub_agents/<child> 为新 Root 的顶层布局
// （子 agent workspace 与父同构递归）。
func (l Layout) SubAgentLayout(childName string) Layout {
	dir := l.SubAgentDir(childName)
	if dir == "" {
		return Layout{}
	}
	return New(dir, "")
}

// joinRel 拼接两段 rel；任一为空返回空串（空值防呆统一口径）。
func joinRel(base, tail string) string {
	if base == "" || tail == "" {
		return ""
	}
	return base + "/" + tail
}
