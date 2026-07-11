package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// system 部分跨 step 字节稳定：缓存前缀命中的前提。
func TestPromptSplit_SystemStableAcrossSteps(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	build := func(stepID, stepText string) PromptParts {
		parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
			RunFlags:          RunFlags{CanSpawnSubAgent: true},
			GoalUnderstanding: "核心目标：测试",
			CurrentStep:       map[string]any{"id": stepID, "step": stepText},
			HasCurrentStep:    true,
		})
		if err != nil {
			t.Fatalf("build think_act (%s) failed: %v", stepID, err)
		}
		return parts
	}

	p1 := build("step-1", "收集证据")
	p2 := build("step-2", "验证调用链")

	if p1.SystemRules != p2.SystemRules {
		t.Fatal("think_act SystemRules must be byte-stable across steps")
	}
	if p1.User == p2.User {
		t.Fatal("think_act User part must vary with current step")
	}
}

// 出站消息结构恒为 system block×N + 首条 user message + stepHistory。
func TestBuildOutboundMsgs_Order(t *testing.T) {
	history := []*ai.MsgInfo{ai.NewAIMsgInfo("prev"), ai.NewUserMsgInfo("tool feedback")}
	msgs := buildOutboundMsgs(PromptParts{
		SystemRules: "rules",
		SystemAgent: "identity+env",
		User:        "task input",
	}, history)

	wantRoles := []string{"system", "system", "user", "assistant", "user"}
	if len(msgs) != len(wantRoles) {
		t.Fatalf("expected %d msgs, got %d", len(wantRoles), len(msgs))
	}
	for i, role := range wantRoles {
		if msgs[i].Role != role {
			t.Fatalf("msg[%d] role = %q, want %q", i, msgs[i].Role, role)
		}
	}
	if msgs[0].Content != "rules" || msgs[1].Content != "identity+env" || msgs[2].Content != "task input" {
		t.Fatalf("unexpected msg contents: %v %v %v", msgs[0].Content, msgs[1].Content, msgs[2].Content)
	}

	// 辅助 prompt 兼容路径：单 system、无 user。
	degraded := buildOutboundMsgs(PromptParts{SystemRules: "only system"}, nil)
	if len(degraded) != 1 || degraded[0].Role != "system" {
		t.Fatalf("expected single system msg for system-only parts, got %v", degraded)
	}
}

// 缓存 key 只随 system 变化，不随 user 变化（user 是任务动态输入，不参与前缀语义标识）。
func TestBuildPromptRequestOptions_KeyTracksSystemOnly(t *testing.T) {
	agent, err := NewReActAgent("cache-key-test", &noopChatClientForCacheTest{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	base := PromptParts{SystemRules: "rules", SystemAgent: "env", User: "input-a"}
	sameSystem := PromptParts{SystemRules: "rules", SystemAgent: "env", User: "input-b"}
	diffSystem := PromptParts{SystemRules: "rules-changed", SystemAgent: "env", User: "input-a"}

	k1 := agent.buildPromptRequestOptions("think_act", base, true).PromptCacheKey
	k2 := agent.buildPromptRequestOptions("think_act", sameSystem, true).PromptCacheKey
	k3 := agent.buildPromptRequestOptions("think_act", diffSystem, true).PromptCacheKey

	if k1 == "" {
		t.Fatal("expected non-empty cache key")
	}
	if k1 != k2 {
		t.Fatalf("cache key must not vary with user part: %q vs %q", k1, k2)
	}
	if k1 == k3 {
		t.Fatal("cache key must vary with system part")
	}
}

// Layer-1 截断对 skill 工具的 tool result 免截断：加载当 step 内它是指令全文唯一来源。
func TestShortenOldToolResults_SkillToolExempt(t *testing.T) {
	long := strings.Repeat("指令正文", 200)

	mkRound := func(callID, toolName, result string) []*ai.MsgInfo {
		call := ai.NewAIMsgInfo("")
		call.ToolCalls = []*ai.FunctionTool{{
			Id:       callID,
			Type:     "function",
			Function: &ai.FunctionDetail{Name: toolName, Arguments: "{}"},
		}}
		return []*ai.MsgInfo{call, ai.NewToolCallResultMsgInfo(result, callID)}
	}

	history := append(append(
		mkRound("c1", builtin_tools.SkillToolName, long),
		mkRound("c2", "bash", long)...),
		mkRound("c3", "read_file", "recent")...)

	shortened, did := shortenOldToolResults(history, 1, 64)
	if !did {
		t.Fatal("expected shortening to happen for old non-skill tool results")
	}

	byCallID := map[string]string{}
	for _, msg := range shortened {
		if msg.Role == "tool" {
			if s, ok := msg.Content.(string); ok {
				byCallID[msg.ToolCallID] = s
			}
		}
	}
	if byCallID["c1"] != strings.TrimSpace(long) {
		t.Fatalf("skill tool result must stay intact, got %d bytes", len(byCallID["c1"]))
	}
	if !strings.Contains(byCallID["c2"], stepHistoryToolResultShortenedHint) {
		t.Fatalf("non-skill old tool result should be shortened, got %q", byCallID["c2"])
	}
	if byCallID["c3"] != "recent" {
		t.Fatalf("last-round tool result must stay intact, got %q", byCallID["c3"])
	}
}

// 首条 user message 按 step 入口冻结：mid-step skill 加载不改写 U（靠 tool result 通道），
// 跨 step 重建并反映最新 ActiveSkillNames。
func TestThinkActPartsForStep_FrozenWithinStep(t *testing.T) {
	agent, err := NewReActAgent(
		"freeze-test",
		&noopChatClientForCacheTest{},
		WithEmitter(NewDummyEmitter()),
		WithSkillsPromptProvider(SkillsPromptProviderFunc(func(_ context.Context, _ string, snapshot builtin_tools.StateSnapshot) (*SkillsPromptContext, error) {
			ctx := &SkillsPromptContext{Table: "| demo-skill | 演示 | available |"}
			if len(snapshot.ActiveSkillNames) > 0 {
				ctx.Injected = "#### demo-skill\ninjected-skill-body"
			}
			return ctx, nil
		})),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	snap := builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		Status:        builtin_tools.TaskStatusRunning,
		CurrentStepID: "s1",
		PlanVersion:   1,
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "第一步", Status: builtin_tools.PlanStepInProgress},
			{ID: "s2", Step: "第二步", Status: builtin_tools.PlanStepPending, DependsOn: []string{"s1"}},
		},
	}
	agent.ReplaceState(snap)
	p1 := agent.thinkActPartsForStep(context.Background(), "", agent.state.Snapshot())
	if strings.Contains(p1.User, "injected-skill-body") {
		t.Fatal("step entry snapshot should not contain not-yet-loaded skill")
	}

	// mid-step 加载 skill：U 冻结不变。
	snap.ActiveSkillNames = []string{"demo-skill"}
	agent.ReplaceState(snap)
	p2 := agent.thinkActPartsForStep(context.Background(), "", agent.state.Snapshot())
	if p2.User != p1.User {
		t.Fatal("user part must stay frozen within the same step")
	}

	// 跨 step：重建并反映最新 ActiveSkillNames。
	snap.CurrentStepID = "s2"
	snap.Plan[0].Status = builtin_tools.PlanStepCompleted
	snap.Plan[1].Status = builtin_tools.PlanStepInProgress
	agent.ReplaceState(snap)
	p3 := agent.thinkActPartsForStep(context.Background(), "", agent.state.Snapshot())
	if p3.User == p1.User {
		t.Fatal("user part must be rebuilt on step switch")
	}
	if !strings.Contains(p3.User, "injected-skill-body") {
		t.Fatal("next step's user part must reflect loaded skills")
	}
}
