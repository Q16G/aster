package react_test

import (
	. "aster/internal/react"
	"context"
	"testing"

	"aster/internal/ai"
)

type jsonReplyChatClient struct {
	reply string
}

func (c *jsonReplyChatClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return c.reply, nil
}

func (c *jsonReplyChatClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (c *jsonReplyChatClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return c.reply, nil
}

func TestDefaultTaskPlanner_Plan_ParsesWrappedJSON(t *testing.T) {
	planner := NewDefaultTaskPlanner(&jsonReplyChatClient{
		reply: "好的，计划如下：\n```json\n{\"needs_planning\":true,\"plan\":[{\"step\":\"先读代码\",\"status\":\"pending\"}],\"explanation\":\"需要分步骤处理\"}\n```",
	})

	result, err := planner.Plan(context.Background(), "请处理复杂任务")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if result == nil || !result.NeedsPlanning {
		t.Fatalf("expected planning result, got %#v", result)
	}
	if len(result.Plan) != 1 || result.Plan[0] == nil || result.Plan[0].Step != "先读代码" {
		t.Fatalf("unexpected plan items: %#v", result.Plan)
	}
}

func TestDefaultTaskPlanner_Plan_ParsesJSONFromNoisyText(t *testing.T) {
	planner := NewDefaultTaskPlanner(&jsonReplyChatClient{
		reply: "说明文字 {\"needs_planning\":true,\"plan\":[{\"step\":\"分析返回结果\",\"status\":\"pending\"}],\"explanation\":\"需要进一步拆解\"} trailing",
	})

	result, err := planner.Plan(context.Background(), "请分析复杂结果")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if result == nil || !result.NeedsPlanning {
		t.Fatalf("expected planning result, got %#v", result)
	}
	if len(result.Plan) != 1 || result.Plan[0] == nil || result.Plan[0].Step != "分析返回结果" {
		t.Fatalf("unexpected plan items: %#v", result.Plan)
	}
}
