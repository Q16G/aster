package react

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"aster/internal/ai"
)

//go:embed prompts/agent_handoff.prompt
var agentHandoffPrompt string

// Deprecated: summarizeAgentHandoff 无调用方。实际 handoff 走 handoff_state.go → defaultOnHandoff。
func summarizeAgentHandoff(ctx context.Context, client ai.ChatClient, manager PromptManager, handoffTo string, agentInstruction string, prevSummary string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("agent handoff summarizer is nil")
	}
	if ctx == nil {
		return "", fmt.Errorf("ctx must not be nil")
	}
	handoffTo = strings.TrimSpace(handoffTo)
	agentInstruction = strings.TrimSpace(agentInstruction)
	prevSummary = strings.TrimSpace(prevSummary)

	if prevSummary == "" {
		return "", nil
	}
	if manager == nil {
		return "", fmt.Errorf("agent handoff prompt manager is nil")
	}
	prompt, err := manager.BuildAgentHandoffPrompt(AgentHandoffPromptInput{
		HandoffTo:        handoffTo,
		AgentInstruction: agentInstruction,
		PrevSummary:      prevSummary,
	})
	if err != nil {
		return "", fmt.Errorf("build agent handoff prompt failed: %w", err)
	}
	return ai.ChatTextWithOptions(ctx, client, prompt, &ai.RequestOptions{PromptFamily: promptFamilyAgentHandoff})
}
