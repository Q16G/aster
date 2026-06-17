package openai

import (
	"testing"

	"aster/internal/ai"
)

func TestMergeLeadingSystemMsgs_MergesConsecutiveLeading(t *testing.T) {
	infos := []*ai.MsgInfo{
		ai.NewSystemMsgInfo("rules block"),
		ai.NewSystemMsgInfo("identity and env block"),
		ai.NewUserMsgInfo("go"),
	}

	merged := mergeLeadingSystemMsgs(infos)

	if len(merged) != 2 {
		t.Fatalf("expected 2 messages after merge, got %d", len(merged))
	}
	if merged[0].Role != "system" {
		t.Fatalf("expected first message system, got %s", merged[0].Role)
	}
	text, _ := merged[0].Content.(string)
	if text != "rules block\n\nidentity and env block" {
		t.Fatalf("unexpected merged content: %q", text)
	}
	if merged[1].Role != "user" {
		t.Fatalf("expected second message user, got %s", merged[1].Role)
	}
}

func TestMergeLeadingSystemMsgs_SingleSystemUntouched(t *testing.T) {
	infos := []*ai.MsgInfo{
		ai.NewSystemMsgInfo("only system"),
		ai.NewUserMsgInfo("go"),
	}

	merged := mergeLeadingSystemMsgs(infos)

	if len(merged) != 2 || merged[0].Content != "only system" {
		t.Fatalf("expected untouched messages, got %v", merged)
	}
}

func TestMergeLeadingSystemMsgs_NonLeadingSystemNotMerged(t *testing.T) {
	infos := []*ai.MsgInfo{
		ai.NewUserMsgInfo("go"),
		ai.NewSystemMsgInfo("late system"),
		ai.NewSystemMsgInfo("another late system"),
	}

	merged := mergeLeadingSystemMsgs(infos)

	if len(merged) != 3 {
		t.Fatalf("expected non-leading system messages untouched, got %d messages", len(merged))
	}
}
