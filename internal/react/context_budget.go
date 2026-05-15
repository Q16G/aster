package react

import (
	"encoding/json"
	"strings"
	"sync"
	"unicode/utf8"

	"aster/internal/ai"
	"aster/internal/provider"
	"github.com/tiktoken-go/tokenizer"
)

var (
	globalRegistryMu sync.RWMutex
	globalRegistry   *provider.Registry
)

func SetProviderRegistry(r *provider.Registry) {
	globalRegistryMu.Lock()
	defer globalRegistryMu.Unlock()
	globalRegistry = r
}

func getProviderRegistry() *provider.Registry {
	globalRegistryMu.RLock()
	defer globalRegistryMu.RUnlock()
	return globalRegistry
}

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
	SupportsVision      bool
	SupportsAudio       bool
}

type knownModelTokenProfile struct {
	Name                string
	Family              string
	MatchPatterns       []string
	ContextWindowTokens int
	InputTokenLimit     int
	OutputTokenLimit    int
	SupportsVision      *bool
	SupportsAudio       *bool
}

var knownModelTokenProfiles = []knownModelTokenProfile{
	{Name: "gpt-5.2", Family: "gpt", MatchPatterns: []string{"gpt-5.2"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000, SupportsVision: ai.BoolPtr(true), SupportsAudio: ai.BoolPtr(true)},
	{Name: "gpt-5.1", Family: "gpt", MatchPatterns: []string{"gpt-5.1"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000, SupportsVision: ai.BoolPtr(true), SupportsAudio: ai.BoolPtr(true)},
	{Name: "gpt-5-mini", Family: "gpt", MatchPatterns: []string{"gpt-5-mini"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000, SupportsVision: ai.BoolPtr(true), SupportsAudio: ai.BoolPtr(true)},
	{Name: "gpt-5-nano", Family: "gpt", MatchPatterns: []string{"gpt-5-nano"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000, SupportsVision: ai.BoolPtr(true)},
	{Name: "gpt-5", Family: "gpt", MatchPatterns: []string{"gpt-5"}, ContextWindowTokens: 400000, OutputTokenLimit: 128000, SupportsVision: ai.BoolPtr(true), SupportsAudio: ai.BoolPtr(true)},
	{Name: "gpt-4.1-mini", Family: "gpt", MatchPatterns: []string{"gpt-4.1-mini"}, ContextWindowTokens: 1000000, OutputTokenLimit: 32768, SupportsVision: ai.BoolPtr(true)},
	{Name: "gpt-4.1-nano", Family: "gpt", MatchPatterns: []string{"gpt-4.1-nano"}, ContextWindowTokens: 1000000, OutputTokenLimit: 32768, SupportsVision: ai.BoolPtr(true)},
	{Name: "gpt-4.1", Family: "gpt", MatchPatterns: []string{"gpt-4.1"}, ContextWindowTokens: 1000000, OutputTokenLimit: 32768, SupportsVision: ai.BoolPtr(true)},
	{Name: "gpt-4o-mini", Family: "gpt", MatchPatterns: []string{"gpt-4o-mini"}, ContextWindowTokens: 128000, OutputTokenLimit: 16384, SupportsVision: ai.BoolPtr(true), SupportsAudio: ai.BoolPtr(true)},
	{Name: "gpt-4o", Family: "gpt", MatchPatterns: []string{"gpt-4o"}, ContextWindowTokens: 128000, OutputTokenLimit: 16384, SupportsVision: ai.BoolPtr(true), SupportsAudio: ai.BoolPtr(true)},
	// DeepSeek V4 (preview): 1M context, 384K max output.
	// NOTE: `deepseek-chat` / `deepseek-reasoner` are legacy names and currently map to V4-Flash non-thinking / thinking mode.
	{Name: "deepseek-v4-pro", Family: "deepseek", MatchPatterns: []string{"deepseek-v4-pro"}, ContextWindowTokens: 1000000, OutputTokenLimit: 384000},
	{Name: "deepseek-v4-flash", Family: "deepseek", MatchPatterns: []string{"deepseek-v4-flash"}, ContextWindowTokens: 1000000, OutputTokenLimit: 384000},
	{Name: "deepseek-reasoner", Family: "deepseek", MatchPatterns: []string{"deepseek-reasoner", "deepseek-r1"}, ContextWindowTokens: 1000000, OutputTokenLimit: 384000},
	{Name: "deepseek-chat", Family: "deepseek", MatchPatterns: []string{"deepseek-chat"}, ContextWindowTokens: 1000000, OutputTokenLimit: 384000},
	{Name: "deepseek-v3.2", Family: "deepseek", MatchPatterns: []string{"deepseek-v3.2", "deepseek-v3"}, ContextWindowTokens: 128000, OutputTokenLimit: 65536},
	{Name: "glm-4.7", Family: "chatglm", MatchPatterns: []string{"glm-4.7", "chatglm-4.7"}, ContextWindowTokens: 204800, OutputTokenLimit: 131072},
	{Name: "glm-4.6", Family: "chatglm", MatchPatterns: []string{"glm-4.6", "chatglm-4.6"}, ContextWindowTokens: 204800, OutputTokenLimit: 131072},
	{Name: "glm-4.5v", Family: "chatglm", MatchPatterns: []string{"glm-4.5v", "chatglm-4v"}, ContextWindowTokens: 64000, OutputTokenLimit: 16384, SupportsVision: ai.BoolPtr(true)},
	{Name: "glm-4.5", Family: "chatglm", MatchPatterns: []string{"glm-4.5", "chatglm-4.5", "chatglm4"}, ContextWindowTokens: 131072, OutputTokenLimit: 98304},
	{Name: "chatglm3", Family: "chatglm", MatchPatterns: []string{"chatglm3", "chatglm-3"}, ContextWindowTokens: 32768, OutputTokenLimit: 8192},
	{Name: "chatglm", Family: "chatglm", MatchPatterns: []string{"chatglm"}, ContextWindowTokens: 131072, OutputTokenLimit: 32768},
}

func resolveContextBudget(client ai.ChatClient) ContextBudget {
	budget := ContextBudget{
		ModelName: "unknown",
	}
	hasExplicitTokenBudget := false
	hasExplicitVision := false
	hasExplicitAudio := false

	if provider, ok := client.(ai.ModelContextProvider); ok {
		info := provider.ModelContextInfo().Normalize()
		if strings.TrimSpace(info.ModelName) != "" {
			budget.ModelName = strings.TrimSpace(info.ModelName)
		}
		if info.ContextWindowTokens > 0 {
			budget.ContextWindowTokens = info.ContextWindowTokens
			hasExplicitTokenBudget = true
		}
		if info.InputTokenLimit > 0 {
			budget.InputTokenLimit = info.InputTokenLimit
			hasExplicitTokenBudget = true
		}
		if info.OutputTokenLimit > 0 {
			budget.OutputTokenLimit = info.OutputTokenLimit
			hasExplicitTokenBudget = true
		}
		if info.SupportsVision != nil {
			budget.SupportsVision = *info.SupportsVision
			hasExplicitVision = true
		}
		if info.SupportsAudio != nil {
			budget.SupportsAudio = *info.SupportsAudio
			hasExplicitAudio = true
		}
	}

	if inferred, ok := inferKnownModelContext(budget.ModelName); ok {
		if !hasExplicitTokenBudget {
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
		if !hasExplicitVision && inferred.SupportsVision != nil {
			budget.SupportsVision = *inferred.SupportsVision
		}
		if !hasExplicitAudio && inferred.SupportsAudio != nil {
			budget.SupportsAudio = *inferred.SupportsAudio
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

	if reg := getProviderRegistry(); reg != nil {
		if ctx, out, ok := reg.ResolveContextBudget(name); ok {
			vision, audio, _ := reg.ResolveModelCapabilities(name)
			info := ai.ModelContextInfo{
				ModelName:           strings.TrimSpace(modelName),
				ContextWindowTokens: ctx,
				OutputTokenLimit:    out,
				SupportsVision:      ai.BoolPtr(vision),
				SupportsAudio:       ai.BoolPtr(audio),
			}
			return info.Normalize(), true
		}
	}

	for _, profile := range knownModelTokenProfiles {
		for _, pattern := range profile.MatchPatterns {
			if strings.Contains(name, strings.ToLower(strings.TrimSpace(pattern))) {
				info := ai.ModelContextInfo{
					ModelName:           strings.TrimSpace(modelName),
					ContextWindowTokens: profile.ContextWindowTokens,
					InputTokenLimit:     profile.InputTokenLimit,
					OutputTokenLimit:    profile.OutputTokenLimit,
				}
				if profile.SupportsVision != nil {
					v := *profile.SupportsVision
					info.SupportsVision = &v
				}
				if profile.SupportsAudio != nil {
					v := *profile.SupportsAudio
					info.SupportsAudio = &v
				}
				return info.Normalize(), true
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

func ModelSupportsVision(client ai.ChatClient) bool {
	return resolveContextBudget(client).SupportsVision
}

func ModelSupportsAudio(client ai.ChatClient) bool {
	return resolveContextBudget(client).SupportsAudio
}
