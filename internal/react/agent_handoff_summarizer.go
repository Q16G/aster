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

func summarizeAgentHandoff(ctx context.Context, client ai.ChatClient, manager PromptManager, handoffTo string, agentInstruction string, prevSummary string, diff string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("agent handoff summarizer is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	handoffTo = strings.TrimSpace(handoffTo)
	agentInstruction = strings.TrimSpace(agentInstruction)
	prevSummary = strings.TrimSpace(prevSummary)
	diff = strings.TrimSpace(diff)

	if prevSummary == "" && diff == "" {
		return "", nil
	}
	if diff == "" {
		return prevSummary, nil
	}
	if manager == nil {
		return "", fmt.Errorf("agent handoff prompt manager is nil")
	}
	prompt, err := manager.BuildAgentHandoffPrompt(AgentHandoffPromptInput{
		HandoffTo:        handoffTo,
		AgentInstruction: agentInstruction,
		PrevSummary:      prevSummary,
		Diff:             diff,
	})
	if err != nil {
		return "", fmt.Errorf("build agent handoff prompt failed: %w", err)
	}
	return ai.ChatTextWithOptions(ctx, client, prompt, &ai.RequestOptions{PromptFamily: promptFamilyAgentHandoff})
}
