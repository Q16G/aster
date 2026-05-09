package react

import (
	"encoding/json"
	"strings"
	"sync"
	"unicode/utf8"

	"aster/internal/ai"
	"github.com/tiktoken-go/tokenizer"
)

const (
	defaultCharsPerToken       = 4
	defaultContextWindowTokens = 128000
	DefaultOutputReserveTokens = 32000
	SessionOutputTokenMax      = 32768
	openCodeCompactionBuffer   = 20000
)

type ContextBudget struct {
	ModelName           string
	ContextWindowTokens int
	InputTokenLimit     int
	OutputTokenLimit    int
	UsableInputTokens   int
	TriggerTokens       int
	OutputCapTokens     int
}

type knownModelTokenProfile struct {
	Name                string
	Family              string
	MatchPatterns       []string
	ContextWindowTokens int
	InputTokenLimit     int
	OutputTokenLimit    int
}

var knownModelTokenProfiles = []knownModelTokenProfile{
	{Name: "gpt-5.2", Family: "gpt", MatchPatterns: []string{"gpt-5.2"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000},
	{Name: "gpt-5.1", Family: "gpt", MatchPatterns: []string{"gpt-5.1"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000},
	{Name: "gpt-5-mini", Family: "gpt", MatchPatterns: []string{"gpt-5-mini"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000},
	{Name: "gpt-5-nano", Family: "gpt", MatchPatterns: []string{"gpt-5-nano"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000},
	{Name: "gpt-5", Family: "gpt", MatchPatterns: []string{"gpt-5"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000},
	{Name: "gpt-4.1-mini", Family: "gpt", MatchPatterns: []string{"gpt-4.1-mini"}, ContextWindowTokens: 1000000, OutputTokenLimit: 32768},
	{Name: "gpt-4.1-nano", Family: "gpt", MatchPatterns: []string{"gpt-4.1-nano"}, ContextWindowTokens: 1000000, OutputTokenLimit: 32768},
	{Name: "gpt-4.1", Family: "gpt", MatchPatterns: []string{"gpt-4.1"}, ContextWindowTokens: 1000000, OutputTokenLimit: 32768},
	{Name: "gpt-4o-mini", Family: "gpt", MatchPatterns: []string{"gpt-4o-mini"}, ContextWindowTokens: 128000, OutputTokenLimit: 16384},
	{Name: "gpt-4o", Family: "gpt", MatchPatterns: []string{"gpt-4o"}, ContextWindowTokens: 128000, OutputTokenLimit: 16384},
	{Name: "deepseek-reasoner", Family: "deepseek", MatchPatterns: []string{"deepseek-reasoner", "deepseek-r1"}, ContextWindowTokens: 128000, OutputTokenLimit: 65536},
	{Name: "deepseek-chat", Family: "deepseek", MatchPatterns: []string{"deepseek-chat"}, ContextWindowTokens: 128000, OutputTokenLimit: 8192},
	{Name: "deepseek-v3.2", Family: "deepseek", MatchPatterns: []string{"deepseek-v3.2", "deepseek-v3"}, ContextWindowTokens: 128000, OutputTokenLimit: 65536},
	{Name: "glm-4.7", Family: "chatglm", MatchPatterns: []string{"glm-4.7", "chatglm-4.7"}, ContextWindowTokens: 204800, OutputTokenLimit: 131072},
	{Name: "glm-4.6", Family: "chatglm", MatchPatterns: []string{"glm-4.6", "chatglm-4.6"}, ContextWindowTokens: 204800, OutputTokenLimit: 131072},
	{Name: "glm-4.5", Family: "chatglm", MatchPatterns: []string{"glm-4.5", "chatglm-4.5", "chatglm4"}, ContextWindowTokens: 131072, OutputTokenLimit: 98304},
	{Name: "glm-4.5v", Family: "chatglm", MatchPatterns: []string{"glm-4.5v", "chatglm-4v"}, ContextWindowTokens: 64000, OutputTokenLimit: 16384},
	{Name: "chatglm3", Family: "chatglm", MatchPatterns: []string{"chatglm3", "chatglm-3"}, ContextWindowTokens: 32768, OutputTokenLimit: 8192},
	{Name: "chatglm", Family: "chatglm", MatchPatterns: []string{"chatglm"}, ContextWindowTokens: 131072, OutputTokenLimit: 32768},
}

func resolveContextBudget(client ai.ChatClient) ContextBudget {
	budget := ContextBudget{
		ModelName: "unknown",
	}
	hasExplicitModelContext := false

	if provider, ok := client.(ai.ModelContextProvider); ok {
		info := provider.ModelContextInfo().Normalize()
		if strings.TrimSpace(info.ModelName) != "" {
			budget.ModelName = strings.TrimSpace(info.ModelName)
		}
		if info.ContextWindowTokens > 0 {
			budget.ContextWindowTokens = info.ContextWindowTokens
			hasExplicitModelContext = true
		}
		if info.InputTokenLimit > 0 {
			budget.InputTokenLimit = info.InputTokenLimit
			hasExplicitModelContext = true
		}
		if info.OutputTokenLimit > 0 {
			budget.OutputTokenLimit = info.OutputTokenLimit
			hasExplicitModelContext = true
		}
	}

	// 模型显式配置优先：仅在模型未提供任何上下文预算时，才使用内置模型档位兜底推断。
	if !hasExplicitModelContext {
		if inferred, ok := inferKnownModelContext(budget.ModelName); ok {
			if budget.ContextWindowTokens <= 0 {
				budget.ContextWindowTokens = inferred.ContextWindowTokens
			}
			if budget.InputTokenLimit <= 0 {
				budget.InputTokenLimit = inferred.InputTokenLimit
			}
			if budget.OutputTokenLimit <= 0 {
				budget.OutputTokenLimit = inferred.OutputTokenLimit
			}
		}
	}

	if budget.ContextWindowTokens <= 0 {
		budget.ContextWindowTokens = defaultContextWindowTokens
	}
	if budget.OutputTokenLimit <= 0 {
		budget.OutputTokenLimit = DefaultOutputReserveTokens
	}

	outputCap := budget.OutputTokenLimit
	if outputCap <= 0 {
		outputCap = DefaultOutputReserveTokens
	}
	if outputCap > SessionOutputTokenMax {
		outputCap = SessionOutputTokenMax
	}
	if outputCap <= 0 {
		outputCap = DefaultOutputReserveTokens
	}
	budget.OutputCapTokens = outputCap

	usable := budget.InputTokenLimit
	if usable <= 0 {
		usable = budget.ContextWindowTokens - outputCap
	} else {
		// When the provider exposes an input limit, reserve space for model output (up to 20k).
		reserved := outputCap
		if reserved > openCodeCompactionBuffer {
			reserved = openCodeCompactionBuffer
		}
		usable -= reserved
		if usable < 0 {
			usable = 0
		}
	}
	if usable <= 0 {
		usable = budget.ContextWindowTokens
	}
	if usable <= 0 {
		usable = defaultContextWindowTokens
	}

	budget.UsableInputTokens = usable
	budget.TriggerTokens = int(float64(usable) * 0.6)
	if budget.TriggerTokens < 1 {
		budget.TriggerTokens = usable
	}

	return budget
}

func inferKnownModelContext(modelName string) (ai.ModelContextInfo, bool) {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return ai.ModelContextInfo{}, false
	}
	for _, profile := range knownModelTokenProfiles {
		for _, pattern := range profile.MatchPatterns {
			if strings.Contains(name, strings.ToLower(strings.TrimSpace(pattern))) {
				return ai.ModelContextInfo{
					ModelName:           strings.TrimSpace(modelName),
					ContextWindowTokens: profile.ContextWindowTokens,
					InputTokenLimit:     profile.InputTokenLimit,
					OutputTokenLimit:    profile.OutputTokenLimit,
				}.Normalize(), true
			}
		}
	}
	return ai.ModelContextInfo{}, false
}

func estimateHistoryTokens(history []*ai.MsgInfo) int {
	total := 0
	for _, msg := range history {
		total += estimateMsgTokens(msg)
	}
	return total
}

func estimateMsgTokens(msg *ai.MsgInfo) int {
	if msg == nil {
		return 0
	}
	tokens := estimateStringTokens(msg.Role) +
		estimateStringTokens(msg.Type) +
		estimateStringTokens(msg.ToolCallID) +
		estimateStringTokens(msg.ReasoningOutput)

	if msg.Content != nil {
		tokens += estimateStringTokens(FormatMsgContent(msg.Content))
	}

	if len(msg.ToolCalls) > 0 {
		// tool calls 以 JSON bytes 为基准，按 bytes/4 估算（结构体字段名全 ASCII）
		if raw, err := json.Marshal(msg.ToolCalls); err == nil {
			tokens += (len(raw) + defaultCharsPerToken - 1) / defaultCharsPerToken
		}
	}

	return tokens
}

func estimateSummaryTokens(summary string) int {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return 0
	}
	return estimateStringTokens(HistoryCompactionRequestText) + estimateStringTokens(summary)
}

var (
	bpeEncoder     tokenizer.Codec
	bpeEncoderOnce sync.Once
)

func getBPEEncoder() tokenizer.Codec {
	bpeEncoderOnce.Do(func() {
		enc, err := tokenizer.Get(tokenizer.Cl100kBase)
		if err == nil {
			bpeEncoder = enc
		}
	})
	return bpeEncoder
}

func countTokens(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	enc := getBPEEncoder()
	if enc == nil {
		return fallbackEstimateTokens(s)
	}
	ids, _, _ := enc.Encode(s)
	if len(ids) == 0 {
		return fallbackEstimateTokens(s)
	}
	return len(ids)
}

func fallbackEstimateTokens(s string) int {
	runeCount := utf8.RuneCountInString(s)
	tokens := (runeCount + 1) / 2
	if tokens < 1 {
		return 1
	}
	return tokens
}

func estimateStringTokens(s string) int {
	return countTokens(s)
}
