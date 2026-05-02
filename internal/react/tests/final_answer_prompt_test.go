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

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      false,
		"plan":           []any{map[string]any{"step": "noop"}},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	if strings.Contains(prompt, "<PLAN_VERSION>") || strings.Contains(prompt, "<PLAN>") {
		t.Fatalf("expected prompt to hide plan section when needs_planning=false, got:\n%s", prompt)
	}
}

func TestBuildFinalAnswerPrompt_ShowsPlanSectionWhenNeedsPlanningTrue(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      true,
		"plan":           []any{map[string]any{"step": "inspect"}},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "<PLAN_VERSION>") || !strings.Contains(prompt, "<PLAN>") {
		t.Fatalf("expected prompt to show plan section when needs_planning=true, got:\n%s", prompt)
	}
}

func TestBuildFinalAnswerPrompt_OmitsPublishedOutputSchemaWithoutPublishContract(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      false,
		"plan":           []any{},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	schema := extractFinalAnswerPromptSchema(t, prompt)
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", schema["properties"])
	}
	if _, exists := properties["published_output"]; exists {
		t.Fatalf("expected published_output to be omitted without publish contract, got %#v", properties["published_output"])
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("expected required array, got %#v", schema["required"])
	}
	for _, item := range required {
		if strings.TrimSpace(item.(string)) == "published_output" {
			t.Fatalf("did not expect published_output in required without publish contract")
		}
	}
}

func TestBuildFinalAnswerPrompt_InlinesPublishedOutputSchemaWhenRequired(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}
	agent.SetFinalAnswerPublishConfig(&FinalAnswerPublishConfig{
		Contract: &PublishedOutputContract{
			Name:   "test_contract",
			Schema: `{"type":"object","additionalProperties":false,"required":["ok"],"properties":{"ok":{"type":"boolean"}}}`,
		},
		Strict: true,
	})

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      false,
		"plan":           []any{},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "test_contract") {
		t.Fatalf("expected prompt to mention publish contract name, got:\n%s", prompt)
	}

	schema := extractFinalAnswerPromptSchema(t, prompt)
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", schema["properties"])
	}
	published, ok := properties["published_output"].(map[string]any)
	if !ok {
		t.Fatalf("expected published_output schema object, got %#v", properties["published_output"])
	}
	if published["type"] != "object" {
		t.Fatalf("expected published_output.type=object, got %#v", published["type"])
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("expected required array, got %#v", schema["required"])
	}
	found := false
	for _, item := range required {
		if strings.TrimSpace(item.(string)) == "published_output" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected published_output in required when publish contract enabled")
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
