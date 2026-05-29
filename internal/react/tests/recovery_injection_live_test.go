package react_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

// TestRecoveryInjection_PlannerLive 用真实 LLM 验证「恢复回合注入」是否让父 planner
// 正确续跑：上一个 step（audit-sast）在中断前未被综合，其子 agent 产出（sub-A 完成 /
// sub-B 中断 / sub-C 失败）以路径指针 + 中断 step 的 shared 目录注入 planner prompt。
// 期望模型 NOT 从零重派全部子 agent，而是先读取/综合盘上已有产出，再补跑缺口（sub-B/sub-C）。
//
// 启用方式：SASTPRO_REACT_LIVE_TEST=1 go test ./internal/react/tests/... -run TestRecoveryInjection -v
func TestRecoveryInjection_PlannerLive(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1")
	}

	client := newOpenCodeGoClient(t)

	// 用 temp dir 作 workspace root，让注入里的路径是真实存在的绝对路径（更贴近生产）。
	root := t.TempDir()
	sharedAuditDir := root + "/shared/audit-sast"
	sharedScanDir := root + "/shared/auto-scan-1"
	subAFinal := root + "/sub_agents/sub-A/final_assessment.json"
	for _, d := range []string{sharedAuditDir, sharedScanDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// 在中断 step 的 shared 目录里放真实半成品产出，模型续跑应据此读取。
	_ = os.WriteFile(sharedAuditDir+"/sqli_candidates.txt", []byte("UserMapper.xml:45 param=name\nOrderMapper.xml:82 param=id\n"), 0o644)
	_ = os.WriteFile(sharedAuditDir+"/rce_candidates.txt", []byte("FileController.java:120 ProcessBuilder\n"), 0o644)
	_ = os.WriteFile(sharedScanDir+"/timeline.jsonl", []byte("{}\n"), 0o644)
	_ = os.MkdirAll(root+"/sub_agents/sub-A", 0o755)
	_ = os.WriteFile(subAFinal, []byte(`{"findings":[{"type":"SQLi","file":"UserMapper.xml"}]}`), 0o644)

	// 这段 JSON 与 buildRecoveryChildContextJSON 的输出结构一致（恢复 gate 命中后产出）。
	recovery := recoveryContextDoc{
		Note: "本次为恢复回合：以下子 agent 隶属的 step 在中断前未被综合进 step_outcomes，其结果对你不可见。请按路径用文件工具读取产出后续跑或综合；status=interrupted 的子 agent 已随父中断停止，由你决定是否重新 sub_agent。",
		ChildAgents: []recoveryChildDoc{
			{AgentID: "sub-A", Status: "completed", ParentStepKey: "audit-sast", ArtifactRootDir: root + "/sub_agents/sub-A", LatestFinalFile: subAFinal},
			{AgentID: "sub-B", Status: "interrupted", ParentStepKey: "audit-sast", ArtifactRootDir: root + "/sub_agents/sub-B", Instruction: "扫描认证模块的越权与 IDOR 漏洞"},
			{AgentID: "sub-C", Status: "failed", ParentStepKey: "audit-sast", ArtifactRootDir: root + "/sub_agents/sub-C"},
		},
		InterruptedStepDirs: []string{sharedAuditDir, sharedScanDir},
	}
	recoveryJSON, _ := json.Marshal(recovery)

	now := time.Now()
	// 恢复后的 snapshot：recon-1 已完成；audit-sast 仍 in_progress（中断点），无对应 step_outcome。
	snapshot := builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhasePlan,
		Status:      builtin_tools.TaskStatusRunning,
		CurrentGoal: "对项目进行安全审计（RCE / SQL 注入 / 越权）",
		PlanVersion: 2,
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "请对项目进行安全审计，覆盖 RCE、SQL 注入与越权", CreatedAt: now.Add(-30 * time.Minute)},
		},
		Plan: []*builtin_tools.PlanItem{
			{ID: "recon-1", Step: "收集项目结构与攻击面", Status: builtin_tools.PlanStepCompleted},
			{ID: "audit-sast", Step: "并发子 agent 审计 RCE/SQLi/越权", Status: builtin_tools.PlanStepInProgress, DependsOn: []string{"recon-1"}},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "recon-1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "已识别 15 controller / 8 mapper", UpdatedAt: now.Add(-25 * time.Minute)},
		},
	}

	agentInstruction := `你是代码安全审计 Agent。续跑被中断的审计任务时，必须先利用已有产出，避免重复劳动：
- 对 status=completed 的子 agent，读取其 latest_final_file，把结果综合进当前 step，不要重派同样的子 agent
- 对 status=interrupted/failed 的子 agent，按其 instruction 决定是否重新 sub_agent 补齐
- 中断 step 的 interrupted_step_shared_dirs 里有半成品产出，用文件工具读取后续跑`

	planInput := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{
		AgentInstruction:    agentInstruction,
		RecoveryContextJSON: string(recoveryJSON),
	})
	if planInput == "" {
		t.Fatal("PlannerInputFromSnapshot returned empty")
	}

	// 确定性断言：注入确实渲染进 planner 输入。
	for _, must := range []string{
		"RECOVERY_CHILD_AGENTS",
		"sub-A", subAFinal,
		"sub-B", "扫描认证模块的越权与 IDOR 漏洞", "interrupted",
		"sub-C", "failed",
		sharedAuditDir,
	} {
		if !strings.Contains(planInput, must) {
			t.Fatalf("rendered planner input missing %q\n--- planInput ---\n%s", must, planInput)
		}
	}

	planner := NewDefaultTaskPlanner(client)
	prompt, err := planner.BuildPrompt(TaskPlannerPromptInput{Input: planInput})
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}

	os.WriteFile("/tmp/recovery_planner_prompt.txt", []byte(prompt), 0o644)
	t.Logf("=== RENDERED PLANNER PROMPT (%d bytes) saved to /tmp/recovery_planner_prompt.txt ===", len(prompt))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Log("Sending recovery planner prompt to LLM...")
	resp, err := client.Chat(ctx, ai.NewSystemMsgInfo(prompt))
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}
	os.WriteFile("/tmp/recovery_planner_response.txt", []byte(resp), 0o644)
	t.Logf("=== LLM RESPONSE ===\n%s", resp)

	// 软验证（LLM 非确定性）：响应应体现「复用已有产出」的恢复语义，而非从零重跑。
	respLower := strings.ToLower(resp)
	reuseSignals := []string{"final_assessment", "sub-a", "shared/audit-sast", "综合", "读取", "已有", "已完成", "复用", "汇总"}
	var hits []string
	for _, s := range reuseSignals {
		if strings.Contains(respLower, strings.ToLower(s)) {
			hits = append(hits, s)
		}
	}
	if len(hits) == 0 {
		t.Errorf("REVIEW: 响应未出现任何「复用已有产出」信号，可能恢复语义未被模型采纳；请人工查看 /tmp/recovery_planner_response.txt")
	} else {
		t.Logf("PASS signals: %v", hits)
	}

	// 解析 plan 打印，便于人工 review。
	clean := resp
	if i := strings.Index(resp, "{"); i >= 0 {
		if j := strings.LastIndex(resp, "}"); j > i {
			clean = resp[i : j+1]
		}
	}
	var result map[string]any
	if json.Unmarshal([]byte(clean), &result) == nil {
		if plan, ok := result["plan"].([]any); ok {
			t.Logf("Plan has %d steps:", len(plan))
			for i, step := range plan {
				if m, ok := step.(map[string]any); ok {
					t.Logf("  step %d: %v [%v]", i+1, m["step"], m["status"])
				}
			}
		}
		if exp, ok := result["explanation"].(string); ok {
			t.Logf("Explanation: %s", exp)
		}
	}
}

// 与 react.recoveryContextView / recoveryChildView 同构（外部测试包无法引用未导出类型，这里复刻字段）。
type recoveryContextDoc struct {
	Note                string             `json:"note"`
	ChildAgents         []recoveryChildDoc `json:"child_agents"`
	InterruptedStepDirs []string           `json:"interrupted_step_shared_dirs,omitempty"`
}

type recoveryChildDoc struct {
	AgentID         string `json:"agent_id"`
	Status          string `json:"status"`
	ParentStepKey   string `json:"parent_step_key,omitempty"`
	ArtifactRootDir string `json:"artifact_root_dir,omitempty"`
	LatestFinalFile string `json:"latest_final_file,omitempty"`
	Instruction     string `json:"instruction,omitempty"`
}
