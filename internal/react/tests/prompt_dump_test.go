package react_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

func buildRealisticSnapshot() builtin_tools.StateSnapshot {
	now := time.Now()
	return builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhaseStep,
		Status:      builtin_tools.TaskStatusRunning,
		CurrentGoal: "对项目进行安全审计，识别所有 SQL 注入漏洞",
		CurrentStepID: "step-2",
		PlanVersion:   1,
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "请对 /repo/project 目录下的 Go 项目进行安全审计，重点关注 SQL 注入漏洞", CreatedAt: now.Add(-10 * time.Minute)},
		},
		Plan: []*builtin_tools.PlanItem{
			{
				ID:     "step-1",
				Step:   "收集项目结构和入口文件",
				Status: builtin_tools.PlanStepCompleted,
			},
			{
				ID:        "step-2",
				Step:      "逐文件检查 SQL 拼接和参数化查询",
				Status:    builtin_tools.PlanStepInProgress,
				DependsOn: []string{"step-1"},
			},
			{
				ID:        "step-3",
				Step:      "汇总发现并生成报告",
				Status:    builtin_tools.PlanStepPending,
				DependsOn: []string{"step-2"},
			},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:       "step-1",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "已收集项目结构，发现 12 个 handler 文件和 5 个 repository 文件",
				KeyFacts: []string{
					"项目使用 Gin 框架",
					"数据库层使用 GORM",
					"存在 3 个直接拼接 SQL 的 repository 文件",
				},
				ToolCallsDigest: []string{
					"list_files(/repo/project) → 发现 45 个 .go 文件",
					"rg(\"db.Raw|db.Exec\") → 在 5 个文件中发现 8 处匹配",
				},
				References:    []string{"shared/step_artifacts/step-1.result.json"},
				StatusSummary: "项目结构收集完毕，已定位潜在风险文件",
				SummaryFile:   "shared/step_artifacts/step-1.summary.md",
				ResultFile:    "shared/step_artifacts/step-1.result.json",
				ContextKey:    "audit:1:step-1",
				UpdatedAt:     now.Add(-5 * time.Minute),
			},
		},
		Warnings:   []string{"repository/user_repo.go 中存在未经过滤的用户输入直接拼接到 SQL"},
		Unresolved: []string{"需要确认 middleware 层是否有统一的输入校验"},
	}
}

func dumpPrompt(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name+".prompt.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write prompt dump %s: %v", name, err)
	}
	// Also write to a persistent location for manual review
	persistDir := "/tmp/prompt_dump_review"
	_ = os.MkdirAll(persistDir, 0o755)
	persistPath := filepath.Join(persistDir, name+".prompt.txt")
	_ = os.WriteFile(persistPath, []byte(content), 0o644)
	t.Logf("prompt dumped to %s (%d bytes)", path, len(content))
}

func mustContainAll(t *testing.T, name, prompt string, markers []string) {
	t.Helper()
	for _, m := range markers {
		if !strings.Contains(prompt, m) {
			t.Errorf("[%s] missing expected content: %q", name, m)
		}
	}
}

func mustNotContain(t *testing.T, name, prompt string, forbidden []string) {
	t.Helper()
	for _, f := range forbidden {
		if strings.Contains(prompt, f) {
			t.Errorf("[%s] unexpected content found: %q", name, f)
		}
	}
}

func assertValidJSON(t *testing.T, name, section, prompt string) {
	t.Helper()
	startTag := "<" + section + ">"
	endTag := "</" + section + ">"
	startIdx := strings.Index(prompt, startTag)
	endIdx := strings.Index(prompt, endTag)
	if startIdx < 0 || endIdx < 0 {
		t.Errorf("[%s] section <%s> not found in prompt", name, section)
		return
	}
	content := strings.TrimSpace(prompt[startIdx+len(startTag) : endIdx])
	if content == "" || content == "null" || content == "[]" {
		return
	}
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Errorf("[%s] <%s> content is not valid JSON: %v\ncontent: %.500s", name, section, err, content)
	}
}

func TestPromptDump_AllPhases(t *testing.T) {
	dumpDir := t.TempDir()
	t.Logf("all prompts dumped to: %s", dumpDir)

	snapshot := buildRealisticSnapshot()

	// ───────────────────────────────────────────────
	// 1. Plan phase prompt (task_planner)
	// ───────────────────────────────────────────────
	t.Run("plan_phase", func(t *testing.T) {
		planner := NewDefaultTaskPlanner(&stubChatClient{})
		planInput := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{
			UserInstruction:    "你是安全审计 Agent，专注于发现 SQL 注入漏洞",
			ExtraContext:       "",
			WorkspaceRootDir:   "/repo/project",
			WorkspaceNamespace: "audit",
		})
		if planInput == "" {
			t.Fatal("PlannerInputFromSnapshot returned empty")
		}

		prompt, err := planner.BuildPrompt(planInput)
		if err != nil {
			t.Fatalf("BuildPrompt failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "01_plan_phase", prompt)

		mustContainAll(t, "plan", prompt, []string{
			"<USER_INSTRUCTION>",
			"安全审计 Agent",
			"<INPUT_TIMELINE>",
			"SQL 注入漏洞",
			"<TASK_ITEMS>",
			"step-1",
			"step-2",
			"step-3",
		})

		// Plan with existing outcomes should include EXECUTION_LINE section
		if len(snapshot.StepOutcomes) > 0 {
			mustContainAll(t, "plan_execution_line", prompt, []string{
				"step-1",
			})
		}
	})

	// ───────────────────────────────────────────────
	// 2. Plan phase with REPLAN_CONTEXT
	// ───────────────────────────────────────────────
	t.Run("plan_phase_replan", func(t *testing.T) {
		replanSnapshot := snapshot
		replanSnapshot.ReplanContext = &builtin_tools.ReplanContext{
			SourceStepID:   "step-2",
			Reason:         "step-2 发现新的攻击面需要额外检查",
			NextGoal:       "补充检查 ORM 层的 raw query 风险",
			MissingItems:   []string{"GORM Raw 调用点未全部覆盖"},
			Warnings:       []string{"部分 handler 使用 string format 构造查询"},
			ReplacePending: true,
		}

		planner := NewDefaultTaskPlanner(&stubChatClient{})
		planInput := PlannerInputFromSnapshot(replanSnapshot, PlannerInputOptions{
			UserInstruction:    "你是安全审计 Agent",
			WorkspaceRootDir:   "/repo/project",
			WorkspaceNamespace: "audit",
		})
		prompt, err := planner.BuildPrompt(planInput)
		if err != nil {
			t.Fatalf("BuildPrompt replan failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "02_plan_phase_replan", prompt)

		mustContainAll(t, "plan_replan", prompt, []string{
			"<REPLAN_CONTEXT>",
			"step-2 发现新的攻击面",
			"补充检查 ORM 层",
			"GORM Raw",
			"replace_pending",
		})
	})

	// ───────────────────────────────────────────────
	// 3. Step phase (think_act) — with dependencies
	// ───────────────────────────────────────────────
	t.Run("step_phase", func(t *testing.T) {
		agent, err := NewReActAgent(
			"security-audit",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
			WithInstruction("你是安全审计 Agent，专注于发现 SQL 注入漏洞。\n请逐文件检查所有数据库交互代码。"),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		agent.ReplaceState(snapshot)

		prompt := agent.BuildThinkActPrompt(context.Background(), "", &TaskContextData{
			Entries: []TaskContextEntry{
				{Label: "项目路径", Value: "/repo/project", Description: "待审计的项目根目录"},
				{Label: "扫描模式", Value: "deep"},
			},
		})
		dumpPrompt(t, dumpDir, "03_step_phase_think_act", prompt)

		mustContainAll(t, "think_act", prompt, []string{
			"安全审计 Agent",
			"<CURRENT_STEP>",
			"step-2",
			"<DEPENDENCY_STEP_SUMMARIES>",
			"step-1",
			"已收集项目结构",
			"项目使用 Gin 框架",
			"<WARNINGS>",
			"未经过滤的用户输入",
			"<UNRESOLVED>",
			"middleware 层",
			"项目路径: /repo/project",
			"扫描模式: deep",
		})

		// Verify dependency summary includes key_facts and tool_calls_digest
		// In JSON output, inner quotes are escaped: rg("db.Raw") → rg(\"db.Raw\")
		mustContainAll(t, "think_act_dep_detail", prompt, []string{
			"list_files",
			"rg(",
		})

		// No double-serialization: JSON objects should not be wrapped in extra quotes
		mustNotContain(t, "think_act_no_double_serial", prompt, []string{
			`"{\"step_id\":`,
		})
	})

	// ───────────────────────────────────────────────
	// 4. Step phase — no dependencies (first step)
	// ───────────────────────────────────────────────
	t.Run("step_phase_first_step", func(t *testing.T) {
		agent, err := NewReActAgent(
			"security-audit",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
			WithInstruction("你是安全审计 Agent"),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		firstStepSnap := builtin_tools.StateSnapshot{
			Phase:         builtin_tools.AgentPhaseStep,
			Status:        builtin_tools.TaskStatusRunning,
			CurrentGoal:   "收集项目结构",
			CurrentStepID: "step-1",
			PlanVersion:   1,
			Plan: []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "收集项目结构和入口文件", Status: builtin_tools.PlanStepInProgress},
				{ID: "step-2", Step: "逐文件检查 SQL", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
			},
		}
		agent.ReplaceState(firstStepSnap)

		prompt := agent.BuildThinkActPrompt(context.Background(), "", nil)
		dumpPrompt(t, dumpDir, "04_step_phase_first_step", prompt)

		mustContainAll(t, "first_step", prompt, []string{
			"<CURRENT_STEP>",
			"step-1",
			"收集项目结构",
		})

		// No dependency summaries for first step
		mustNotContain(t, "first_step_no_deps", prompt, []string{
			"<DEPENDENCY_STEP_SUMMARIES>",
		})
	})

	// ───────────────────────────────────────────────
	// 5. StepReplan phase prompt
	// ───────────────────────────────────────────────
	t.Run("step_replan_phase", func(t *testing.T) {
		agent, err := NewReActAgent(
			"step-replan-test",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		outcome := &builtin_tools.StepOutcome{
			StepID:       "step-2",
			Status:       builtin_tools.StepOutcomeCompleted,
			ShortSummary: "已检查 8 个 db.Raw 调用点，发现 3 处 SQL 注入",
			KeyFacts: []string{
				"user_repo.go:45 — 直接拼接用户输入到 WHERE 子句",
				"order_repo.go:82 — fmt.Sprintf 构造 ORDER BY",
				"search_handler.go:31 — 查询参数直接传入 db.Raw",
			},
			ToolCallsDigest: []string{
				"rg(\"db.Raw\") → 8 matches in 5 files",
				"read_file(user_repo.go) → 发现拼接 SQL",
				"read_file(order_repo.go) → 发现 fmt.Sprintf SQL",
			},
			OpenQuestions: []string{
				"是否存在通过 middleware 层统一过滤的情况？",
				"GORM 的 Scope 方法是否也有风险？",
			},
			References:  []string{"shared/step_artifacts/step-2.result.json"},
			SummaryFile: "shared/step_artifacts/step-2.summary.md",
			ResultFile:  "shared/step_artifacts/step-2.result.json",
			ContextKey:  "audit:1:step-2",
			UpdatedAt:   time.Now(),
		}

		prompt, err := agent.BuildStepReplanPrompt(map[string]any{
			"current_goal": "对项目进行安全审计，识别所有 SQL 注入漏洞",
			"current_step": map[string]any{
				"id":     "step-2",
				"step":   "逐文件检查 SQL 拼接和参数化查询",
				"status": "completed",
			},
			"step_outcome": outcome,
			"task_plan": []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "收集项目结构", Status: builtin_tools.PlanStepCompleted},
				{ID: "step-2", Step: "逐文件检查 SQL", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-1"}},
				{ID: "step-3", Step: "汇总报告", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-2"}},
			},
			"step_outcomes": []*builtin_tools.StepOutcome{
				snapshot.StepOutcomes[0],
				outcome,
			},
			"warnings":           []string{"user_repo.go 存在高危 SQL 注入"},
			"unresolved":         []string{"middleware 层输入校验未确认"},
			"step_result_path":   "/workspace/steps/step-2/attempts/001/result.json",
			"step_contexts_path": "/workspace/step_contexts.jsonl",
		})
		if err != nil {
			t.Fatalf("BuildStepReplanPrompt failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "05_step_replan_phase", prompt)

		// Verify all sections render with real data
		mustContainAll(t, "step_replan", prompt, []string{
			"<CURRENT_GOAL>",
			"SQL 注入漏洞",
			"<CURRENT_STEP>",
			"step-2",
			"<STEP_OUTCOME>",
			"已检查 8 个 db.Raw",
			"user_repo.go:45",
			`rg(\"db.Raw\")`,
			"<TASK_PLAN>",
			"step-3",
			"<STEP_OUTCOMES>",
			"<WARNINGS>",
			"高危 SQL 注入",
			"<UNRESOLVED>",
			"middleware",
		})

		// Verify file paths render
		mustContainAll(t, "step_replan_paths", prompt, []string{
			"/workspace/steps/step-2/attempts/001/result.json",
			"/workspace/step_contexts.jsonl",
		})

		// Verify STEP_OUTCOME is valid JSON, not double-serialized
		assertValidJSON(t, "step_replan", "STEP_OUTCOME", prompt)
		mustNotContain(t, "step_replan_no_double_serialize", prompt, []string{
			`"{\"`,
			`\"}"`,
		})

		// Verify STEP_OUTCOMES is valid JSON
		assertValidJSON(t, "step_replan", "STEP_OUTCOMES", prompt)

		// Verify TASK_PLAN is valid JSON
		assertValidJSON(t, "step_replan", "TASK_PLAN", prompt)
	})

	// ───────────────────────────────────────────────
	// 6. StepReplan — fast path (skip LLM) note
	// ───────────────────────────────────────────────
	// shouldSkipReplanLLM is unexported, tested indirectly via Execute tests.
	// Fast path fires when: outcome.Status==completed && no open_questions && no warnings && no unresolved.
	// In that case no prompt is built — nothing to dump.

	// ───────────────────────────────────────────────
	// 7. FinalAnswer phase prompt — task completed
	// ───────────────────────────────────────────────
	t.Run("final_answer_completed", func(t *testing.T) {
		agent, err := NewReActAgent(
			"final-answer-test",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		completedOutcomes := []*builtin_tools.StepOutcome{
			{
				StepID:       "step-1",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "已收集项目结构",
				KeyFacts:     []string{"45 个 .go 文件", "Gin + GORM 架构"},
				ToolCallsDigest: []string{
					"list_files → 45 files",
				},
				References:  []string{"shared/step-1.result.json"},
				SummaryFile: "shared/step-1.summary.md",
				ContextKey:  "audit:1:step-1",
				UpdatedAt:   time.Now().Add(-5 * time.Minute),
			},
			{
				StepID:       "step-2",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "发现 3 处 SQL 注入漏洞",
				LongSummary:  "在 user_repo.go、order_repo.go、search_handler.go 中各发现一处直接拼接用户输入到 SQL 的漏洞。",
				KeyFacts: []string{
					"user_repo.go:45 — WHERE id = " + `"` + " + userID",
					"order_repo.go:82 — ORDER BY " + `"` + " + sortField",
					"search_handler.go:31 — db.Raw(query)",
				},
				ToolCallsDigest: []string{
					"rg(\"db.Raw|db.Exec\") → 8 matches",
					"read_file(user_repo.go) → SQL injection confirmed",
				},
				References:  []string{"shared/step-2.result.json"},
				ContextKey:  "audit:1:step-2",
				UpdatedAt:   time.Now().Add(-2 * time.Minute),
			},
			{
				StepID:         "step-3",
				Status:         builtin_tools.StepOutcomeCompleted,
				ShortSummary:   "报告已生成",
				DisplayResult:  "## SQL 注入审计报告\n\n共发现 3 处高危漏洞...",
				References:     []string{"shared/audit_report.md"},
				ContextKey:     "audit:1:step-3",
				UpdatedAt:      time.Now(),
			},
		}

		plan := []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "收集项目结构", Status: builtin_tools.PlanStepCompleted},
			{ID: "step-2", Step: "逐文件检查 SQL", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-1"}},
			{ID: "step-3", Step: "汇总报告", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-2"}},
		}

		prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
			"status":      builtin_tools.TaskStatusRunning,
			"state_error": "",
			"input_timeline": []*builtin_tools.TimelineInput{
				{Content: "请对 /repo/project 进行 SQL 注入审计", CreatedAt: time.Now().Add(-10 * time.Minute)},
			},
			"show_plan":     true,
			"plan":          plan,
			"plan_version":  1,
			"step_outcomes": completedOutcomes,
			"warnings":      []string{"user_repo.go 高危"},
			"unresolved":    []string{},
		})
		if err != nil {
			t.Fatalf("BuildFinalAnswerPrompt failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "06_final_answer_completed", prompt)

		mustContainAll(t, "final_answer", prompt, []string{
			"<STATUS>",
			"<INPUT_TIMELINE>",
			"SQL 注入审计",
			"<PLAN_VERSION>",
			"<PLAN>",
			"step-1",
			"step-2",
			"step-3",
			"<STEP_OUTCOMES>",
			"已收集项目结构",
			"发现 3 处 SQL 注入漏洞",
			"报告已生成",
			"<WARNINGS>",
			"高危",
		})

		// Verify step outcomes contain key_facts and tool_calls_digest
		mustContainAll(t, "final_answer_detail", prompt, []string{
			"user_repo.go:45",
			"audit_report.md",
		})

		assertValidJSON(t, "final_answer", "STEP_OUTCOMES", prompt)
		assertValidJSON(t, "final_answer", "PLAN", prompt)
		assertValidJSON(t, "final_answer", "INPUT_TIMELINE", prompt)

		// STATUS is now a plain string (not JSON-quoted), verify it renders cleanly
		mustContainAll(t, "final_answer_status_unquoted", prompt, []string{"running"})
		mustNotContain(t, "final_answer_status_no_quotes", prompt, []string{
			`<STATUS>
"running"`,
		})
	})

	// ───────────────────────────────────────────────
	// 8. FinalAnswer — no plan (direct response scenario)
	// ───────────────────────────────────────────────
	t.Run("final_answer_no_plan", func(t *testing.T) {
		agent, err := NewReActAgent(
			"final-no-plan",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
			"status":      builtin_tools.TaskStatusRunning,
			"state_error": "",
			"input_timeline": []*builtin_tools.TimelineInput{
				{Content: "你好", CreatedAt: time.Now()},
			},
			"show_plan":     false,
			"plan":          []*builtin_tools.PlanItem{},
			"plan_version":  0,
			"step_outcomes": []*builtin_tools.StepOutcome{},
			"warnings":      []string{},
			"unresolved":    []string{},
		})
		if err != nil {
			t.Fatalf("BuildFinalAnswerPrompt no-plan failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "07_final_answer_no_plan", prompt)

		// Plan section should be hidden
		mustNotContain(t, "final_no_plan", prompt, []string{
			"<PLAN_VERSION>",
			"<PLAN>",
		})

		mustContainAll(t, "final_no_plan", prompt, []string{
			"<STATUS>",
			"<INPUT_TIMELINE>",
			"你好",
		})
	})

	// ───────────────────────────────────────────────
	// 9. FinalAnswer — with error state
	// ───────────────────────────────────────────────
	t.Run("final_answer_error_state", func(t *testing.T) {
		agent, err := NewReActAgent(
			"final-error",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
			"status":      builtin_tools.TaskStatusFailed,
			"state_error": "step-2 执行超时：读取大文件时 context deadline exceeded",
			"input_timeline": []*builtin_tools.TimelineInput{
				{Content: "请审计代码", CreatedAt: time.Now()},
			},
			"show_plan":    true,
			"plan":         []*builtin_tools.PlanItem{{ID: "step-1", Step: "审计", Status: builtin_tools.PlanStepFailed}},
			"plan_version": 1,
			"step_outcomes": []*builtin_tools.StepOutcome{
				{
					StepID:       "step-1",
					Status:       builtin_tools.StepOutcomeFailed,
					ShortSummary: "超时失败",
					Error:        "context deadline exceeded",
					UpdatedAt:    time.Now(),
				},
			},
			"warnings":   []string{"执行超时"},
			"unresolved": []string{"审计未完成"},
		})
		if err != nil {
			t.Fatalf("BuildFinalAnswerPrompt error state failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "08_final_answer_error_state", prompt)

		mustContainAll(t, "final_error", prompt, []string{
			"<STATE_ERROR>",
			"context deadline exceeded",
			"超时失败",
		})
	})

	// ───────────────────────────────────────────────
	// 10. StepReplan — outcome as StepOutcome struct (production path)
	//     vs outcome as map (also valid)
	// ───────────────────────────────────────────────
	t.Run("step_replan_struct_vs_map_outcome", func(t *testing.T) {
		agent, err := NewReActAgent(
			"replan-struct-test",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		structOutcome := &builtin_tools.StepOutcome{
			StepID:       "step-1",
			Status:       builtin_tools.StepOutcomeCompleted,
			ShortSummary: "struct outcome summary",
			KeyFacts:     []string{"fact-from-struct"},
		}

		mapOutcome := map[string]any{
			"step_id":       "step-1",
			"status":        "completed",
			"short_summary": "map outcome summary",
			"key_facts":     []string{"fact-from-map"},
		}

		basePayload := map[string]any{
			"current_goal":       "test goal",
			"current_step":       map[string]any{"id": "step-1", "step": "test step"},
			"task_plan":          []any{},
			"step_outcomes":      []any{},
			"warnings":           []string{},
			"unresolved":         []string{},
			"step_result_path":   "",
			"step_contexts_path": "",
		}

		// Test with struct
		p1 := copyPayload(basePayload)
		p1["step_outcome"] = structOutcome
		prompt1, err := agent.BuildStepReplanPrompt(p1)
		if err != nil {
			t.Fatalf("struct outcome prompt failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "09a_step_replan_struct_outcome", prompt1)

		// Test with map
		p2 := copyPayload(basePayload)
		p2["step_outcome"] = mapOutcome
		prompt2, err := agent.BuildStepReplanPrompt(p2)
		if err != nil {
			t.Fatalf("map outcome prompt failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "09b_step_replan_map_outcome", prompt2)

		// Both should produce valid JSON in STEP_OUTCOME
		assertValidJSON(t, "struct_outcome", "STEP_OUTCOME", prompt1)
		assertValidJSON(t, "map_outcome", "STEP_OUTCOME", prompt2)

		mustContainAll(t, "struct_outcome", prompt1, []string{"struct outcome summary", "fact-from-struct"})
		mustContainAll(t, "map_outcome", prompt2, []string{"map outcome summary", "fact-from-map"})

		// Neither should have double-serialization artifacts
		for _, p := range []string{prompt1, prompt2} {
			mustNotContain(t, "no_double_serial", p, []string{`"{\"`, `\"}"`})
		}
	})

	// ───────────────────────────────────────────────
	// 11. StepReplan — empty/nil edge cases
	// ───────────────────────────────────────────────
	t.Run("step_replan_empty_fields", func(t *testing.T) {
		agent, err := NewReActAgent(
			"replan-empty-test",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		prompt, err := agent.BuildStepReplanPrompt(map[string]any{
			"current_goal": "",
			"current_step": map[string]any{"id": "step-1", "step": ""},
			"step_outcome": map[string]any{
				"status": "completed",
			},
			"task_plan":          []any{},
			"step_outcomes":      []any{},
			"warnings":           []string{},
			"unresolved":         []string{},
			"step_result_path":   "",
			"step_contexts_path": "",
		})
		if err != nil {
			t.Fatalf("BuildStepReplanPrompt empty fields failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "10_step_replan_empty_fields", prompt)

		// Should still render all sections, just with empty/null data
		mustContainAll(t, "replan_empty", prompt, []string{
			"<CURRENT_GOAL>",
			"<STEP_OUTCOME>",
			"<TASK_PLAN>",
		})

		// Empty paths: "可主动读取的文件" section should NOT render
		mustNotContain(t, "replan_empty_no_file_section", prompt, []string{
			"可主动读取的文件",
			"step result 文件",
			"step contexts 文件",
		})
	})

	// ───────────────────────────────────────────────
	// 12. Step phase — with handoff/extra context
	// ───────────────────────────────────────────────
	t.Run("step_phase_with_handoff", func(t *testing.T) {
		agent, err := NewReActAgent(
			"handoff-step",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
			WithInstruction("你是安全审计 Agent"),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		agent.ReplaceState(builtin_tools.StateSnapshot{
			Phase:         builtin_tools.AgentPhaseStep,
			Status:        builtin_tools.TaskStatusRunning,
			CurrentGoal:   "检查代码",
			CurrentStepID: "step-1",
			Plan: []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "检查代码", Status: builtin_tools.PlanStepInProgress},
			},
		})

		prompt := agent.BuildThinkActPrompt(context.Background(),
			"来自上游 Agent 的交接：已完成 SAST 扫描，请重点检查 user_repo.go 中第 45 行的 SQL 拼接",
			nil,
		)
		dumpPrompt(t, dumpDir, "11_step_phase_with_handoff", prompt)

		mustContainAll(t, "handoff", prompt, []string{
			"交接上下文",
			"来自上游 Agent",
			"user_repo.go",
			"第 45 行",
		})
	})

	// ───────────────────────────────────────────────
	// 13. FinalAnswer — multi-input timeline (resume scenario)
	// ───────────────────────────────────────────────
	t.Run("final_answer_multi_input_resume", func(t *testing.T) {
		agent, err := NewReActAgent(
			"resume-test",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		now := time.Now()
		prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
			"status":      builtin_tools.TaskStatusRunning,
			"state_error": "",
			"input_timeline": []*builtin_tools.TimelineInput{
				{Content: "请审计 /repo/project 的 SQL 注入", CreatedAt: now.Add(-30 * time.Minute)},
				{Content: "另外也检查一下 XSS 漏洞", CreatedAt: now.Add(-20 * time.Minute)},
				{Content: "优先级以 SQL 注入为主", CreatedAt: now.Add(-15 * time.Minute)},
			},
			"show_plan": true,
			"plan": []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "收集项目结构", Status: builtin_tools.PlanStepCompleted},
				{ID: "step-2", Step: "检查 SQL 注入", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-1"}},
				{ID: "step-3", Step: "检查 XSS", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-1"}},
			},
			"plan_version": 2,
			"step_outcomes": []*builtin_tools.StepOutcome{
				{StepID: "step-1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "结构已收集", UpdatedAt: now.Add(-25 * time.Minute)},
				{StepID: "step-2", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "发现 3 处 SQL 注入", UpdatedAt: now.Add(-10 * time.Minute)},
				{StepID: "step-3", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "发现 1 处 XSS", UpdatedAt: now.Add(-5 * time.Minute)},
			},
			"warnings":   []string{},
			"unresolved": []string{},
		})
		if err != nil {
			t.Fatalf("BuildFinalAnswerPrompt resume failed: %v", err)
		}
		dumpPrompt(t, dumpDir, "12_final_answer_multi_input_resume", prompt)

		// All 3 user inputs should appear in timeline
		mustContainAll(t, "resume_timeline", prompt, []string{
			"SQL 注入",
			"XSS 漏洞",
			"优先级以 SQL 注入为主",
		})

		// Plan version should be 2 (replan happened)
		mustContainAll(t, "resume_plan_version", prompt, []string{
			"2",
		})

		assertValidJSON(t, "resume", "INPUT_TIMELINE", prompt)
	})

	// ───────────────────────────────────────────────
	// 14. Cross-phase JSON consistency check
	// ───────────────────────────────────────────────
	t.Run("cross_phase_json_consistency", func(t *testing.T) {
		agent, err := NewReActAgent(
			"json-consistency",
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
		)
		if err != nil {
			t.Fatalf("NewReActAgent failed: %v", err)
		}

		// A single StepOutcome object — check it renders consistently
		// across step_replan and final_answer
		sharedOutcome := &builtin_tools.StepOutcome{
			StepID:       "step-X",
			Status:       builtin_tools.StepOutcomeCompleted,
			ShortSummary: "跨阶段一致性测试",
			KeyFacts:     []string{"fact-α", "fact-β"},
			ToolCallsDigest: []string{
				"rg('pattern with special chars: α β') → 3 matches",
			},
			ContextKey: "ns:1:step-X",
			UpdatedAt:  time.Now(),
		}

		// step_replan prompt
		replanPrompt, err := agent.BuildStepReplanPrompt(map[string]any{
			"current_goal":       "consistency",
			"current_step":       map[string]any{"id": "step-X", "step": "一致性测试"},
			"step_outcome":       sharedOutcome,
			"task_plan":          []any{},
			"step_outcomes":      []*builtin_tools.StepOutcome{sharedOutcome},
			"warnings":           []string{},
			"unresolved":         []string{},
			"step_result_path":   "",
			"step_contexts_path": "",
		})
		if err != nil {
			t.Fatalf("consistency replan failed: %v", err)
		}

		// final_answer prompt
		finalPrompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
			"status":         builtin_tools.TaskStatusRunning,
			"state_error":    "",
			"input_timeline": []*builtin_tools.TimelineInput{{Content: "test", CreatedAt: time.Now()}},
			"show_plan":      false,
			"plan":           []*builtin_tools.PlanItem{},
			"plan_version":   1,
			"step_outcomes":  []*builtin_tools.StepOutcome{sharedOutcome},
			"warnings":       []string{},
			"unresolved":     []string{},
		})
		if err != nil {
			t.Fatalf("consistency final failed: %v", err)
		}

		dumpPrompt(t, dumpDir, "13a_consistency_step_replan", replanPrompt)
		dumpPrompt(t, dumpDir, "13b_consistency_final_answer", finalPrompt)

		// Both should contain the special chars without corruption
		for _, p := range []struct {
			name   string
			prompt string
		}{
			{"replan", replanPrompt},
			{"final", finalPrompt},
		} {
			mustContainAll(t, p.name+"_consistency", p.prompt, []string{
				"fact-α",
				"fact-β",
				"step-X",
				"跨阶段一致性测试",
				"rg('pattern with special chars",
			})
		}
	})

	t.Logf("\n=== ALL PROMPTS DUMPED TO: %s ===", dumpDir)
	t.Logf("To review: ls -la %s", dumpDir)
}

func copyPayload(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
