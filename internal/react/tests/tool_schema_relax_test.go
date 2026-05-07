package react_test

import (
	. "aster/internal/react"
	"context"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type rigidSchemaTool struct{}

func (t *rigidSchemaTool) Name() string { return "rigid_schema_tool" }

func (t *rigidSchemaTool) Description() string { return "tool with strict schema for relaxation tests" }

func (t *rigidSchemaTool) Parameters() any {
	return map[string]any{
		"type":                 "object",
		"required":             []string{"input"},
		"additionalProperties": false,
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"enum":        []any{"a", "b"},
				"description": "required input",
			},
			"nested": map[string]any{
				"type":                 "object",
				"required":             []string{"child"},
				"additionalProperties": false,
				"properties": map[string]any{
					"child": map[string]any{
						"type":    "integer",
						"minimum": 1,
					},
				},
			},
			"list": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
					"enum": []any{"x"},
				},
			},
		},
	}
}

func (t *rigidSchemaTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return "ok", nil
}

type orderedTool struct {
	name string
}

func (t *orderedTool) Name() string { return t.name }

func (t *orderedTool) Description() string { return "ordered tool" }

func (t *orderedTool) Parameters() any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *orderedTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return "ok", nil
}

func containsRequired(list any, key string) bool {
	switch typed := list.(type) {
	case []string:
		for _, item := range typed {
			if item == key {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if builtin_tools.ToolRuntimeValue(item) == key {
				return true
			}
		}
	}
	return false
}

func TestBuildFunctionTools_RelaxesStrictParameterSchema(t *testing.T) {
	agent, err := NewReActAgent("schema-test", &stubChatClient{}, WithEmitter(NewDummyEmitter()), WithTool(&rigidSchemaTool{}))
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	tools, _ := agent.BuildFunctionTools(builtin_tools.AgentPhaseStep)
	var target *ai.FunctionTool
	for _, tool := range tools {
		if tool != nil && tool.Function != nil && tool.Function.Name == "rigid_schema_tool" {
			target = tool
			break
		}
	}
	if target == nil || target.Function == nil {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	params, ok := target.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("expected relaxed parameters object, got %#v", target.Function.Parameters)
	}
	if params["additionalProperties"] != true {
		t.Fatalf("expected top-level additionalProperties=true, got %#v", params["additionalProperties"])
	}
	if required, ok := params["required"]; !ok || !containsRequired(required, "input") {
		t.Fatalf("expected top-level required preserved, got %#v", params["required"])
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", params["properties"])
	}
	input, ok := props["input"].(map[string]any)
	if !ok {
		t.Fatalf("expected input property object, got %#v", props["input"])
	}
	if input["type"] != "string" {
		t.Fatalf("expected input type preserved, got %#v", input["type"])
	}
	if _, ok := input["enum"]; ok {
		t.Fatalf("expected input enum removed, got %#v", input["enum"])
	}

	nested, ok := props["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested property object, got %#v", props["nested"])
	}
	if nested["type"] != "object" {
		t.Fatalf("expected nested type preserved, got %#v", nested["type"])
	}
	if nested["additionalProperties"] != true {
		t.Fatalf("expected nested additionalProperties=true, got %#v", nested["additionalProperties"])
	}
	if _, ok := nested["required"]; ok {
		t.Fatalf("expected nested required removed, got %#v", nested["required"])
	}
	child := nested["properties"].(map[string]any)["child"].(map[string]any)
	if child["type"] != "integer" {
		t.Fatalf("expected child type preserved, got %#v", child["type"])
	}
	if _, ok := child["minimum"]; ok {
		t.Fatalf("expected child minimum removed, got %#v", child["minimum"])
	}

	list, ok := props["list"].(map[string]any)
	if !ok {
		t.Fatalf("expected list property object, got %#v", props["list"])
	}
	if list["type"] != "array" {
		t.Fatalf("expected list type preserved, got %#v", list["type"])
	}
	items, ok := list["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list items object, got %#v", list["items"])
	}
	if items["type"] != "string" {
		t.Fatalf("expected item type preserved, got %#v", items["type"])
	}
	if _, ok := items["enum"]; ok {
		t.Fatalf("expected item enum removed, got %#v", items["enum"])
	}
}

func TestBuildFunctionTools_RelaxesBuiltInUpdateCurrentStepSchema(t *testing.T) {
	agent, err := NewReActAgent("schema-test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	var updateTool *ai.FunctionTool
	tools, _ := agent.BuildFunctionTools(builtin_tools.AgentPhaseStep)
	for _, tool := range tools {
		if tool != nil && tool.Function != nil && tool.Function.Name == builtin_tools.UpdateCurrentStepToolName {
			updateTool = tool
			break
		}
	}
	if updateTool == nil || updateTool.Function == nil {
		t.Fatalf("expected update_current_step tool in function tools")
	}

	params, ok := updateTool.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("expected parameters object, got %#v", updateTool.Function.Parameters)
	}
	if _, ok := params["required"]; !ok {
		t.Fatalf("expected required preserved, got %#v", params["required"])
	}
	if params["additionalProperties"] != true {
		t.Fatalf("expected additionalProperties=true, got %#v", params["additionalProperties"])
	}
}

func TestBuildFunctionTools_FollowsRegistrationOrder(t *testing.T) {
	agent, err := NewReActAgent(
		"ordered-tools-test",
		&stubChatClient{},
		WithEmitter(NewDummyEmitter()),
		WithTool(&orderedTool{name: "z-last"}),
		WithTool(&orderedTool{name: "a-middle"}),
		WithTool(&orderedTool{name: "m-final"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	tools, _ := agent.BuildFunctionTools(builtin_tools.AgentPhaseStep)
	var names []string
	for _, tool := range tools {
		if tool == nil || tool.Function == nil {
			continue
		}
		names = append(names, tool.Function.Name)
	}

	expected := []string{
		builtin_tools.UpdateCurrentStepToolName,
		builtin_tools.HumanConfirmToolName,
		"z-last",
		"a-middle",
		"m-final",
	}
	if len(names) != len(expected) {
		t.Fatalf("unexpected tool count: got %v want %v", names, expected)
	}
	for i, name := range expected {
		if names[i] != name {
			t.Fatalf("unexpected tool order at %d: got %v want %v", i, names, expected)
		}
	}
}
