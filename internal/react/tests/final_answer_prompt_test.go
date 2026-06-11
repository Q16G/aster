package react_test

import (
	. "aster/internal/react"
	"encoding/json"
	"strings"
	"testing"

	"aster/internal/ai"
)

func TestBuildFinalAnswerPrompt_HidesPlanSectionWhenNeedsPlanningFalse(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	parts, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      false,
		"plan":           []any{map[string]any{"step": "noop"}},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	prompt := parts.Joined()
	if strings.Contains(prompt, "<PLAN_VERSION>") || strings.Contains(prompt, "<STEP_OUTCOMES>") {
		t.Fatalf("expected removed sections to stay absent, got:\n%s", prompt)
	}
}

func TestBuildFinalAnswerPrompt_RendersPlanItemsCards(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	parts, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"plan_items":     []any{map[string]any{"id": "step-1", "step": "inspect", "status": "completed", "short_summary": "已检查"}},
		"warnings":       []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	if !strings.Contains(parts.User, "<PLAN_ITEMS>") || !strings.Contains(parts.User, "已检查") {
		t.Fatalf("expected user part to render plan item cards, got:\n%s", parts.User)
	}
}

func extractFinalAnswerPromptSchema(t *testing.T, prompt string) map[string]any {
	t.Helper()
	start := strings.Index(prompt, "<JSON-SCHEMA>")
	end := strings.Index(prompt, "</JSON-SCHEMA>")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("prompt does not contain JSON-SCHEMA block")
	}
	schemaText := strings.TrimSpace(prompt[start+len("<JSON-SCHEMA>") : end])
	var out map[string]any
	if err := json.Unmarshal([]byte(schemaText), &out); err != nil {
		t.Fatalf("unmarshal schema failed: %v\nschema=%s", err, schemaText)
	}
	return out
}
