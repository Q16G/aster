package openai

import (
	"net/http"
	"time"

	"aster/internal/ai"
	aiusage "aster/internal/ai/usage"
)

type Config struct {
	URL string
	// URLAutoComplete 控制是否自动将 URL 规范化到聊天/向量接口路径。
	URLAutoComplete bool

	APIKey string
	Model  string

	ContextWindowTokens int
	InputTokenLimit     int
	OutputTokenLimit    int

	Timeout    time.Duration
	Proxy      string
	MaxRetries int
	// RetryCodes 保留兼容；当前默认重试策略不再按 HTTP 状态码筛选。
	RetryCodes  []int
	Stream      bool
	Temperature *float64
	TopP        *float64
	MaxTokens   int
	ExtraBody   map[string]any

	HTTPClient    *http.Client
	StreamFunc    StreamFunc
	RetryCallback RetryCallback

	// InsecureSkipVerify 是否跳过 TLS 证书校验（默认 false）
	InsecureSkipVerify bool

	// UsagePricing stores model pricing metadata for cost computation.
	UsagePricing aiusage.PricingModel
}

type StreamEvent struct {
	Content       string
	ReasonContent string
	ToolCalls     []*ai.FunctionTool
	FinishReason  string
	Done          bool
}

type StreamFunc func(event *StreamEvent)
type RetryCallback func(event RetryEvent)

type Option func(*Config)

func WithURL(url string) Option {
	return func(c *Config) {
		c.URL = url
	}
}

func WithURLAutoComplete(enabled bool) Option {
	return func(c *Config) {
		c.URLAutoComplete = enabled
	}
}

func WithAPIKey(key string) Option {
	return func(c *Config) {
		c.APIKey = key
	}
}

func WithModel(model string) Option {
	return func(c *Config) {
		c.Model = model
	}
}

func WithContextWindowTokens(tokens int) Option {
	return func(c *Config) {
		if tokens > 0 {
			c.ContextWindowTokens = tokens
		}
	}
}

func WithInputTokenLimit(tokens int) Option {
	return func(c *Config) {
		if tokens > 0 {
			c.InputTokenLimit = tokens
		}
	}
}

func WithOutputTokenLimit(tokens int) Option {
	return func(c *Config) {
		if tokens > 0 {
			c.OutputTokenLimit = tokens
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Timeout = timeout
	}
}

func WithProxy(proxy string) Option {
	return func(c *Config) {
		c.Proxy = proxy
	}
}

func WithMaxRetries(retries int) Option {
	return func(c *Config) {
		c.MaxRetries = retries
	}
}

// WithRetryCode 保留兼容，仍会写入 Config.RetryCodes。
// 当前默认重试策略不再按 HTTP 状态码筛选。
func WithRetryCode(codes ...int) Option {
	return func(c *Config) {
		if len(codes) == 0 {
			return
		}
		seen := make(map[int]struct{}, len(codes))
		normalized := make([]int, 0, len(codes))
		for _, code := range codes {
			if code < 100 || code > 599 {
				continue
			}
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			normalized = append(normalized, code)
		}
		if len(normalized) == 0 {
			return
		}
		c.RetryCodes = normalized
	}
}

func WithStream(stream bool) Option {
	return func(c *Config) {
		c.Stream = stream
	}
}

func WithTemperature(temp float64) Option {
	return func(c *Config) {
		c.Temperature = &temp
	}
}

func WithTopP(topP float64) Option {
	return func(c *Config) {
		c.TopP = &topP
	}
}

func WithMaxTokens(tokens int) Option {
	return func(c *Config) {
		c.MaxTokens = tokens
	}
}

func WithExtraBody(extra map[string]any) Option {
	return func(c *Config) {
		if len(extra) == 0 {
			return
		}
		if c.ExtraBody == nil {
			c.ExtraBody = make(map[string]any, len(extra))
		}
		for key, value := range extra {
			c.ExtraBody[key] = value
		}
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Config) {
		c.HTTPClient = client
	}
}

func WithStreamFunc(fn StreamFunc) Option {
	return func(c *Config) {
		c.StreamFunc = fn
	}
}

func WithRetryCallback(fn RetryCallback) Option {
	return func(c *Config) {
		c.RetryCallback = fn
	}
}

func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Config) {
		c.InsecureSkipVerify = skip
	}
}

func WithUsagePricing(model aiusage.PricingModel) Option {
	return func(c *Config) {
		if c == nil {
			return
		}
		c.UsagePricing = model
	}
}

var defaultRetryCodes = []int{429, 500, 502, 503, 504}

func DefaultConfig() *Config {
	return &Config{
		URL:             "https://api.openai.com/v1/chat/completions",
		URLAutoComplete: true,
		Timeout:         120 * time.Second,
		MaxRetries:      3,
		RetryCodes:      append([]int(nil), defaultRetryCodes...),
		Stream:          true,
		ExtraBody:       map[string]any{},
	}
}
