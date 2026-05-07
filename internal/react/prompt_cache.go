package react

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"aster/internal/ai"
)

const (
	promptFamilyThinkAct          = "think_act"
	promptFamilyIntentRecognition = "intent_recognition"
	promptFamilySimpleReply       = "simple_reply"
	promptFamilyHistoryCompaction = "history_compaction"
	promptFamilyAgentHandoff      = "agent_handoff"
)

func (a *Agent) buildPromptRequestOptions(promptFamily string, prompt string, enableCache bool, tools ...*ai.FunctionTool) *ai.RequestOptions {
	options := &ai.RequestOptions{
		PromptFamily: strings.TrimSpace(promptFamily),
	}
	if a == nil || !enableCache {
		return ai.NormalizeRequestOptions(options)
	}

	stablePrefix := extractStablePromptPrefix(promptFamily, prompt)
	stablePrefixHash := hashText(stablePrefix)
	toolHash := hashToolDefinitions(tools)
	scope := firstNonEmptyPromptCache(
		strings.TrimSpace(a.workspaceNamespace),
		strings.TrimSpace(a.workspaceSessionID),
		strings.TrimSpace(a.workspaceRootDir),
		"default",
	)
	key := strings.Join([]string{
		"prompt-cache",
		scope,
		strings.TrimSpace(a.agentName),
		strings.TrimSpace(promptFamily),
		stablePrefixHash,
		toolHash,
	}, ":")
	options.PromptCacheEnabled = true
	options.PromptCacheKey = key
	options.PromptCacheKeyHash = hashText(key)
	return ai.NormalizeRequestOptions(options)
}

func extractStablePromptPrefix(promptFamily string, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if strings.TrimSpace(promptFamily) != promptFamilyThinkAct {
		return prompt
	}
	for _, marker := range []string{
		"<PHASE>",
		"<CURRENT_GOAL>",
		"<CURRENT_STEP_ID>",
		"<CURRENT_STEP>",
		"<LATEST_INPUT>",
		"<INPUT_TIMELINE>",
		"<DEPENDENCY_STEP_SUMMARIES>",
		"<EXECUTION_CONTEXTS>",
		"<WARNINGS>",
		"<UNRESOLVED>",
	} {
		if idx := strings.Index(prompt, marker); idx >= 0 {
			return strings.TrimSpace(prompt[:idx])
		}
	}
	return prompt
}

func hashToolDefinitions(tools []*ai.FunctionTool) string {
	if len(tools) == 0 {
		return hashText("")
	}
	var b strings.Builder
	for _, tool := range tools {
		if tool == nil || tool.Function == nil {
			continue
		}
		b.WriteString(strings.TrimSpace(tool.Function.Name))
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(tool.Function.Description))
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(prettyJSON(tool.Function.Parameters)))
		b.WriteString("\n---\n")
	}
	return hashText(b.String())
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmptyPromptCache(vals ...string) string {
	for _, item := range vals {
		item = strings.TrimSpace(item)
		if item != "" {
			return item
		}
	}
	return ""
}
