package react

import (
	"strings"
	"testing"
)

func TestPromptManager_BuildersDoNotRenderNonce(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	cases := []struct {
		name  string
		build func() (string, error)
	}{
		{
			name: "think_act",
			build: func() (string, error) {
				return manager.BuildThinkActPrompt(ThinkActPromptInput{
					AgentInstruction: "你是测试代理",
				})
			},
		},
		{
			name: "history_compaction",
			build: func() (string, error) {
				return manager.BuildHistoryCompactionPrompt(HistoryCompactionPromptInput{
					Instruction: "总结对话",
					PrevSummary: "已有摘要",
				})
			},
		},
		{
			name: "agent_handoff",
			build: func() (string, error) {
				return manager.BuildAgentHandoffPrompt(AgentHandoffPromptInput{
					HandoffTo:        "sub_agent",
					AgentInstruction: "继续处理",
					PrevSummary:      "已有交接",
				})
			},
		},
	}

	for _, tc := range cases {
		rendered, err := tc.build()
		if err != nil {
			t.Fatalf("%s build failed: %v", tc.name, err)
		}
		if strings.Contains(strings.ToLower(rendered), "nonce") {
			t.Fatalf("%s prompt should not contain nonce markers, got:\n%s", tc.name, rendered)
		}
	}
}
