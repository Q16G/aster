package react

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"aster/internal/ai"
)

//go:embed prompts/history_compaction.prompt
var historyCompactionPrompt string

func summarizeHistoryCompaction(ctx context.Context, client ai.ChatClient, manager PromptManager, instruction string, prevSummary string, msgs []*ai.MsgInfo) (string, error) {
	if client == nil {
		return "", fmt.Errorf("history compaction summarizer is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	instruction = strings.TrimSpace(instruction)
	prevSummary = strings.TrimSpace(prevSummary)
	if len(msgs) == 0 {
		return prevSummary, nil
	}
	if manager == nil {
		return "", fmt.Errorf("history compaction prompt manager is nil")
	}
	prompt, err := manager.BuildHistoryCompactionPrompt(HistoryCompactionPromptInput{
		Instruction: instruction,
		PrevSummary: prevSummary,
		Nonce:       generateNonce(8),
	})
	if err != nil {
		return "", fmt.Errorf("build history compaction prompt failed: %w", err)
	}
	request := NormalizeHistoryMsgInfos(msgs)
	request = append(request, ai.NewUserMsgInfo(prompt))
	choices, err := client.ChatEx(ctx, request)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(ai.ChatChoice2String(choices)), nil
}
