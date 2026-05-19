package openai

import (
	"aster/internal/ai"
	aiusage "aster/internal/ai/usage"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

type Client struct {
	config     *Config
	httpClient *http.Client
	lastUsage  *ai.TokenUsage
}

type RetryEvent struct {
	Attempt     int
	MaxAttempts int
	Delay       time.Duration
	Next        time.Time
	Message     string
}

func NewClient(opts ...Option) *Client {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.URL == "" {
		cfg.URL = "https://api.openai.com/v1/chat/completions"
	}

	client := &Client{config: cfg}
	client.httpClient = client.buildHTTPClient()

	return client
}

func (c *Client) buildHTTPClient() *http.Client {
	if c.config.HTTPClient != nil {
		return c.config.HTTPClient
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.config.InsecureSkipVerify},
	}

	if c.config.Proxy != "" {
		proxyURL, err := url.Parse(c.config.Proxy)
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
		// Timeout must be per-attempt (context deadline) instead of a fixed client-wide value.
		// A non-zero http.Client.Timeout would cap all retries to the same timeout.
		Timeout: 0,
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

func (c *Client) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return c.ChatTextWithOptions(ctx, text, nil, tools...)
}

func (c *Client) ChatStream(ctx context.Context, infos []*ai.MsgInfo, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	return c.ChatStreamWithOptions(ctx, infos, nil, handler, tools...)
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

func (c *Client) ChatStreamWithOptions(ctx context.Context, infos []*ai.MsgInfo, options *ai.RequestOptions, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	if c == nil {
		return fmt.Errorf("client is nil")
	}

	cfg := DefaultConfig()
	if c.config != nil {
		*cfg = *c.config
	}
	originalStreamFunc := cfg.StreamFunc
	var handlerErr error
	cfg.Stream = true
	cfg.StreamFunc = func(event *StreamEvent) {
		if originalStreamFunc != nil {
			originalStreamFunc(event)
		}
		if handler == nil || handlerErr != nil || event == nil {
			return
		}
		if event.Done {
			handlerErr = handler(nil, true)
			return
		}
		handlerErr = handler(&ai.StreamDelta{
			ReasoningContent: event.ReasonContent,
			Content:          event.Content,
			ToolCalls:        ai.NormalizeFunctionToolSlice(event.ToolCalls),
			FinishReason:     strings.TrimSpace(event.FinishReason),
		}, false)
	}
	streamClient := &Client{
		config:     cfg,
		httpClient: c.httpClient,
	}

	_, err := streamClient.ChatExWithOptions(ctx, infos, options, tools...)
	if lastUsage := streamClient.LastTokenUsage(); lastUsage != nil {
		c.lastUsage = lastUsage
	} else {
		c.lastUsage = nil
	}
	if err != nil {
		return err
	}
	return handlerErr
}

func (c *Client) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return c.ChatExWithOptions(ctx, infos, nil, tools...)
}

func (c *Client) ChatExWithOptions(ctx context.Context, infos []*ai.MsgInfo, options *ai.RequestOptions, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqBody := c.buildRequestBody(infos, tools, options)

	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		attemptTimeout := AttemptTimeoutForAttempt(c.config.Timeout, attempt)
		attemptCtx := ctx
		cancelAttempt := func() {}
		if attemptTimeout > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, attemptTimeout)
		}

		choices, _, err := c.doRequest(attemptCtx, reqBody)
		cancelAttempt()
		if err == nil {
			return choices, nil
		}

		lastErr = err

		retryDecision := BuildRetryDecision(err, NormalizeRetryCodes(c.config.RetryCodes))
		if !retryDecision.Retry {
			return nil, err
		}

		if attempt < c.config.MaxRetries {
			waitDuration := c.backoffDuration(attempt + 1)
			c.reportRetryAttempt(RetryEvent{
				Attempt:     attempt + 1,
				MaxAttempts: c.config.MaxRetries + 1,
				Delay:       waitDuration,
				Next:        time.Now().Add(waitDuration),
				Message:     retryDecision.Message,
			}, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitDuration):
			}
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (c *Client) reportRetryAttempt(event RetryEvent, err error) {
	if c == nil || c.config == nil {
		return
	}
	if c.config.RetryCallback != nil {
		c.config.RetryCallback(event)
		return
	}
	log.Printf(
		"[openai.retry] attempt=%d/%d delay=%s error=%v",
		event.Attempt,
		event.MaxAttempts,
		event.Delay,
		err,
	)
}

func (c *Client) buildRequestBody(infos []*ai.MsgInfo, tools []*ai.FunctionTool, options *ai.RequestOptions) map[string]any {
	messages := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		msg := map[string]any{
			"role":    info.Role,
			"content": info.Content,
		}
		if msgType := strings.TrimSpace(info.Type); msgType != "" {
			msg["type"] = msgType
		}
		if info.ToolCallID != "" {
			msg["tool_call_id"] = info.ToolCallID
		}
		if reasoning := strings.TrimSpace(info.ReasoningOutput); reasoning != "" {
			msg["reasoning_content"] = reasoning
		}
		if len(info.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, 0, len(info.ToolCalls))
			for _, tc := range info.ToolCalls {
				toolCalls = append(toolCalls, map[string]any{
					"id":   tc.Id,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				})
			}
			msg["tool_calls"] = toolCalls
		}
		messages = append(messages, msg)
	}

	body := map[string]any{
		"model":    c.config.Model,
		"messages": messages,
		"stream":   c.config.Stream,
	}

	if c.config.Temperature != nil {
		body["temperature"] = *c.config.Temperature
	}
	if c.config.TopP != nil {
		body["top_p"] = *c.config.TopP
	}
	if c.config.MaxTokens > 0 {
		body["max_tokens"] = c.config.MaxTokens
	}
	if len(c.config.ExtraBody) > 0 {
		for key, value := range c.config.ExtraBody {
			body[key] = value
		}
	}
	if options := ai.NormalizeRequestOptions(options); options != nil && options.PromptCacheEnabled {
		if options.PromptCacheKey != "" {
			body["prompt_cache_key"] = options.PromptCacheKey
		}
		if options.PromptCacheRetention != "" {
			body["prompt_cache_retention"] = options.PromptCacheRetention
		}
	}

	if len(tools) > 0 {
		toolDefs := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			toolDefs = append(toolDefs, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"parameters":  tool.Function.Parameters,
				},
			})
		}
		body["tools"] = toolDefs
	}

	return body
}

func (c *Client) doRequest(ctx context.Context, reqBody map[string]any) ([]*ai.ChatChoices, time.Duration, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimSpace(c.config.URL)
	if c.config.URLAutoComplete {
		endpoint = ResolveChatCompletionsURL(endpoint)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	applyRequestHeaders(req.Header, c.config.Headers)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
		}
	}

	if c.config.Stream {
		return c.ParseStreamResponse(resp.Body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	return c.parseJSONResponse(bytes.NewReader(body))
}

func (c *Client) parseJSONResponse(body io.Reader) ([]*ai.ChatChoices, time.Duration, error) {
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	trimmedBody := bytes.TrimSpace(bodyBytes)
	if bytes.HasPrefix(trimmedBody, []byte("data:")) {
		return nil, 0, fmt.Errorf("decode response: unexpected sse body for non-stream request")
	}

	var (
		found   bool
		lastErr error
		result  openAIChatResponse
	)
	for _, candidate := range extractJSONCandidates(string(bodyBytes)) {
		result = openAIChatResponse{}
		if err := json.Unmarshal([]byte(candidate), &result); err != nil {
			lastErr = err
			continue
		}
		if len(result.Choices) == 0 && (result.Error == nil || result.Error.Message == "") {
			continue
		}
		found = true
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, 0, fmt.Errorf("decode response: %w", lastErr)
	}
	if !found {
		return nil, 0, fmt.Errorf("decode response: no valid chat response candidate")
	}

	if result.Error != nil && result.Error.Message != "" {
		return nil, 0, &APIError{Message: result.Error.Message}
	}

	usage := normalizeTokenUsage(nil)
	if result.Usage != nil {
		usage = normalizeTokenUsage(result.Usage.toTokenUsage())
	}

	choices := make([]*ai.ChatChoices, 0, len(result.Choices))
	for _, item := range result.Choices {
		choice := item.toAIChoice()
		if choice == nil {
			continue
		}
		if choice.Message != nil {
			normalizeAssistantMessage(choice.Message)
		}
		if choice.Usage == nil {
			choice.Usage = usage
		} else {
			choice.Usage = normalizeTokenUsage(choice.Usage)
		}
		choices = append(choices, choice)
	}
	if len(choices) > 0 {
		last := choices[len(choices)-1]
		if last != nil && last.Usage != nil {
			c.lastUsage = last.Usage
		} else {
			c.lastUsage = usage
		}
	} else {
		c.lastUsage = usage
	}

	return choices, 0, nil
}

func (c *Client) LastTokenUsage() *ai.TokenUsage {
	if c == nil {
		return nil
	}
	return ai.NormalizeTokenUsagePtr(c.lastUsage)
}

// MaxRetries returns the configured retry count for diagnostics and tests.
func (c *Client) MaxRetries() int {
	if c == nil || c.config == nil {
		return 0
	}
	return c.config.MaxRetries
}

func (c *Client) UsagePricingModel() aiusage.PricingModel {
	if c == nil || c.config == nil {
		return aiusage.PricingModel{}
	}
	return c.config.UsagePricing
}

type openAIUsage struct {
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	ReasoningTokens       int
	CachedInputTokens     int
	CacheWriteInputTokens int

	ExcludesCachedTokens bool
}

func (u *openAIUsage) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*u = openAIUsage{}

	if value, path, ok := firstUsageIntByPaths(raw,
		"input_tokens",
		"inputTokens",
		"usage.input_tokens",
		"usage.inputTokens",
		"prompt_tokens",
		"promptTokens",
		"usage.prompt_tokens",
		"usage.promptTokens",
	); ok {
		u.InputTokens = value
		_ = path
	}

	if value, _, ok := firstUsageIntByPaths(raw,
		"output_tokens",
		"outputTokens",
		"usage.output_tokens",
		"usage.outputTokens",
		"completion_tokens",
		"completionTokens",
		"usage.completion_tokens",
		"usage.completionTokens",
		"response_tokens",
		"responseTokens",
	); ok {
		u.OutputTokens = value
	}

	if value, _, ok := firstUsageIntByPaths(raw,
		"total_tokens",
		"totalTokens",
		"usage.total_tokens",
		"usage.totalTokens",
	); ok {
		u.TotalTokens = value
	}

	if value, _, ok := firstUsageIntByPaths(raw,
		"reasoning_tokens",
		"reasoningTokens",
		"thought_tokens",
		"thoughtTokens",
		"usage.reasoning_tokens",
		"usage.reasoningTokens",
		"completion_tokens_details.reasoning_tokens",
		"completionTokensDetails.reasoningTokens",
		"output_tokens_details.reasoning_tokens",
		"outputTokensDetails.reasoningTokens",
	); ok {
		u.ReasoningTokens = value
	}

	if value, _, ok := firstUsageIntByPaths(raw,
		"cached_input_tokens",
		"cachedInputTokens",
		"cache_read_tokens",
		"cacheReadTokens",
		"cache_read_input_tokens",
		"cacheReadInputTokens",
		"prompt_cache_hit_tokens",
		"promptCacheHitTokens",
		"usage.cached_input_tokens",
		"usage.cachedInputTokens",
		"usage.cache_read_tokens",
		"usage.cacheReadTokens",
		"usage.cache_read_input_tokens",
		"usage.cacheReadInputTokens",
		"usage.prompt_cache_hit_tokens",
		"usage.promptCacheHitTokens",
		"prompt_tokens_details.cached_tokens",
		"promptTokensDetails.cachedTokens",
		"prompt_tokens_details.cache_read_tokens",
		"promptTokensDetails.cacheReadTokens",
		"input_tokens_details.cached_tokens",
		"inputTokensDetails.cachedTokens",
		"input_tokens_details.cache_read_tokens",
		"inputTokensDetails.cacheReadTokens",
		"input_tokens_details.cache_read_input_tokens",
		"inputTokensDetails.cacheReadInputTokens",
		"cache.read",
		"cache.cache_read_tokens",
		"cache.cacheReadTokens",
		"anthropic.cache_read_input_tokens",
		"anthropic.cacheReadInputTokens",
		"bedrock.usage.cacheReadInputTokens",
		"venice.usage.cacheReadInputTokens",
		"metadata.anthropic.cacheReadInputTokens",
		"metadata.bedrock.usage.cacheReadInputTokens",
		"metadata.venice.usage.cacheReadInputTokens",
	); ok {
		u.CachedInputTokens = value
	}

	if value, _, ok := firstUsageIntByPaths(raw,
		"cache_creation_input_tokens",
		"cacheCreationInputTokens",
		"cache_write_input_tokens",
		"cacheWriteInputTokens",
		"cache_write_tokens",
		"cacheWriteTokens",
		"prompt_cache_miss_tokens",
		"promptCacheMissTokens",
		"usage.cache_creation_input_tokens",
		"usage.cacheCreationInputTokens",
		"usage.cache_write_input_tokens",
		"usage.cacheWriteInputTokens",
		"usage.cache_write_tokens",
		"usage.cacheWriteTokens",
		"usage.prompt_cache_miss_tokens",
		"usage.promptCacheMissTokens",
		"prompt_tokens_details.cache_creation_tokens",
		"promptTokensDetails.cacheCreationTokens",
		"prompt_tokens_details.cache_write_tokens",
		"promptTokensDetails.cacheWriteTokens",
		"input_tokens_details.cache_creation_input_tokens",
		"inputTokensDetails.cacheCreationInputTokens",
		"input_tokens_details.cache_write_input_tokens",
		"inputTokensDetails.cacheWriteInputTokens",
		"input_tokens_details.cache_write_tokens",
		"inputTokensDetails.cacheWriteTokens",
		"cache.write",
		"cache.cache_write_tokens",
		"cache.cacheWriteTokens",
		"anthropic.cache_creation_input_tokens",
		"anthropic.cacheCreationInputTokens",
		"bedrock.usage.cacheWriteInputTokens",
		"venice.usage.cacheCreationInputTokens",
		"metadata.anthropic.cacheCreationInputTokens",
		"metadata.bedrock.usage.cacheWriteInputTokens",
		"metadata.venice.usage.cacheCreationInputTokens",
	); ok {
		u.CacheWriteInputTokens = value
	}

	u.ExcludesCachedTokens = hasAnyUsagePath(raw,
		"anthropic",
		"bedrock",
		"metadata.anthropic",
		"metadata.bedrock",
	)

	return nil
}

func (u *openAIUsage) toTokenUsage() *ai.TokenUsage {
	if u == nil {
		return nil
	}
	total := u.TotalTokens
	if total < 0 {
		total = 0
	}
	input := u.InputTokens
	if input < 0 {
		input = 0
	}
	output := u.OutputTokens
	if output <= 0 && u.TotalTokens > 0 && input > 0 {
		estimated := u.TotalTokens - input
		if estimated > 0 {
			output = estimated
		}
	}

	reasoning := u.ReasoningTokens
	cacheRead := u.CachedInputTokens
	cacheWrite := u.CacheWriteInputTokens

	if (cacheRead > 0 || cacheWrite > 0) && !u.ExcludesCachedTokens {
		adjusted := input - cacheRead - cacheWrite
		if adjusted < 0 {
			adjusted = 0
		}
		input = adjusted
	}
	// Some providers exclude cached tokens from inputTokens, so totalTokens from the SDK
	// might be missing or undercounted; recompute from the token components.
	if u.ExcludesCachedTokens {
		total = input + output + cacheRead + cacheWrite
	}

	usage := &ai.TokenUsage{
		TotalTokens:      total,
		InputTokens:      input,
		OutputTokens:     output,
		ReasoningTokens:  reasoning,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
	}
	usage.NormalizeInPlace()
	if usage.IsZero() {
		return nil
	}
	return usage
}

func firstUsageIntByPaths(raw map[string]any, paths ...string) (int, string, bool) {
	if len(raw) == 0 || len(paths) == 0 {
		return 0, "", false
	}
	for _, path := range paths {
		value, ok := usageValueByPath(raw, path)
		if !ok {
			continue
		}
		parsed, ok := usageIntFromAny(value)
		if !ok {
			continue
		}
		if parsed < 0 {
			parsed = 0
		}
		return parsed, path, true
	}
	return 0, "", false
}

func hasAnyUsagePath(raw map[string]any, paths ...string) bool {
	if len(raw) == 0 || len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if _, ok := usageValueByPath(raw, path); ok {
			return true
		}
	}
	return false
}

func usageValueByPath(raw map[string]any, path string) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}

	current := any(raw)
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, false
		}

		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := nextMap[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func usageIntFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		if v > math.MaxInt {
			return math.MaxInt, true
		}
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		if v > uint64(math.MaxInt) {
			return math.MaxInt, true
		}
		return int(v), true
	case float32:
		value64 := float64(v)
		if !isFiniteFloat(value64) {
			return 0, false
		}
		return int(value64), true
	case float64:
		if !isFiniteFloat(v) {
			return 0, false
		}
		return int(v), true
	case json.Number:
		if iv, err := v.Int64(); err == nil {
			return int(iv), true
		}
		fv, err := v.Float64()
		if err != nil || !isFiniteFloat(fv) {
			return 0, false
		}
		return int(fv), true
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, false
		}
		if iv, err := strconv.ParseInt(text, 10, 64); err == nil {
			if iv > int64(math.MaxInt) {
				return math.MaxInt, true
			}
			if iv < int64(math.MinInt) {
				return math.MinInt, true
			}
			return int(iv), true
		}
		fv, err := strconv.ParseFloat(text, 64)
		if err != nil || !isFiniteFloat(fv) {
			return 0, false
		}
		if fv > float64(math.MaxInt) {
			return math.MaxInt, true
		}
		if fv < float64(math.MinInt) {
			return math.MinInt, true
		}
		return int(fv), true
	default:
		return 0, false
	}
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func normalizeTokenUsage(usage *ai.TokenUsage) *ai.TokenUsage {
	return ai.NormalizeTokenUsagePtr(usage)
}

func (c *Client) backoffDuration(attempt int) time.Duration {
	base := time.Second
	for i := 0; i < attempt; i++ {
		base *= 2
	}
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	return base
}

func AttemptTimeoutForAttempt(base time.Duration, attempt int) time.Duration {
	return ai.AttemptTimeoutForAttempt(base, attempt)
}


func extractContent(choice *ai.ChatChoices) string {
	if choice == nil || choice.Message == nil {
		return ""
	}
	switch v := choice.Message.Content.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func applyRequestHeaders(dst http.Header, headers map[string]string) {
	if len(headers) == 0 {
		return
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		dst.Set(key, value)
	}
}

func (c *Client) Embedding(ctx context.Context, texts []string, model string) ([][]float32, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	debugHTTP := shouldDebugEmbeddingHTTP()

	reqBody := map[string]any{
		"model": model,
		"input": texts,
	}
	if len(c.config.ExtraBody) > 0 {
		for key, value := range c.config.ExtraBody {
			reqBody[key] = value
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := strings.TrimSpace(c.config.URL)
	if c.config.URLAutoComplete {
		endpoint = ResolveEmbeddingsURL(endpoint)
	}

	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		attemptTimeout := AttemptTimeoutForAttempt(c.config.Timeout, attempt)
		attemptCtx := ctx
		cancelAttempt := func() {}
		var attemptErr error
		if attemptTimeout > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, attemptTimeout)
		}

		req, err := http.NewRequestWithContext(attemptCtx, "POST", endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			cancelAttempt()
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.config.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
		}
		applyRequestHeaders(req.Header, c.config.Headers)
		if debugHTTP {
			log.Printf("[openai] embedding request packet:\n%s", formatDebugHTTPRequest(req, bodyBytes))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			cancelAttempt()
			attemptErr = fmt.Errorf("http request: %w", err)
		} else {
			respBody, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			cancelAttempt()
			if readErr != nil {
				attemptErr = fmt.Errorf("read response: %w", readErr)
			} else if debugHTTP {
				log.Printf("[openai] embedding response packet:\n%s", formatDebugHTTPResponse(resp, respBody))
			}

			if attemptErr == nil && resp.StatusCode != http.StatusOK {
				attemptErr = &HTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
			}
			if attemptErr == nil {
				embeddings, parseErr := parseEmbeddingResponse(respBody)
				if parseErr != nil {
					attemptErr = fmt.Errorf("failed to decode response: %w", parseErr)
				} else {
					return embeddings, nil
				}
			}
		}

		lastErr = attemptErr
		retryDecision := BuildRetryDecision(attemptErr, NormalizeRetryCodes(c.config.RetryCodes))
		if !retryDecision.Retry {
			return nil, attemptErr
		}
		if attempt < c.config.MaxRetries {
			waitDuration := c.backoffDuration(attempt + 1)
			c.reportRetryAttempt(RetryEvent{
				Attempt:     attempt + 1,
				MaxAttempts: c.config.MaxRetries + 1,
				Delay:       waitDuration,
				Next:        time.Now().Add(waitDuration),
				Message:     retryDecision.Message,
			}, attemptErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitDuration):
			}
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func parseEmbeddingResponse(respBody []byte) ([][]float32, error) {
	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
		Data       []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if len(result.Embeddings) > 0 {
		return result.Embeddings, nil
	}
	if len(result.Data) > 0 {
		embeddings := make([][]float32, len(result.Data))
		for i, data := range result.Data {
			embeddings[i] = data.Embedding
		}
		return embeddings, nil
	}

	return nil, fmt.Errorf("no embeddings field found")
}

func shouldDebugEmbeddingHTTP() bool {
	raw := strings.TrimSpace(os.Getenv("SASTPRO_DEBUG_EMBED_HTTP"))
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func formatDebugHTTPRequest(req *http.Request, body []byte) string {
	if req == nil {
		return "(nil request)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s HTTP/1.1\n", req.Method, req.URL.String())
	for key, values := range redactDebugHeaders(req.Header) {
		for _, value := range values {
			fmt.Fprintf(&b, "%s: %s\n", key, value)
		}
	}
	if len(body) > 0 {
		b.WriteString("\n")
		b.Write(body)
	}
	return b.String()
}

func formatDebugHTTPResponse(resp *http.Response, body []byte) string {
	if resp == nil {
		return "(nil response)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %s\n", resp.Status)
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Fprintf(&b, "%s: %s\n", key, value)
		}
	}
	if len(body) > 0 {
		b.WriteString("\n")
		b.Write(body)
	}
	return b.String()
}

func redactDebugHeaders(header http.Header) http.Header {
	if len(header) == 0 {
		return http.Header{}
	}
	out := make(http.Header, len(header))
	for key, values := range header {
		copied := append([]string(nil), values...)
		if strings.EqualFold(key, "Authorization") {
			copied = []string{"Bearer ***REDACTED***"}
		}
		out[key] = copied
	}
	return out
}

func (c *Client) ModelContextInfo() ai.ModelContextInfo {
	if c == nil || c.config == nil {
		return ai.ModelContextInfo{}
	}
	return ai.ModelContextInfo{
		ModelName:           c.config.Model,
		ContextWindowTokens: c.config.ContextWindowTokens,
		InputTokenLimit:     c.config.InputTokenLimit,
		OutputTokenLimit:    c.config.OutputTokenLimit,
		SupportsVision:      c.config.SupportsVision,
		SupportsAudio:       c.config.SupportsAudio,
	}.Normalize()
}
