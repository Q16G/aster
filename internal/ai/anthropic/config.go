package anthropic

import (
	"net/http"
	"time"

	aiusage "aster/internal/ai/usage"
)

type Config struct {
	URL string

	APIKey  string
	Model   string
	Version string

	Timeout     time.Duration
	Proxy       string
	MaxRetries  int
	MaxTokens   int
	Temperature *float64
	TopP        *float64

	Headers    map[string]string
	HTTPClient *http.Client

	ContextWindowTokens int
	OutputTokenLimit    int
	SupportsVision      *bool
	SupportsAudio       *bool

	UsagePricing aiusage.PricingModel
}

type Option func(*Config)

func WithURL(url string) Option {
	return func(c *Config) {
		c.URL = url
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

func WithVersion(version string) Option {
	return func(c *Config) {
		c.Version = version
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

func WithMaxTokens(tokens int) Option {
	return func(c *Config) {
		if tokens > 0 {
			c.MaxTokens = tokens
		}
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

func WithHeaders(headers map[string]string) Option {
	return func(c *Config) {
		if len(headers) == 0 {
			return
		}
		if c.Headers == nil {
			c.Headers = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			c.Headers[key] = value
		}
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Config) {
		c.HTTPClient = client
	}
}

func WithContextWindowTokens(tokens int) Option {
	return func(c *Config) {
		if tokens > 0 {
			c.ContextWindowTokens = tokens
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

func WithSupportsVision(v bool) Option {
	return func(c *Config) {
		c.SupportsVision = &v
	}
}

func WithSupportsAudio(v bool) Option {
	return func(c *Config) {
		c.SupportsAudio = &v
	}
}

func WithUsagePricing(model aiusage.PricingModel) Option {
	return func(c *Config) {
		c.UsagePricing = model
	}
}

func DefaultConfig() *Config {
	return &Config{
		URL:        "https://api.anthropic.com/v1/messages",
		Version:    "2023-06-01",
		Timeout:    120 * time.Second,
		MaxRetries: 3,
		MaxTokens:  16384,
		Headers:    map[string]string{},
	}
}
