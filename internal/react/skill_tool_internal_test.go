package react

import (
	"context"
	"testing"
)

type fakeSkillLookup struct {
	context string
	err     error
}

func (f fakeSkillLookup) LookupSkill(_ context.Context, name string) (*SkillInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &SkillInfo{Name: name, Context: f.context}, nil
}

type plainTool struct{}

func (plainTool) Name() string                                            { return "read_file" }
func (plainTool) Description() string                                     { return "" }
func (plainTool) Parameters() any                                         { return nil }
func (plainTool) Execute(context.Context, map[string]any) (string, error) { return "", nil }

func TestSkillToolIsAgentForArgs(t *testing.T) {
	ctx := context.Background()
	args := map[string]any{"skill": "sast-scan"}

	inline := &SkillTool{lookup: fakeSkillLookup{context: "inline"}}
	if IsAgentToolForCall(ctx, inline, args) {
		t.Fatalf("inline skill must not be classified as agent")
	}

	fork := &SkillTool{lookup: fakeSkillLookup{context: "fork"}}
	if !IsAgentToolForCall(ctx, fork, args) {
		t.Fatalf("fork skill must be classified as agent")
	}

	// 缺失 skill 名 / 查找失败时回退为非 agent,而非误判为子 agent。
	if IsAgentToolForCall(ctx, fork, map[string]any{}) {
		t.Fatalf("skill call without name must not be classified as agent")
	}
}

func TestIsAgentToolForCallFallsBackForPlainTool(t *testing.T) {
	if IsAgentToolForCall(context.Background(), plainTool{}, nil) {
		t.Fatalf("plain tool must not be classified as agent")
	}
}
