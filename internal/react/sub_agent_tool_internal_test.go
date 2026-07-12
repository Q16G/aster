package react

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// TestMutateWorkspaceState_ConcurrentNoLostUpdate 锁 P6b 全局 state RMW 锁的**逻辑正确性**：
// N goroutine 并发各写一条 ChildAgents，收尾后全部都在——`-race` 抓不到「读全量-改一区-写全量」
// 的丢更新，只有这种确定性断言能守住锁失效导致的跨区丢失。
func TestMutateWorkspaceState_ConcurrentNoLostUpdate(t *testing.T) {
	rt, err := newLocalWorkspaceRuntime("", t.TempDir(), "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("c-%d", i)
			_ = rt.MutateWorkspaceState(func(s *builtin_tools.WorkspaceState) error {
				s.ChildAgents[id] = &builtin_tools.WorkspaceChildAgentPointer{Status: "running", ParentStepKey: id}
				return nil
			})
		}(i)
	}
	wg.Wait()

	st, err := rt.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(st.ChildAgents) != n {
		t.Fatalf("lost update: expected %d ChildAgents, got %d", n, len(st.ChildAgents))
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c-%d", i)
		if st.ChildAgents[id] == nil {
			t.Fatalf("missing %s — RMW lost update", id)
		}
	}
}

func TestPreRegisterChildAgent_CreatesRunningEntry(t *testing.T) {
	parentRoot := t.TempDir()
	parentRuntime, err := newLocalWorkspaceRuntime("sess-1", parentRoot, "root")
	if err != nil {
		t.Fatalf("create parent runtime: %v", err)
	}

	parent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	parent.workspaceRuntime = parentRuntime

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(parent, factory)

	runtime := builtin_tools.ToolRuntimeInfo{CurrentStepID: "step-3"}
	tool.preRegisterChildAgent(runtime, "sub-abc123", "/tmp/ws/sub_agents/sub-abc123")

	state, err := parentRuntime.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	ptr := state.ChildAgents["sub-abc123"]
	if ptr == nil {
		t.Fatal("expected ChildAgents entry for sub-abc123")
	}
	if ptr.Status != "running" {
		t.Fatalf("expected status=running, got %q", ptr.Status)
	}
	if ptr.ParentStepKey != "step-3" {
		t.Fatalf("expected ParentStepKey=step-3, got %q", ptr.ParentStepKey)
	}
	if ptr.ArtifactRootDir != "/tmp/ws/sub_agents/sub-abc123" {
		t.Fatalf("expected ArtifactRootDir, got %q", ptr.ArtifactRootDir)
	}
}

func TestFinalizeChildAgent_UpdatesStatus(t *testing.T) {
	parentRoot := t.TempDir()
	parentRuntime, err := newLocalWorkspaceRuntime("sess-1", parentRoot, "root")
	if err != nil {
		t.Fatalf("create parent runtime: %v", err)
	}

	parent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	parent.workspaceRuntime = parentRuntime

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(parent, factory)

	runtime := builtin_tools.ToolRuntimeInfo{CurrentStepID: "step-5"}
	tool.preRegisterChildAgent(runtime, "sub-xyz", "/tmp/ws/sub_agents/sub-xyz")

	cases := []struct {
		name       string
		result     *builtin_tools.RunResult
		wantStatus string
	}{
		{"success", &builtin_tools.RunResult{Success: true, Result: "done"}, "completed"},
		{"failure", &builtin_tools.RunResult{Success: false, Error: "boom"}, "failed"},
		{"nil result", nil, "failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool.finalizeChildAgent(runtime, "sub-xyz", "/tmp/ws/sub_agents/sub-xyz", tc.result)

			state, err := parentRuntime.LoadWorkspaceState()
			if err != nil {
				t.Fatalf("load state: %v", err)
			}
			ptr := state.ChildAgents["sub-xyz"]
			if ptr == nil {
				t.Fatal("expected ChildAgents entry")
			}
			if ptr.Status != tc.wantStatus {
				t.Fatalf("expected status=%q, got %q", tc.wantStatus, ptr.Status)
			}
		})
	}
}

func TestPreRegisterChildAgent_NilParentRuntime(t *testing.T) {
	parent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(parent, factory)

	// Should not panic
	tool.preRegisterChildAgent(builtin_tools.ToolRuntimeInfo{}, "sub-x", "/tmp/x")
}

func TestChildAgentToken(t *testing.T) {
	cases := []struct {
		name   string
		callID string
		want   string
	}{
		{"plain", "call_00_abcd", "call_00_abcd"},
		{"keeps full suffix", "call_07_zzzz9999", "call_07_zzzz9999"},
		{"sanitizes dashes", "call-7-uuid-xyz", "call_7_uuid_xyz"},
		{"sanitizes other", "id/with.dots:colon", "id_with_dots_colon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := childAgentToken(tc.callID); got != tc.want {
				t.Fatalf("childAgentToken(%q) = %q, want %q", tc.callID, got, tc.want)
			}
		})
	}

	// Empty call_id must fall back to a non-empty random token.
	if got := childAgentToken(""); got == "" {
		t.Fatal("empty call_id must produce a random token, got empty")
	}

	// Regression: two call_ids sharing an 8-char prefix must NOT collapse to the
	// same token (the old truncateID(callID, 8) bug collapsed both to "call_00_").
	a := childAgentToken("call_00_aaaa")
	b := childAgentToken("call_00_bbbb")
	if a == b {
		t.Fatalf("tokens for distinct call_ids collided: %q == %q", a, b)
	}
}

// TestBuildChild_NoCollisionOnSharedCallIDPrefix is the end-to-end regression
// for the childName collision: two sub_agent spawns whose call_ids share the
// "call_00_" provider prefix must get distinct childNames (hence distinct
// workspace dirs / registry slots / durable pointers).
func TestBuildChild_NoCollisionOnSharedCallIDPrefix(t *testing.T) {
	parent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	parent.workspaceRootDir = t.TempDir()
	parent.sourceWorkingDir = "/repo/source"
	parent.runtimeRepoContext = RuntimeRepoContext{
		SourceWorkingDir: "/repo/source",
		RepoRootDir:      "/repo/source",
		IsGitRepo:        true,
		Branch:           "feature/root",
		IsWorktree:       true,
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(parent, factory)

	args := map[string]any{"instruction": "do work"}
	setupA, err := tool.buildChild(context.Background(), args, builtin_tools.ToolRuntimeInfo{CallID: "call_00_aaaa"})
	if err != nil {
		t.Fatalf("buildChild A: %v", err)
	}
	setupB, err := tool.buildChild(context.Background(), args, builtin_tools.ToolRuntimeInfo{CallID: "call_00_bbbb"})
	if err != nil {
		t.Fatalf("buildChild B: %v", err)
	}

	if setupA.childName == setupB.childName {
		t.Fatalf("childName collision: both = %q", setupA.childName)
	}
	if setupA.childRootDir == setupB.childRootDir {
		t.Fatalf("childRootDir collision: both = %q", setupA.childRootDir)
	}
	if !strings.HasPrefix(setupA.childName, "sub-") {
		t.Fatalf("expected sub- prefix, got %q", setupA.childName)
	}
	if len(setupA.execOpts) == 0 {
		t.Fatalf("expected child exec options to be populated")
	}
	cfg := &ExecuteConfig{}
	for _, opt := range setupA.execOpts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.sourceWorkingDir != "/repo/source" {
		t.Fatalf("expected child to inherit source working dir, got %q", cfg.sourceWorkingDir)
	}
}

// TestBuildChild_InheritsParentSessionID locks the invariant that a sub-agent
// runs under the SAME workspace session id as its parent (== Task.TaskID). Risk
// reporting (report_web_risk / report_code_risk) resolves the owning task by this
// session id (rt.WorkspaceSessionID), so a child that inherited a different or
// empty session would silently drop every risk it reports. The parent's RunID is
// deliberately NOT propagated to children (a child gets a fresh turn-id) — that is
// exactly why the stable session id, not the RunID, must be the resolution key.
func TestBuildChild_InheritsParentSessionID(t *testing.T) {
	const parentSession = "tsk_parent12"

	parent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	parent.workspaceRootDir = t.TempDir()
	parent.workspaceSessionID = parentSession

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(parent, factory)

	setup, err := tool.buildChild(context.Background(),
		map[string]any{"instruction": "do work"},
		builtin_tools.ToolRuntimeInfo{CallID: "call_00_aaaa"})
	if err != nil {
		t.Fatalf("buildChild: %v", err)
	}

	// Apply the child's exec options and read the workspace runtime it will run
	// under. Its SessionID becomes the child agent's workspaceSessionID
	// (agent_execute.go: a.workspaceSessionID = workspaceRuntime.SessionID()),
	// which is what the child's tool runtime exposes as WorkspaceSessionID.
	cfg := &ExecuteConfig{}
	for _, opt := range setup.execOpts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.workspaceRuntime == nil {
		t.Fatal("child exec options must set a workspace runtime")
	}
	if got := cfg.workspaceRuntime.SessionID(); got != parentSession {
		t.Fatalf("child session id = %q, want parent's %q (risk ingest would fail to resolve the task)", got, parentSession)
	}
}

type stubClient struct{}

func (s *stubClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (s *stubClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}
func (s *stubClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestResolveChildToolNames_FiltersPolicyManagedTools(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	parent, err := NewReActAgent("parent", &stubClient{},
		WithEmitter(NewDummyEmitter()),
		WithBashTool(bashCfg),
		WithTools(builtin_tools.NewReadFileTool()),
	)
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}

	registry := NewDefaultToolRegistry()
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(registry),
	)

	sub := NewSubAgentTool(parent, factory)

	tests := []struct {
		name      string
		requested []string
		wantIn    []string
		wantOut   []string
	}{
		{
			name:      "bash filtered from explicit request",
			requested: []string{"bash", "read_file"},
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash"},
		},
		{
			name:      "all policy-managed tools filtered",
			requested: []string{"bash", "sub_agent", "update_current_step", "task_status", "human_confirm", "skill", "read_file"},
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash", "sub_agent", "update_current_step", "task_status", "human_confirm", "skill"},
		},
		{
			name:      "registry tools pass through",
			requested: []string{"read_file", "list_files", "rg"},
			wantIn:    []string{"read_file", "list_files", "rg"},
			wantOut:   nil,
		},
		{
			name:      "empty request inherits domain tools and excludes platform tools",
			requested: nil,
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash", "sub_agent", "update_current_step", "task_status", "human_confirm"},
		},
		{
			name:      "unknown tools filtered by parent+registry check",
			requested: []string{"bash", "nonexistent_tool", "read_file"},
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash", "nonexistent_tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sub.resolveChildToolNames(tt.requested)
			for _, want := range tt.wantIn {
				if !slices.Contains(got, want) {
					t.Errorf("expected %q in result %v", want, got)
				}
			}
			for _, reject := range tt.wantOut {
				if slices.Contains(got, reject) {
					t.Errorf("expected %q NOT in result %v", reject, got)
				}
			}
		})
	}
}

// TestResolveChildToolNames_AlwaysIncludesBaselineDomainTools guards the fix
// for the planner crash: a child delegated with tools:["bash"] used to end up
// with an empty ToolNames (bash is policy-stripped) and therefore lost
// read_file/list_files/rg, which the plan/replan prompts structurally rely on.
// The baseline read-only domain tools must now be present regardless of what
// the parent requested.
func TestResolveChildToolNames_AlwaysIncludesBaselineDomainTools(t *testing.T) {
	parent, err := NewReActAgent("parent", &stubClient{},
		WithEmitter(NewDummyEmitter()),
		WithTools(builtin_tools.NewReadFileTool()),
	)
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)
	sub := NewSubAgentTool(parent, factory)

	baseline := []string{
		builtin_tools.ReadFileToolName,
		builtin_tools.ListFilesToolName,
		builtin_tools.RgToolName,
	}

	t.Run("policy-only request still yields baseline", func(t *testing.T) {
		got := sub.resolveChildToolNames([]string{"bash"})
		for _, name := range baseline {
			if !slices.Contains(got, name) {
				t.Errorf("expected baseline tool %q in result %v", name, got)
			}
		}
		if slices.Contains(got, builtin_tools.BashToolName) {
			t.Errorf("bash must remain policy-stripped, got %v", got)
		}
	})

	t.Run("no duplicates when baseline already requested", func(t *testing.T) {
		got := sub.resolveChildToolNames([]string{"bash", "read_file"})
		count := 0
		for _, name := range got {
			if name == builtin_tools.ReadFileToolName {
				count++
			}
		}
		if count != 1 {
			t.Errorf("read_file must appear exactly once, got %d in %v", count, got)
		}
		for _, name := range baseline {
			if !slices.Contains(got, name) {
				t.Errorf("expected baseline tool %q in result %v", name, got)
			}
		}
	})

	t.Run("default inheritance path retains baseline", func(t *testing.T) {
		got := sub.resolveChildToolNames(nil)
		for _, name := range baseline {
			if !slices.Contains(got, name) {
				t.Errorf("expected baseline tool %q in default result %v", name, got)
			}
		}
	})
}

// TestResolveChildToolNames_ExcludesAgentResidentMCPTools guards the fix for
// the bug where a parent's MCP tool adapters (registered directly on the agent,
// not in the ToolRegistry) leaked into the child's ToolNames and caused
// Build's resolveTools to fail with "tool ... not registered".
func TestResolveChildToolNames_ExcludesAgentResidentMCPTools(t *testing.T) {
	const mcpToolName = "rename_payload_group"

	parent, err := NewReActAgent("parent", &stubClient{},
		WithEmitter(NewDummyEmitter()),
		WithTools(builtin_tools.NewReadFileTool()),
	)
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	// Simulate an MCP adapter: registered directly on the agent, absent from
	// the registry.
	if err := parent.registerTool(&unsafeTool{name: mcpToolName}); err != nil {
		t.Fatalf("register mcp-like tool: %v", err)
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)
	sub := NewSubAgentTool(parent, factory)

	// Explicit request: the MCP name is dropped, the registry name is kept.
	explicit := sub.resolveChildToolNames([]string{"read_file", mcpToolName})
	if slices.Contains(explicit, mcpToolName) {
		t.Errorf("explicit path must drop agent-resident %q, got %v", mcpToolName, explicit)
	}
	if !slices.Contains(explicit, builtin_tools.ReadFileToolName) {
		t.Errorf("explicit path must keep registry tool read_file, got %v", explicit)
	}

	// Default inheritance: same expectation.
	inherited := sub.parentDomainToolNames()
	if slices.Contains(inherited, mcpToolName) {
		t.Errorf("inheritance must drop agent-resident %q, got %v", mcpToolName, inherited)
	}
	if !slices.Contains(inherited, builtin_tools.ReadFileToolName) {
		t.Errorf("inheritance must keep registry tool read_file, got %v", inherited)
	}
}

// ---------------------------------------------------------------------------
// Demo: Sub-agent pending 步骤信息丢失的完整复现
// ---------------------------------------------------------------------------

// TestStepReplanDecision_BlindToSubAgentPlan 验证 StepReplan 决策逻辑看不到 sub-agent plan。
// 模拟 phase_step_replan.go:174-181 的决策：should_replan=false + root plan 全完成 → FinalAnswer。
func TestStepReplanDecision_BlindToSubAgentPlan(t *testing.T) {
	// Root plan: 7 个步骤全部完成（对应真实 session）
	rootPlan := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "全局侦察与信息收集", Status: builtin_tools.PlanStepCompleted},
		{ID: "vuln-scan", Step: "SAST 漏洞扫描", Status: builtin_tools.PlanStepCompleted},
		{ID: "auth-review", Step: "认证授权审计", Status: builtin_tools.PlanStepCompleted},
		{ID: "biz-logic", Step: "业务逻辑审计", Status: builtin_tools.PlanStepCompleted},
		{ID: "dep-audit", Step: "依赖安全审计", Status: builtin_tools.PlanStepCompleted},
		{ID: "dataflow", Step: "数据流分析", Status: builtin_tools.PlanStepCompleted},
		{ID: "summary", Step: "汇总所有发现与证据链", Status: builtin_tools.PlanStepCompleted},
	}

	// Sub-agent plan: 6 个步骤全部 pending（对 root 不可见）
	subAgentPlan := []*builtin_tools.PlanItem{
		{ID: "install-deps", Step: "安装环境依赖", Status: builtin_tools.PlanStepPending},
		{ID: "captcha-script", Step: "编写验证码识别脚本", Status: builtin_tools.PlanStepPending},
		{ID: "try-passwords", Step: "尝试弱密码登录", Status: builtin_tools.PlanStepPending},
		{ID: "hashcat", Step: "hashcat 破解密码哈希", Status: builtin_tools.PlanStepPending},
		{ID: "get-jwt", Step: "获取 JWT token", Status: builtin_tools.PlanStepPending},
		{ID: "save-token", Step: "保存 token 到工作区", Status: builtin_tools.PlanStepPending},
	}

	// 模拟 applyReplanResult 的决策逻辑 (phase_step_replan.go:174-181)
	shouldReplan := false // StepReplan 模型判断不需要重规划
	var nextPhase string
	nextRunnableStepID := ""

	if shouldReplan {
		nextPhase = "plan"
	} else if candidate := builtin_tools.NextRunnablePlanStepID(rootPlan); candidate != "" {
		nextRunnableStepID = candidate
		nextPhase = "step"
	} else {
		nextPhase = "final_answer"
	}

	t.Log("=== 信息丢失点 2: StepReplan 决策 ===")
	t.Logf("Root plan: %d 步, 全部 completed", len(rootPlan))
	t.Logf("Sub-agent plan: %d 步, 全部 pending（root 看不到）", len(subAgentPlan))
	t.Logf("NextRunnablePlanStepID(rootPlan) = %q", nextRunnableStepID)
	t.Logf("决策结果: nextPhase = %q", nextPhase)

	if nextPhase != "final_answer" {
		t.Fatalf("expected final_answer, got %q", nextPhase)
	}

	// sub-agent plan 有 6 个可运行的 pending 步骤，但 root 完全看不到
	subNextID := builtin_tools.NextRunnablePlanStepID(subAgentPlan)
	if subNextID == "" {
		t.Fatal("sub-agent plan should have runnable steps")
	}
	t.Logf("Sub-agent NextRunnablePlanStepID = %q（对 root 不可见）", subNextID)
	t.Log("结论: root 进入 FinalAnswer，sub-agent 的 6 个 pending 步骤被忽略")
}

// TestFinalizeChildAgent_NoSubAgentPlanPersisted 验证 finalizeChildAgent 只保存
// status，不保存 sub-agent 的 plan 完成统计。
func TestFinalizeChildAgent_NoSubAgentPlanPersisted(t *testing.T) {
	parentRoot := t.TempDir()
	parentRuntime, err := newLocalWorkspaceRuntime("sess-1", parentRoot, "root")
	if err != nil {
		t.Fatalf("create parent runtime: %v", err)
	}

	parent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	parent.workspaceRuntime = parentRuntime

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(parent, factory)

	runtime := builtin_tools.ToolRuntimeInfo{CurrentStepID: "auth-review"}

	// Sub-agent 返回 Success=true 但实际上 plan 有 6 个 pending 步骤
	result := &builtin_tools.RunResult{
		Success: true,
		Result:  "认证测试完成",
	}
	tool.finalizeChildAgent(runtime, "sub-cred", "/tmp/ws/sub-cred", result)

	state, err := parentRuntime.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	ptr := state.ChildAgents["sub-cred"]
	if ptr == nil {
		t.Fatal("expected ChildAgents entry")
	}

	t.Log("=== 信息丢失点 3: finalizeChildAgent ===")
	t.Logf("保存到 parent workspace 的信息:")
	t.Logf("  Status: %q", ptr.Status)
	t.Logf("  ParentStepKey: %q", ptr.ParentStepKey)
	t.Logf("  ArtifactRootDir: %q", ptr.ArtifactRootDir)

	if ptr.Status != "completed" {
		t.Fatalf("expected status=completed, got %q", ptr.Status)
	}

	t.Log("丢失: sub-agent plan 完成状态（6 步中 0 步完成）")
	t.Log("parent 只知道 child agent 'completed'（执行结束），不知道 child 的 plan 是否完成")
}


func TestParentDomainToolNames_ExcludesInheritanceBlocked(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	parent, err := NewReActAgent("parent", &stubClient{},
		WithEmitter(NewDummyEmitter()),
		WithBashTool(bashCfg),
	)
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)

	sub := NewSubAgentTool(parent, factory)
	names := sub.parentDomainToolNames()

	for _, blocked := range []string{
		builtin_tools.BashToolName,
		builtin_tools.SubAgentToolName,
		builtin_tools.SubAgentStatusToolName,
		builtin_tools.AwaitSubAgentsToolName,
		builtin_tools.UpdateCurrentStepToolName,
		builtin_tools.TaskStatusQueryToolName,
		builtin_tools.HumanConfirmToolName,
		builtin_tools.SkillToolName,
		builtin_tools.ListSkillsToolName,
		builtin_tools.EjectSkillToolName,
	} {
		if slices.Contains(names, blocked) {
			t.Errorf("parentDomainToolNames should not contain %q, got %v", blocked, names)
		}
	}
}

func TestAwaitSubAgentsTool_ExcludedFromInheritanceMaps(t *testing.T) {
	if !excludeFromInheritance[builtin_tools.AwaitSubAgentsToolName] {
		t.Error("await_subagents must be in excludeFromInheritance")
	}
	if !policyManagedTools[builtin_tools.AwaitSubAgentsToolName] {
		t.Error("await_subagents must be in policyManagedTools")
	}
}

func TestBuild_SubAgentOmitsOrchestrationTools(t *testing.T) {
	orchestrationTools := []string{
		builtin_tools.SubAgentToolName,
		builtin_tools.SubAgentStatusToolName,
		builtin_tools.AwaitSubAgentsToolName,
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)

	t.Run("sub-agent does not register or expose them", func(t *testing.T) {
		child, err := factory.Build(AgentDefinition{Name: "child", IsSubAgent: true})
		if err != nil {
			t.Fatalf("build sub-agent: %v", err)
		}
		for _, name := range orchestrationTools {
			if _, ok := child.GetTool(name); ok {
				t.Errorf("sub-agent must not register %q", name)
			}
		}
		fnTools, _ := child.BuildFunctionTools(nil, builtin_tools.AgentPhaseStep)
		for _, ft := range fnTools {
			if ft.Function == nil {
				continue
			}
			if slices.Contains(orchestrationTools, ft.Function.Name) {
				t.Errorf("sub-agent prompt must not expose %q", ft.Function.Name)
			}
		}
	})

	t.Run("root agent still registers them", func(t *testing.T) {
		root, err := factory.Build(AgentDefinition{Name: "root"})
		if err != nil {
			t.Fatalf("build root: %v", err)
		}
		for _, name := range orchestrationTools {
			if _, ok := root.GetTool(name); !ok {
				t.Errorf("root agent must register %q", name)
			}
		}
	})
}

func TestBuild_SubAgentOmitsHumanConfirm(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)

	exposesHumanConfirm := func(agent *Agent) bool {
		fnTools, _ := agent.BuildFunctionTools(nil, builtin_tools.AgentPhaseStep)
		for _, ft := range fnTools {
			if ft.Function != nil && ft.Function.Name == builtin_tools.HumanConfirmToolName {
				return true
			}
		}
		return false
	}

	t.Run("sub-agent neither registers nor exposes human_confirm", func(t *testing.T) {
		child, err := factory.Build(AgentDefinition{Name: "child", IsSubAgent: true})
		if err != nil {
			t.Fatalf("build sub-agent: %v", err)
		}
		if _, ok := child.GetTool(builtin_tools.HumanConfirmToolName); ok {
			t.Errorf("sub-agent must not register %q", builtin_tools.HumanConfirmToolName)
		}
		if exposesHumanConfirm(child) {
			t.Errorf("sub-agent prompt must not expose %q", builtin_tools.HumanConfirmToolName)
		}
	})

	t.Run("root agent registers and exposes human_confirm", func(t *testing.T) {
		root, err := factory.Build(AgentDefinition{Name: "root"})
		if err != nil {
			t.Fatalf("build root: %v", err)
		}
		if _, ok := root.GetTool(builtin_tools.HumanConfirmToolName); !ok {
			t.Errorf("root agent must register %q", builtin_tools.HumanConfirmToolName)
		}
		if !exposesHumanConfirm(root) {
			t.Errorf("root agent prompt must expose %q", builtin_tools.HumanConfirmToolName)
		}
	})
}

func TestRunningChildAgentNames(t *testing.T) {
	root := t.TempDir()
	ws, err := newLocalWorkspaceRuntime("sess-1", root, "root")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	agent, err := NewReActAgent("test", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.workspaceRuntime = ws

	state, _ := ws.LoadWorkspaceState()
	state.ChildAgents = map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"scanner": {Status: "completed"},
		"auditor": {Status: "failed"},
		"worker":  {Status: "running"},
		"pending": {Status: ""},
	}
	if err := ws.SaveWorkspaceState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	running := agent.runningChildAgentNames()
	if len(running) != 2 {
		t.Fatalf("expected 2 running agents, got %d: %v", len(running), running)
	}
	for _, name := range running {
		if name != "worker" && name != "pending" {
			t.Errorf("unexpected running agent: %s", name)
		}
	}
}

func TestRunningChildAgentNames_AllTerminated(t *testing.T) {
	root := t.TempDir()
	ws, err := newLocalWorkspaceRuntime("sess-1", root, "root")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	agent, err := NewReActAgent("test", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.workspaceRuntime = ws

	state, _ := ws.LoadWorkspaceState()
	state.ChildAgents = map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"scanner": {Status: "completed"},
		"auditor": {Status: "failed"},
	}
	if err := ws.SaveWorkspaceState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	running := agent.runningChildAgentNames()
	if len(running) != 0 {
		t.Fatalf("expected no running agents, got %v", running)
	}
}

func TestRunningChildAgentNames_NoWorkspace(t *testing.T) {
	agent, err := NewReActAgent("test", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	// workspaceRuntime is nil

	running := agent.runningChildAgentNames()
	if running != nil {
		t.Fatalf("expected nil when no workspace, got %v", running)
	}
}
