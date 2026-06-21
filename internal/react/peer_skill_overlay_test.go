package react

import (
	"context"
	"strings"
	"sync"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// 方案 B 红线：peer 路径 skill 状态走 LocalActiveSkillNames overlay 而非全局
// Agent.state.ActiveSkillNames，确保 3 个并发 peer 不互相串扰。

type peerSkillStubClient struct{}

func (s *peerSkillStubClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (s *peerSkillStubClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}
func (s *peerSkillStubClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func newPeerSkillTestAgent(t *testing.T) *Agent {
	t.Helper()
	agent, err := NewReActAgent("peer-skill-test", &peerSkillStubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	return agent
}

// TestSkill_PeerLoadDoesNotAffectGlobal 红线：peer 路径 load_skill 后只动 overlay，
// 全局 a.state.ActiveSkillNames 保持不变。
func TestSkill_PeerLoadDoesNotAffectGlobal(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:            builtin_tools.AgentPhaseStep,
		Status:           builtin_tools.TaskStatusRunning,
		ActiveSkillNames: []string{"baseline-skill"},
	})

	overlay := builtin_tools.CloneStringSlice(agent.state.Snapshot().ActiveSkillNames)
	runCtx := &InlineStepCtx{StepID: "s2", LocalActiveSkillNames: &overlay}

	agent.handleSkillToolStateSync(
		builtin_tools.SkillToolName,
		map[string]any{},
		`{"ok":true,"skills":[{"name":"peer-new"}]}`,
		"",
		runCtx,
	)

	if got := strings.Join(overlay, ","); got != "baseline-skill,peer-new" {
		t.Errorf("peer overlay should contain baseline + peer-new; got %q", got)
	}
	// 全局保持不变
	if got := strings.Join(agent.state.Snapshot().ActiveSkillNames, ","); got != "baseline-skill" {
		t.Errorf("global ActiveSkillNames MUST NOT change on peer load; got %q", got)
	}
}

// TestSkill_PeerEjectDoesNotAffectGlobal 红线：peer 路径 eject_skill 不影响全局。
func TestSkill_PeerEjectDoesNotAffectGlobal(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:            builtin_tools.AgentPhaseStep,
		Status:           builtin_tools.TaskStatusRunning,
		ActiveSkillNames: []string{"sql-injection", "auth-bypass"},
	})

	overlay := builtin_tools.CloneStringSlice(agent.state.Snapshot().ActiveSkillNames)
	runCtx := &InlineStepCtx{StepID: "s2", LocalActiveSkillNames: &overlay}

	agent.handleSkillToolStateSync(
		builtin_tools.EjectSkillToolName,
		map[string]any{"name": "sql-injection"},
		`{"ok":true,"name":"sql-injection"}`,
		"",
		runCtx,
	)

	if got := strings.Join(overlay, ","); got != "auth-bypass" {
		t.Errorf("peer overlay after eject should be auth-bypass; got %q", got)
	}
	if got := strings.Join(agent.state.Snapshot().ActiveSkillNames, ","); got != "sql-injection,auth-bypass" {
		t.Errorf("global ActiveSkillNames MUST NOT change on peer eject; got %q", got)
	}
}

// TestSkill_ConcurrentPeersIndependentOverlay **关键并发红线**：
// 3 个 goroutine 各自构造 runCtx 调 load_skill 加不同 skill，断言互不串扰。
func TestSkill_ConcurrentPeersIndependentOverlay(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:            builtin_tools.AgentPhaseStep,
		Status:           builtin_tools.TaskStatusRunning,
		ActiveSkillNames: nil,
	})

	peerNames := map[string]string{
		"s1": "sql-injection",
		"s2": "auth-bypass",
		"s3": "xss",
	}
	overlays := make(map[string]*[]string)
	for stepID := range peerNames {
		base := []string{}
		overlays[stepID] = &base
	}

	var wg sync.WaitGroup
	for stepID, skill := range peerNames {
		stepID, skill := stepID, skill
		wg.Add(1)
		go func() {
			defer wg.Done()
			runCtx := &InlineStepCtx{StepID: stepID, LocalActiveSkillNames: overlays[stepID]}
			agent.handleSkillToolStateSync(
				builtin_tools.SkillToolName,
				map[string]any{},
				`{"ok":true,"skills":[{"name":"`+skill+`"}]}`,
				"",
				runCtx,
			)
		}()
	}
	wg.Wait()

	for stepID, expectedSkill := range peerNames {
		got := *overlays[stepID]
		if len(got) != 1 || got[0] != expectedSkill {
			t.Errorf("peer %s overlay should be [%s]; got %v", stepID, expectedSkill, got)
		}
		for otherStepID, otherSkill := range peerNames {
			if otherStepID == stepID {
				continue
			}
			for _, s := range got {
				if s == otherSkill {
					t.Errorf("peer %s overlay MUST NOT contain peer %s's skill %q (cross-contamination)",
						stepID, otherStepID, otherSkill)
				}
			}
		}
	}
	// 全局应保持空
	if got := agent.state.Snapshot().ActiveSkillNames; len(got) != 0 {
		t.Errorf("global ActiveSkillNames MUST stay empty under concurrent peer loads; got %v", got)
	}
}

// TestSkill_MainPathStillWritesGlobal runCtx == nil 时走全局（原行为不变）。
func TestSkill_MainPathStillWritesGlobal(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	agent.ReplaceState(builtin_tools.StateSnapshot{Phase: builtin_tools.AgentPhaseStep})

	agent.handleSkillToolStateSync(
		builtin_tools.SkillToolName,
		map[string]any{},
		`{"ok":true,"skills":[{"name":"main-skill"}]}`,
		"",
		nil, // 主路径
	)

	if got := strings.Join(agent.state.Snapshot().ActiveSkillNames, ","); got != "main-skill" {
		t.Errorf("main path should write global ActiveSkillNames; got %q", got)
	}
}

// TestSkill_PeerWithNilOverlayFallsBackToGlobal 防御：runCtx != nil 但 LocalActiveSkillNames
// 为 nil 时退化到主路径行为（保持兼容性，避免漏初始化导致功能丢失）。
func TestSkill_PeerWithNilOverlayFallsBackToGlobal(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	agent.ReplaceState(builtin_tools.StateSnapshot{Phase: builtin_tools.AgentPhaseStep})

	runCtx := &InlineStepCtx{StepID: "s2"} // 未初始化 LocalActiveSkillNames

	agent.handleSkillToolStateSync(
		builtin_tools.SkillToolName,
		map[string]any{},
		`{"ok":true,"skills":[{"name":"fallback-skill"}]}`,
		"",
		runCtx,
	)

	// runCtx 非空但 LocalActiveSkillNames=nil → 退化主路径
	if got := strings.Join(agent.state.Snapshot().ActiveSkillNames, ","); got != "fallback-skill" {
		t.Errorf("nil overlay should fall back to global; got %q", got)
	}
}

// TestPeerScopedSnapshot_ProjectsActiveSkillNames helper 应当把 peer overlay 投影到 snapshot.ActiveSkillNames。
func TestPeerScopedSnapshot_ProjectsActiveSkillNames(t *testing.T) {
	snap := builtin_tools.StateSnapshot{
		CurrentStepID:    "main-s1",
		ActiveSkillNames: []string{"global-skill"},
	}
	overlay := []string{"peer-only"}
	runCtx := &InlineStepCtx{StepID: "s2", LocalActiveSkillNames: &overlay}

	out := peerScopedSnapshot(snap, runCtx)
	if out.CurrentStepID != "s2" {
		t.Errorf("CurrentStepID=%q, want s2", out.CurrentStepID)
	}
	if got := strings.Join(out.ActiveSkillNames, ","); got != "peer-only" {
		t.Errorf("ActiveSkillNames=%q, want peer-only", got)
	}
	// 原 snap 不受影响（不变性）
	if got := strings.Join(snap.ActiveSkillNames, ","); got != "global-skill" {
		t.Errorf("input snap should not mutate; got %q", got)
	}
}

// TestPeerScopedSnapshot_NilRunCtxNoop 主路径 noop。
func TestPeerScopedSnapshot_NilOverlayPreservesGlobal(t *testing.T) {
	snap := builtin_tools.StateSnapshot{
		CurrentStepID:    "main-s1",
		ActiveSkillNames: []string{"global"},
	}
	runCtx := &InlineStepCtx{StepID: "s2"} // 未初始化 overlay
	out := peerScopedSnapshot(snap, runCtx)

	if got := strings.Join(out.ActiveSkillNames, ","); got != "global" {
		t.Errorf("nil overlay should preserve global ActiveSkillNames; got %q", got)
	}
}

// TestApplySkillToolToOverlay_DeduplicatesAndNormalizes 微测试：overlay 上的 load
// 应该去重 + normalize。
func TestApplySkillToolToOverlay_DeduplicatesAndNormalizes(t *testing.T) {
	overlay := []string{"existing"}
	applySkillToolToOverlay(&overlay,
		builtin_tools.SkillToolName,
		nil,
		`{"ok":true,"skills":[{"name":"existing"},{"name":"new-one"},{"name":"new-one"}]}`,
	)
	if got := strings.Join(overlay, ","); got != "existing,new-one" {
		t.Errorf("dedup + normalize: got %q want existing,new-one", got)
	}
}

// TestSkill_PeerOverlay_CrossIterPersistence 红线（reviewer §9 补漏）：
// peer 在 iter 1 load 一个 skill 后，iter 2 / iter 3 prompt 派生仍能看到该 skill。
// overlay 是 runCtx 字段，runCtx 在整个 peer goroutine 生命周期内有效——跨 iter
// 持久性来自 runCtx 不被 GC 而非 cache。本测试断言「同 runCtx 多次 promptSnapshot
// 投影都拿到最新 overlay 内容」。
func TestSkill_PeerOverlay_CrossIterPersistence(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:            builtin_tools.AgentPhaseStep,
		Status:           builtin_tools.TaskStatusRunning,
		ActiveSkillNames: nil,
	})

	overlay := []string{}
	runCtx := &InlineStepCtx{StepID: "s2", LocalActiveSkillNames: &overlay}

	// iter 1: peer load skill_a
	agent.handleSkillToolStateSync(
		builtin_tools.SkillToolName,
		nil,
		`{"ok":true,"skills":[{"name":"skill_a"}]}`,
		"",
		runCtx,
	)

	// iter 1 prompt 派生
	snap := agent.state.Snapshot()
	projected1 := peerScopedSnapshot(snap, runCtx)
	if got := strings.Join(projected1.ActiveSkillNames, ","); got != "skill_a" {
		t.Fatalf("iter 1 projected ActiveSkillNames=%q want skill_a", got)
	}

	// iter 2: peer 又 load skill_b
	agent.handleSkillToolStateSync(
		builtin_tools.SkillToolName,
		nil,
		`{"ok":true,"skills":[{"name":"skill_b"}]}`,
		"",
		runCtx,
	)

	// iter 2 prompt 派生应当看到累积的 [skill_a, skill_b]
	projected2 := peerScopedSnapshot(snap, runCtx)
	if got := strings.Join(projected2.ActiveSkillNames, ","); got != "skill_a,skill_b" {
		t.Errorf("iter 2 projected ActiveSkillNames=%q want skill_a,skill_b (cross-iter persistence)", got)
	}

	// iter 3: peer eject skill_a
	agent.handleSkillToolStateSync(
		builtin_tools.EjectSkillToolName,
		map[string]any{"name": "skill_a"},
		`{"ok":true,"name":"skill_a"}`,
		"",
		runCtx,
	)
	projected3 := peerScopedSnapshot(snap, runCtx)
	if got := strings.Join(projected3.ActiveSkillNames, ","); got != "skill_b" {
		t.Errorf("iter 3 after eject projected=%q want skill_b", got)
	}

	// 全局始终未被动过
	if got := agent.state.Snapshot().ActiveSkillNames; len(got) != 0 {
		t.Errorf("global ActiveSkillNames MUST stay empty across all peer iters; got %v", got)
	}
}

// TestSkill_PeerOverlay_SpawnTimeBaselineFrozen 红线（reviewer §9 补漏）：
// peer 在 spawn 时刻拿到当时的全局 ActiveSkillNames 作为 baseline。spawn 之后
// 主路径继续 load/eject 不影响 peer overlay（baseline 是「冻结」语义，不跟随漂移）。
// 这是方案 B 的核心设计权衡——隔离优于实时同步。
func TestSkill_PeerOverlay_SpawnTimeBaselineFrozen(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	// 主路径 spawn 时刻已有 ["baseline-A", "baseline-B"]
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:            builtin_tools.AgentPhaseStep,
		Status:           builtin_tools.TaskStatusRunning,
		ActiveSkillNames: []string{"baseline-A", "baseline-B"},
	})

	// 模拟 spawnInlinePeer：取 snapshot 时 deep-copy 作为 baseline
	spawnSnap := agent.state.Snapshot()
	baseline := builtin_tools.CloneStringSlice(spawnSnap.ActiveSkillNames)
	runCtx := &InlineStepCtx{StepID: "s2", LocalActiveSkillNames: &baseline}

	// 主路径在 peer spawn 之后 load 新 skill
	agent.handleSkillToolStateSync(
		builtin_tools.SkillToolName,
		nil,
		`{"ok":true,"skills":[{"name":"main-added-after-spawn"}]}`,
		"",
		nil, // 主路径
	)
	// 主路径还 eject 了 baseline-A
	agent.handleSkillToolStateSync(
		builtin_tools.EjectSkillToolName,
		map[string]any{"name": "baseline-A"},
		`{"ok":true,"name":"baseline-A"}`,
		"",
		nil,
	)

	// 主路径全局现状：[baseline-B, main-added-after-spawn]
	mainSnap := agent.state.Snapshot()
	if got := strings.Join(mainSnap.ActiveSkillNames, ","); got != "baseline-B,main-added-after-spawn" {
		t.Errorf("main path ActiveSkillNames=%q want baseline-B,main-added-after-spawn", got)
	}

	// peer overlay 应当**保持** spawn 时刻的 baseline——不跟随主路径漂移
	if got := strings.Join(baseline, ","); got != "baseline-A,baseline-B" {
		t.Errorf("peer baseline=%q want baseline-A,baseline-B (FROZEN at spawn time, not following main)", got)
	}

	// peer prompt 派生看到的也是冻结 baseline
	projected := peerScopedSnapshot(mainSnap, runCtx)
	if got := strings.Join(projected.ActiveSkillNames, ","); got != "baseline-A,baseline-B" {
		t.Errorf("peer projected snapshot ActiveSkillNames=%q want baseline-A,baseline-B", got)
	}
}
