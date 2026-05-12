package react

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

type stubChatClientForHIL struct{}

func (s *stubChatClientForHIL) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *stubChatClientForHIL) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *stubChatClientForHIL) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestHumanConfirm_PersistenceBarrier_FailsFastOnBlobWriteError(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-hil")
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}

	blobsDir := filepath.Join(store.SessionDir(), "blobs")
	if err := os.Chmod(blobsDir, 0o555); err != nil {
		t.Fatalf("chmod blobs dir: %v", err)
	}

	agent, err := NewReActAgent("test", &stubChatClientForHIL{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.v2Store = store
	agent.currentGroupID = "group-1"
	agent.currentTurnID = "turn-1"

	tc := &ai.FunctionTool{
		Id: "call-1",
		Function: &ai.FunctionDetail{
			Name:      builtin_tools.HumanConfirmToolName,
			Arguments: `{"question":"ok?"}`,
		},
	}
	err = agent.executeToolCall(context.Background(), 1, tc, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := isTurnInterruptRaised(err); ok {
		t.Fatalf("expected persistence failure (no interrupt raised), got interrupt sentinel: %v", err)
	}
}
