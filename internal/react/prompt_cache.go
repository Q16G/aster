package react

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"aster/internal/ai"
)

const (
	promptFamilyThinkAct          = "think_act"
	promptFamilyTaskPlanner       = "task_planner"
	promptFamilyStepReplan        = "step_replan"
	promptFamilyFinalAnswer       = "final_answer"
	promptFamilyIntentRecognition = "intent_recognition"
	promptFamilySimpleReply       = "simple_reply"
	promptFamilyHistoryCompaction = "history_compaction"
	promptFamilyAgentHandoff      = "agent_handoff"
)

func (a *Agent) buildPromptRequestOptions(promptFamily string, parts PromptParts, enableCache bool, tools ...*ai.FunctionTool) *ai.RequestOptions {
	options := &ai.RequestOptions{
		PromptFamily: strings.TrimSpace(promptFamily),
	}
	if a == nil || !enableCache {
		return ai.NormalizeRequestOptions(options)
	}
	if pcc := a.cfg.PromptCacheConfig; pcc != nil && !pcc.Enabled {
		return ai.NormalizeRequestOptions(options)
	}

	stablePrefixHash := hashText(parts.SystemJoined())
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
	if pcc := a.cfg.PromptCacheConfig; pcc != nil && pcc.TTL != "" {
		options.PromptCacheRetention = pcc.TTL
	}
	return ai.NormalizeRequestOptions(options)
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
