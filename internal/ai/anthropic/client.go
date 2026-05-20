package anthropic

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aster/internal/ai"
	aiusage "aster/internal/ai/usage"

	"golang.org/x/net/proxy"
)

type Client struct {
	config     *Config
	httpClient *http.Client
	lastUsage  *ai.TokenUsage
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      *anthropicUsage         `json:"usage,omitempty"`
	Error      *anthropicError         `json:"error,omitempty"`
}

type anthropicError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

type httpError struct {
	StatusCode int
	Body       string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("anthropic api error: status=%d body=%s", e.StatusCode, e.Body)
}

var nonRetryableStatusCodes = map[int]bool{
	400: true, 401: true, 403: true,
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var he *httpError
	if errors.As(err, &he) {
		if nonRetryableStatusCodes[he.StatusCode] {
			return false
		}
		return true
	}
	// All non-HTTP errors (timeout, connection reset, EOF, etc.) are assumed transient.
	return true
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type anthropicTextBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  any                    `json:"input_schema,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

func NewClient(opts ...Option) *Client {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	if strings.TrimSpace(cfg.URL) == "" {
		cfg.URL = "https://api.anthropic.com/v1/messages"
	}
	return &Client{
		config:     cfg,
		httpClient: buildHTTPClient(cfg),
	}
}

func buildHTTPClient(cfg *Config) *http.Client {
	if cfg != nil && cfg.HTTPClient != nil {
		return cfg.HTTPClient
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{},
	}
	if cfg != nil && strings.TrimSpace(cfg.Proxy) != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err == nil {
			switch proxyURL.Scheme {
			case "http", "https":
				transport.Proxy = http.ProxyURL(proxyURL)
			case "socks5":
				dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
				if err == nil {
					transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
						return dialer.Dial(network, addr)
					}
				}
			}
		}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   0,
	}
}

func (c *Client) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	choices, err := c.ChatExWithOptions(ctx, []*ai.MsgInfo{info}, nil, tools...)
	if err != nil {
		return "", err
	}
	if len(choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return extractContent(choices[0]), nil
}

func (c *Client) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return c.ChatExWithOptions(ctx, infos, nil, tools...)
}

func (c *Client) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return c.ChatTextWithOptions(ctx, text, nil, tools...)
}

func (c *Client) ChatTextWithOptions(ctx context.Context, text string, options *ai.RequestOptions, tools ...*ai.FunctionTool) (string, error) {
	return c.ChatWithOptions(ctx, ai.NewUserMsgInfo(text), options, tools...)
}

func (c *Client) ChatWithOptions(ctx context.Context, info *ai.MsgInfo, options *ai.RequestOptions, tools ...*ai.FunctionTool) (string, error) {
	choices, err := c.ChatExWithOptions(ctx, []*ai.MsgInfo{info}, options, tools...)
	if err != nil {
		return "", err
	}
	if len(choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return extractContent(choices[0]), nil
}

func (c *Client) ChatStream(ctx context.Context, infos []*ai.MsgInfo, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	return c.ChatStreamWithOptions(ctx, infos, nil, handler, tools...)
}

func (c *Client) ChatStreamWithOptions(ctx context.Context, infos []*ai.MsgInfo, options *ai.RequestOptions, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	choices, err := c.ChatExWithOptions(ctx, infos, options, tools...)
	if err != nil {
		return err
	}
	if handler == nil {
		return nil
	}
	if len(choices) == 0 || choices[0] == nil || choices[0].Message == nil {
		return handler(nil, true)
	}
	choice := choices[0]
	content := extractContent(choice)
	err = handler(&ai.StreamDelta{
		Content:      content,
		ToolCalls:    ai.NormalizeFunctionToolSlice(choice.Message.ToolCalls),
		FinishReason: strings.TrimSpace(choice.FinishReason),
	}, false)
	if err != nil {
		return err
	}
	return handler(nil, true)
}

func (c *Client) ChatExWithOptions(ctx context.Context, infos []*ai.MsgInfo, options *ai.RequestOptions, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqBody := c.buildRequestBody(infos, tools, options)

	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		attemptTimeout := ai.AttemptTimeoutForAttempt(c.config.Timeout, attempt)
		attemptCtx := ctx
		cancelAttempt := func() {}
		if attemptTimeout > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, attemptTimeout)
		}
		choices, err := c.doRequest(attemptCtx, reqBody)
		cancelAttempt()
		if err == nil {
			return choices, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
		if attempt >= c.config.MaxRetries {
			break
		}
		waitDuration := time.Duration(math.Min(30, math.Pow(2, float64(attempt)))) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitDuration):
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// MaxRetries returns the configured retry count for diagnostics and orchestration.
func (c *Client) MaxRetries() int {
	if c == nil || c.config == nil {
		return 0
	}
	return c.config.MaxRetries
}

func (c *Client) buildRequestBody(infos []*ai.MsgInfo, tools []*ai.FunctionTool, options *ai.RequestOptions) map[string]any {
	options = ai.NormalizeRequestOptions(options)
	systemBlocks, messages := splitMessages(infos, options)
	body := map[string]any{
		"model":      strings.TrimSpace(c.config.Model),
		"max_tokens": c.config.MaxTokens,
		"messages":   messages,
	}
	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}
	if c.config.Temperature != nil {
		body["temperature"] = *c.config.Temperature
	}
	if c.config.TopP != nil {
		body["top_p"] = *c.config.TopP
	}
	if len(tools) > 0 {
		toolDefs := make([]anthropicTool, 0, len(tools))
		for _, tool := range tools {
			if tool == nil || tool.Function == nil {
				continue
			}
			def := anthropicTool{
				Name:        strings.TrimSpace(tool.Function.Name),
				Description: strings.TrimSpace(tool.Function.Description),
				InputSchema: tool.Function.Parameters,
			}
			if options != nil && options.PromptCacheEnabled {
				def.CacheControl = buildCacheControl(options)
			}
			toolDefs = append(toolDefs, def)
		}
		if len(toolDefs) > 0 {
			body["tools"] = toolDefs
		}
	}
	return body
}

func splitMessages(infos []*ai.MsgInfo, options *ai.RequestOptions) ([]anthropicTextBlock, []map[string]any) {
	var (
		systemBlocks []anthropicTextBlock
		messages     []map[string]any
	)
	for _, info := range infos {
		if info == nil {
			continue
		}
		role := strings.TrimSpace(info.Role)
		switch role {
		case "system":
			text := strings.TrimSpace(anyToText(info.Content))
			if text == "" {
				continue
			}
			if options != nil && options.PromptCacheEnabled {
				stablePrefix := extractStableSystemPrefix(options.PromptFamily, text)
				dynamicSuffix := strings.TrimSpace(strings.TrimPrefix(text, stablePrefix))
				if stablePrefix != "" {
					systemBlocks = append(systemBlocks, anthropicTextBlock{
						Type:         "text",
						Text:         stablePrefix,
						CacheControl: buildCacheControl(options),
					})
				}
				if dynamicSuffix != "" {
					systemBlocks = append(systemBlocks, anthropicTextBlock{
						Type: "text",
						Text: dynamicSuffix,
					})
				}
				continue
			}
			systemBlocks = append(systemBlocks, anthropicTextBlock{
				Type: "text",
				Text: text,
			})
		case "tool":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": strings.TrimSpace(info.ToolCallID),
						"content":     buildToolResultContent(info),
					},
				},
			})
		default:
			msg := map[string]any{
				"role": normalizeAnthropicRole(role),
			}
			content := buildAnthropicContent(info)
			if len(content) == 0 {
				content = []map[string]any{{
					"type": "text",
					"text": anyToText(info.Content),
				}}
			}
			msg["content"] = content
			messages = append(messages, msg)
		}
	}
	return systemBlocks, messages
}

func buildAnthropicContent(info *ai.MsgInfo) []map[string]any {
	if info == nil {
		return nil
	}
	content := make([]map[string]any, 0, 1+len(info.ToolCalls))
	if contexts := extractChatContexts(info.Content); len(contexts) > 0 {
		for _, ctx := range contexts {
			if ctx == nil {
				continue
			}
			switch ctx.Type {
			case "text":
				if t := strings.TrimSpace(ctx.Text); t != "" {
					content = append(content, map[string]any{"type": "text", "text": t})
				}
			case "image_url":
				if src := convertImageURLToAnthropicSource(ctx.ImageURL); src != nil {
					content = append(content, map[string]any{"type": "image", "source": src})
				}
			}
		}
	} else if text := strings.TrimSpace(anyToText(info.Content)); text != "" {
		content = append(content, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for _, tool := range info.ToolCalls {
		if tool == nil || tool.Function == nil {
			continue
		}
		input := map[string]any{}
		if parsed, ok := tool.Function.Arguments.(map[string]any); ok {
			input = parsed
		} else if raw := strings.TrimSpace(anyToText(tool.Function.Arguments)); raw != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    strings.TrimSpace(tool.Id),
			"name":  strings.TrimSpace(tool.Function.Name),
			"input": input,
		})
	}
	return content
}

func buildToolResultContent(info *ai.MsgInfo) any {
	if contexts := extractChatContexts(info.Content); len(contexts) > 0 {
		var blocks []map[string]any
		for _, ctx := range contexts {
			if ctx == nil {
				continue
			}
			switch ctx.Type {
			case "text":
				if t := strings.TrimSpace(ctx.Text); t != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": t})
				}
			case "image_url":
				if src := convertImageURLToAnthropicSource(ctx.ImageURL); src != nil {
					blocks = append(blocks, map[string]any{"type": "image", "source": src})
				}
			}
		}
		if len(blocks) > 0 {
			return blocks
		}
	}
	return anyToText(info.Content)
}

func normalizeAnthropicRole(role string) string {
	switch strings.TrimSpace(role) {
	case "assistant":
		return "assistant"
	default:
		return "user"
	}
}

func extractStableSystemPrefix(promptFamily string, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.TrimSpace(promptFamily) != "think_act" {
		return text
	}
	for _, marker := range []string{
		"<CURRENT_STEP>",
		"<DEPENDENCY_STEP_SUMMARIES>",
		"<EXECUTION_CONTEXTS>",
		"<WARNINGS>",
		"<UNRESOLVED>",
	} {
		if idx := strings.Index(text, marker); idx >= 0 {
			return strings.TrimSpace(text[:idx])
		}
	}
	return text
}

func buildCacheControl(options *ai.RequestOptions) *anthropicCacheControl {
	if options == nil || !options.PromptCacheEnabled {
		return nil
	}
	cacheControl := &anthropicCacheControl{Type: "ephemeral"}
	if options.PromptCacheRetention != "" {
		cacheControl.TTL = strings.TrimSpace(options.PromptCacheRetention)
	}
	return cacheControl
}

func (c *Client) doRequest(ctx context.Context, reqBody map[string]any) ([]*ai.ChatChoices, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSpace(c.config.URL), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("x-api-key", c.config.APIKey)
	}
	req.Header.Set("anthropic-version", strings.TrimSpace(c.config.Version))
	for key, value := range c.config.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, &httpError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return nil, fmt.Errorf("anthropic api error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	choice := parsed.toChoice()
	if choice != nil && choice.Usage != nil {
		c.lastUsage = choice.Usage
	} else {
		c.lastUsage = nil
	}
	if choice == nil {
		return nil, nil
	}
	return []*ai.ChatChoices{choice}, nil
}

func (r anthropicResponse) toChoice() *ai.ChatChoices {
	content := make([]string, 0, len(r.Content))
	toolCalls := make([]*ai.FunctionTool, 0)
	for _, block := range r.Content {
		switch strings.TrimSpace(block.Type) {
		case "text":
			if text := strings.TrimSpace(block.Text); text != "" {
				content = append(content, text)
			}
		case "tool_use":
			args := "{}"
			if len(block.Input) > 0 {
				if raw, err := json.Marshal(block.Input); err == nil {
					args = string(raw)
				}
			}
			toolCalls = append(toolCalls, &ai.FunctionTool{
				Id:   strings.TrimSpace(block.ID),
				Type: "function",
				Function: &ai.FunctionDetail{
					Name:      strings.TrimSpace(block.Name),
					Arguments: args,
				},
			})
		}
	}
	msg := ai.NewAIMsgInfo(strings.Join(content, "\n"))
	msg.ToolCalls = ai.NormalizeFunctionToolSlice(toolCalls)
	msg.Usage = r.Usage.toUsage()
	return &ai.ChatChoices{
		Index:        0,
		Message:      msg,
		Usage:        msg.Usage,
		FinishReason: strings.TrimSpace(r.StopReason),
	}
}

func (u *anthropicUsage) toUsage() *ai.TokenUsage {
	if u == nil {
		return nil
	}
	usage := &ai.TokenUsage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	return ai.NormalizeTokenUsagePtr(usage)
}

func (c *Client) LastTokenUsage() *ai.TokenUsage {
	if c == nil {
		return nil
	}
	return ai.NormalizeTokenUsagePtr(c.lastUsage)
}

func (c *Client) ModelContextInfo() ai.ModelContextInfo {
	if c == nil || c.config == nil {
		return ai.ModelContextInfo{}
	}
	return ai.ModelContextInfo{
		ModelName:           c.config.Model,
		ContextWindowTokens: c.config.ContextWindowTokens,
		OutputTokenLimit:    c.config.OutputTokenLimit,
		SupportsVision:      c.config.SupportsVision,
		SupportsAudio:       c.config.SupportsAudio,
	}.Normalize()
}

func (c *Client) UsagePricingModel() aiusage.PricingModel {
	if c == nil || c.config == nil {
		return aiusage.PricingModel{}
	}
	return c.config.UsagePricing
}

func extractContent(choice *ai.ChatChoices) string {
	if choice == nil || choice.Message == nil {
		return ""
	}
	if s, ok := choice.Message.Content.(string); ok {
		return s
	}
	return ""
}

func anyToText(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case []*ai.ChatContext:
		var parts []string
		for _, ctx := range typed {
			if ctx == nil {
				continue
			}
			if ctx.Type == "text" && strings.TrimSpace(ctx.Text) != "" {
				parts = append(parts, strings.TrimSpace(ctx.Text))
			} else if ctx.Type == "image_url" {
				parts = append(parts, "[image]")
			}
		}
		return strings.Join(parts, "\n")
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(raw)
	}
}

func parseDataURI(rawURL string) (mediaType string, data string, ok bool) {
	const marker = ";base64,"
	idx := strings.Index(rawURL, marker)
	if idx < 0 || !strings.HasPrefix(rawURL, "data:") {
		return "", "", false
	}
	mediaType = rawURL[len("data:"):idx]
	data = rawURL[idx+len(marker):]
	if mediaType == "" || data == "" {
		return "", "", false
	}
	return mediaType, data, true
}

func extractChatContexts(content any) []*ai.ChatContext {
	switch v := content.(type) {
	case []*ai.ChatContext:
		return v
	case []any:
		var result []*ai.ChatContext
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			raw, err := json.Marshal(m)
			if err != nil {
				continue
			}
			var ctx ai.ChatContext
			if err := json.Unmarshal(raw, &ctx); err != nil {
				continue
			}
			if ctx.Type != "" {
				result = append(result, &ctx)
			}
		}
		return result
	case map[string]any:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var ctx ai.ChatContext
		if err := json.Unmarshal(raw, &ctx); err != nil {
			return nil
		}
		if ctx.Type != "" {
			return []*ai.ChatContext{&ctx}
		}
		return nil
	default:
		return nil
	}
}

func convertImageURLToAnthropicSource(imageURL map[string]any) map[string]any {
	if imageURL == nil {
		return nil
	}
	rawURL, _ := imageURL["url"].(string)
	if rawURL == "" {
		return nil
	}
	if strings.HasPrefix(rawURL, "data:") {
		mediaType, data, ok := parseDataURI(rawURL)
		if !ok {
			return nil
		}
		return map[string]any{
			"type":       "base64",
			"media_type": mediaType,
			"data":       data,
		}
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return map[string]any{
			"type": "url",
			"url":  rawURL,
		}
	}
	return nil
}

