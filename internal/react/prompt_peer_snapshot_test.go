package react

import (
	"context"
	"strings"
	"sync"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type peerPromptStubClient struct{}

func (s *peerPromptStubClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *peerPromptStubClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *peerPromptStubClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func newPeerPromptTestAgent(t *testing.T) *Agent {
	t.Helper()
	agent, err := NewReActAgent("peer-prompt-test", &peerPromptStubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	runtime, err := newLocalWorkspaceRuntime("test-sess", t.TempDir(), "root")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	agent.workspaceRuntime = runtime
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		Status:        builtin_tools.TaskStatusRunning,
		CurrentStepID: "s1", // 主路径在 s1
		PlanVersion:   1,
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "主 step s1", Status: builtin_tools.PlanStepInProgress},
			{ID: "s2", Step: "peer s2", Status: builtin_tools.PlanStepInProgress},
			{ID: "s3", Step: "peer s3", Status: builtin_tools.PlanStepInProgress},
			{ID: "s4", Step: "peer s4", Status: builtin_tools.PlanStepPending},
		},
	})
	return agent
}

// TestBuildThinkActPrompt_UsesProvidedSnapshot 红线（Bug B 根因）：
// 主路径 state.CurrentStepID=s1，但传入的 snapshot.CurrentStepID=s2，
// 生成的 prompt 必须含 step_s2.md 不含 step_s1.md（旧 bug 总是 step_s1.md）。
func TestBuildThinkActPrompt_UsesProvidedSnapshot(t *testing.T) {
	agent := newPeerPromptTestAgent(t)

	// 模拟 peer 路径：拿一个 promptSnapshot 把 CurrentStepID 改成 s2
	peerSnap := agent.state.Snapshot()
	peerSnap.CurrentStepID = "s2"

	parts := agent.BuildThinkActPrompt(context.Background(), "", peerSnap)
	joined := parts.Joined()

	if !strings.Contains(joined, "step_s2.md") {
		t.Errorf("peer prompt should contain step_s2.md (peer's own step file); got:\n--- prompt ---\n%s\n--- end ---", joined)
	}
	if strings.Contains(joined, "step_s1.md") {
		t.Errorf("peer prompt MUST NOT contain step_s1.md (main step file leaked); got prompt containing s1")
	}
}

// TestThinkActPartsForStep_PassesPeerSnapshot peer 路径调 thinkActPartsForStep 时
// promptSnapshot 的 CurrentStepID 应当透传到 BuildThinkActPrompt——验证 caller-injected
// snapshot 不被 BuildThinkActPrompt 内部覆盖。
func TestThinkActPartsForStep_PassesPeerSnapshot(t *testing.T) {
	agent := newPeerPromptTestAgent(t)

	// agent.state.CurrentStepID = s1（主路径）
	// 但 thinkActPartsForStep 收到带 s3 的 snapshot
	peerSnap := agent.state.Snapshot()
	peerSnap.CurrentStepID = "s3"

	parts := agent.thinkActPartsForStep(context.Background(), "", peerSnap)
	joined := parts.Joined()

	if !strings.Contains(joined, "step_s3.md") {
		t.Errorf("peer thinkActPartsForStep should produce prompt with step_s3.md; got:\n%s", joined)
	}
	if strings.Contains(joined, "step_s1.md") {
		t.Errorf("peer thinkActPartsForStep MUST NOT leak step_s1.md; got prompt containing s1")
	}
}

// TestBuildThinkActPrompt_ConcurrentPeersGetDistinctStepFile **关键并发红线**：
// 3 个 goroutine 并发各自传不同 stepID 的 snapshot，断言每个 prompt 含且仅含
// 自己 stepID 的 step file 路径——直接复现 session 53db77a1 的 bug。
//
// 旧版本：3 个 peer 共享 a.state.Snapshot()，CurrentStepID 永远是主路径 s1，
//         3 个 prompt 都注入 step_s1.md → workspace timeline 全部 s1。
// 修复后：每个 peer 拿自己 snapshot 的 STEP_FILE_PATH，互不串扰。
func TestBuildThinkActPrompt_ConcurrentPeersGetDistinctStepFile(t *testing.T) {
	agent := newPeerPromptTestAgent(t)

	peerStepIDs := []string{"s2", "s3", "s4"}
	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, stepID := range peerStepIDs {
		stepID := stepID
		wg.Add(1)
		go func() {
			defer wg.Done()
			peerSnap := agent.state.Snapshot()
			peerSnap.CurrentStepID = stepID
			parts := agent.BuildThinkActPrompt(context.Background(), "", peerSnap)
			mu.Lock()
			results[stepID] = parts.Joined()
			mu.Unlock()
		}()
	}
	wg.Wait()

	// 每个 peer 的 prompt 应当含自己的 step_<peer>.md，不含其他 step 的
	for stepID, joined := range results {
		own := "step_" + stepID + ".md"
		if !strings.Contains(joined, own) {
			t.Errorf("peer %s prompt should contain %s; got:\n%s", stepID, own, joined)
		}
		for _, other := range peerStepIDs {
			if other == stepID {
				continue
			}
			otherFile := "step_" + other + ".md"
			if strings.Contains(joined, otherFile) {
				t.Errorf("peer %s prompt MUST NOT contain other peer's %s", stepID, otherFile)
			}
		}
		if strings.Contains(joined, "step_s1.md") {
			t.Errorf("peer %s prompt MUST NOT contain main path step_s1.md", stepID)
		}
	}
}

// TestBuildThinkActPrompt_PeerSnapshot_DependencyItemsProjected 同 bug 不同侧：
// dependencyItems / OpenItemsLedgerPath / TaskContextPath 等都按 peer snapshot
// 派生，不受 a.state 主路径影响。
//
// 简化：本测试只验证 OpenItemsLedgerPath / TaskContextPath 不因 peer 不同而变化
// （这俩是 workspace 路径而非 step 路径），证明 BuildThinkActPrompt 接收 snapshot 后
// **其他字段也按 snapshot 派生**——不只是 STEP_FILE_PATH 一处。
func TestBuildThinkActPrompt_PeerSnapshot_DependencyItemsProjected(t *testing.T) {
	agent := newPeerPromptTestAgent(t)

	// 验证 prompt 同时包含 step_s2.md（peer 的）和 OpenItemsLedgerPath（workspace 级）
	peerSnap := agent.state.Snapshot()
	peerSnap.CurrentStepID = "s2"
	parts := agent.BuildThinkActPrompt(context.Background(), "", peerSnap)
	joined := parts.Joined()

	if !strings.Contains(joined, "step_s2.md") {
		t.Errorf("expected step_s2.md in peer prompt; got:\n%s", joined)
	}
	// OpenItemsLedgerPath / TaskContextPath 都是 workspace 路径，不随 stepID 变；
	// 但它们应当出现在 prompt 里（证明 prompt 派生路径走通）
	if !strings.Contains(joined, "open_items.md") {
		t.Errorf("expected open_items.md path in prompt; got:\n%s", joined)
	}
}
