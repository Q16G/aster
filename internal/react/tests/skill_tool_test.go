package react_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

type mockSkillLookup struct {
	skills map[string]*SkillInfo
}

// forkScriptClient 让 skill-fork 子 loop 首轮即调 submit_result 收尾（避免 stub 空返回触发
// AICallProxy 15s 空响应退避），并记录收到的出站消息供断言「skill body 进了 task/user 消息」。
type forkScriptClient struct {
	recorded [][]*ai.MsgInfo
}

func (c *forkScriptClient) ModelContextInfo() ai.ModelContextInfo {
	return ai.ModelContextInfo{ModelName: "test-model", InputTokenLimit: 128000, OutputTokenLimit: 8000}.Normalize()
}
func (c *forkScriptClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (c *forkScriptClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (c *forkScriptClient) ChatEx(_ context.Context, msgs []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.recorded = append(c.recorded, msgs)
	args, _ := json.Marshal(map[string]any{"status": "completed", "result": "skill done"})
	tc := &ai.FunctionTool{Id: "sr", Type: "function", Function: &ai.FunctionDetail{Name: builtin_tools.SubmitResultToolName, Arguments: string(args)}}
	msg := ai.NewAIMsgInfo("")
	msg.ToolCalls = []*ai.FunctionTool{tc}
	return []*ai.ChatChoices{{Message: msg, FinishReason: "tool_calls"}}, nil
}
func (c *forkScriptClient) sawInOutbound(substr string) bool {
	for _, msgs := range c.recorded {
		for _, m := range msgs {
			if m != nil && strings.Contains(fmt.Sprintf("%v", m.Content), substr) {
				return true
			}
		}
	}
	return false
}

func (m *mockSkillLookup) LookupSkill(_ context.Context, name string) (*SkillInfo, error) {
	if info, ok := m.skills[name]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

func TestSkillTool_Name(t *testing.T) {
	tool := NewSkillTool(nil, nil, nil)
	if tool.Name() != builtin_tools.SkillToolName {
		t.Fatalf("expected name %q, got %q", builtin_tools.SkillToolName, tool.Name())
	}
}

func TestSkillTool_IsAgent(t *testing.T) {
	tool := NewSkillTool(nil, nil, nil)
	if !tool.IsAgent() {
		t.Fatal("expected IsAgent() = true for SkillTool (fork mode runs a child agent)")
	}
}

func TestSkillTool_Parameters_SkillRequired(t *testing.T) {
	tool := NewSkillTool(nil, nil, nil)
	params := tool.Parameters()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["skill"]; !ok {
		t.Fatal("expected 'skill' property")
	}
	if _, ok := props["args"]; !ok {
		t.Fatal("expected 'args' property")
	}

	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("expected required array")
	}
	found := false
	for _, r := range required {
		if r == "skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("'skill' should be in required")
	}
}

func TestSkillTool_Execute_NilLookup(t *testing.T) {
	tool := NewSkillTool(nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "test"})
	if err == nil {
		t.Fatal("expected error for nil lookup")
	}
}

func TestSkillTool_Execute_SkillNotFound(t *testing.T) {
	lookup := &mockSkillLookup{skills: map[string]*SkillInfo{}}

	agent, err := NewReActAgent("test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSkillTool(agent, factory, lookup)

	_, err = tool.Execute(context.Background(), map[string]any{"skill": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestSkillTool_Execute_InlineMode(t *testing.T) {
	lookup := &mockSkillLookup{
		skills: map[string]*SkillInfo{
			"test-skill": {
				Name:         "test-skill",
				Description:  "A test skill",
				Instructions: "Execute $ARGUMENTS in $0 mode",
				Context:      "inline",
				SkillDir:     "/skills/test",
			},
		},
	}

	agent, err := NewReActAgent("test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSkillTool(agent, factory, lookup)

	result, err := tool.Execute(context.Background(), map[string]any{
		"skill": "test-skill",
		"args":  "staging fast",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var payload struct {
		OK     bool `json:"ok"`
		Count  int  `json:"count"`
		Skills []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			Context      string `json:"context"`
			Instructions string `json:"instructions"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}
	if payload.Count != 1 {
		t.Fatalf("expected count=1, got %d", payload.Count)
	}
	if len(payload.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(payload.Skills))
	}

	skill := payload.Skills[0]
	if skill.Name != "test-skill" {
		t.Fatalf("expected skill name 'test-skill', got %q", skill.Name)
	}
	if skill.Instructions != "Execute staging fast in staging mode" {
		t.Fatalf("expected substituted instructions, got %q", skill.Instructions)
	}
}

func TestSkillTool_Execute_InlineNoArgs(t *testing.T) {
	lookup := &mockSkillLookup{
		skills: map[string]*SkillInfo{
			"simple": {
				Name:         "simple",
				Description:  "Simple skill",
				Instructions: "Just do it",
				Context:      "inline",
			},
		},
	}

	agent, err := NewReActAgent("test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSkillTool(agent, factory, lookup)

	result, err := tool.Execute(context.Background(), map[string]any{"skill": "simple"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var payload struct {
		OK     bool `json:"ok"`
		Skills []struct {
			Instructions string `json:"instructions"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Skills[0].Instructions != "Just do it" {
		t.Fatalf("unexpected instructions: %q", payload.Skills[0].Instructions)
	}
}

func TestSkillTool_Execute_MissingSkillName(t *testing.T) {
	lookup := &mockSkillLookup{skills: map[string]*SkillInfo{}}
	agent, err := NewReActAgent("test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSkillTool(agent, factory, lookup)

	_, err = tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing skill name")
	}
}

func TestSkillTool_RegisteredViaFactory(t *testing.T) {
	lookup := &mockSkillLookup{skills: map[string]*SkillInfo{}}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
		WithFactorySkillLookup(lookup),
	)
	agent, err := factory.Build(AgentDefinition{
		Name:        "parent",
		Instruction: "test",
		ToolNames:   []string{"read_file"},
	})
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	tool, ok := agent.GetTool(builtin_tools.SkillToolName)
	if !ok || tool == nil {
		t.Fatal("skill tool should be registered by factory when skillLookup is set")
	}
}

func TestSkillTool_NotRegisteredWithoutLookup(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)
	agent, err := factory.Build(AgentDefinition{
		Name:        "parent",
		Instruction: "test",
		ToolNames:   []string{"read_file"},
	})
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	_, ok := agent.GetTool(builtin_tools.SkillToolName)
	if ok {
		t.Fatal("skill tool should NOT be registered when skillLookup is nil")
	}
}

func TestSkillTool_ForkMode_AllowedInsideSubAgent(t *testing.T) {
	lookup := &mockSkillLookup{
		skills: map[string]*SkillInfo{
			"fork-skill": {
				Name:         "fork-skill",
				Instructions: "Do stuff UNIQUE_BODY_MARKER",
				Context:      "fork",
			},
		},
	}

	agent, err := NewReActAgent("test", &forkScriptClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	client := &forkScriptClient{}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(client),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSkillTool(agent, factory, lookup)

	ctx := builtin_tools.WithToolRuntime(context.Background(), builtin_tools.ToolRuntimeInfo{
		Emitter:    NewDummyEmitter(),
		StackDepth: 1,
	})
	out, err := tool.Execute(ctx, map[string]any{"skill": "fork-skill"})
	if err != nil {
		t.Fatalf("skill fork should be allowed inside sub-agent, got: %v", err)
	}

	// S2：fork 走单循环内核 → 统一信封（有 status/workspace，无旧字段 ok/agent_name/plan_summary）。
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("fork result must be JSON envelope: %v (%s)", err, out)
	}
	if env["status"] == nil || env["workspace"] == nil {
		t.Fatalf("expected unified envelope with status/workspace, got: %s", out)
	}
	for _, banned := range []string{"ok", "agent_name", "plan_summary"} {
		if _, ok := env[banned]; ok {
			t.Fatalf("banned legacy field %q in fork envelope: %s", banned, out)
		}
	}
	// 关键契约：skill body 必须经 task 通道进子 loop user 消息（sub_agent 相位不渲染 Instruction，
	// body 塞 Instruction 会静默丢失）。
	if !client.sawInOutbound("UNIQUE_BODY_MARKER") {
		t.Fatalf("skill body 未进子 loop 出站消息——task 通道契约被破坏")
	}
}
