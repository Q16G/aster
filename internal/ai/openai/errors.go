package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

type APIError struct {
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error: %s", e.Message)
}

type RetryDecision struct {
	Retry            bool
	Message          string
	ReasonCode       string
	UserMessage      string
	SuggestedActions []string
}

type providerErrorDetails struct {
	Message string
	Code    string
	Type    string
}

const (
	RetryReasonProviderQuota     = "provider_quota"
	RetryReasonProviderAuth      = "provider_auth"
	RetryReasonRateLimit         = "rate_limit_transient"
	RetryReasonProviderTransient = "provider_transient"
	RetryReasonRequestTimeout    = "request_timeout"
	RetryReasonConnectionIssue   = "connection_interrupted"
)

func IsRetryableError(err error, retryCodes []int) bool {
	return BuildRetryDecision(err, retryCodes).Retry
}

func BuildRetryDecision(err error, retryCodes []int) RetryDecision {
	if err == nil {
		return RetryDecision{}
	}
	if errors.Is(err, context.Canceled) {
		return RetryDecision{}
	}

	// Per-attempt deadline exceeded is retryable; caller's outer ctx cancellation is handled separately.
	if errors.Is(err, context.DeadlineExceeded) {
		return RetryDecision{
			Retry:       true,
			Message:     "Request timed out",
			ReasonCode:  RetryReasonRequestTimeout,
			UserMessage: "请求超时，系统会自动重试。",
			SuggestedActions: []string{
				"稍后重试当前请求",
			},
		}
	}

	// Retry on transient I/O failures that commonly happen on broken connections.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return RetryDecision{
			Retry:       true,
			Message:     "Connection interrupted",
			ReasonCode:  RetryReasonConnectionIssue,
			UserMessage: "连接中断，系统会自动重试。",
			SuggestedActions: []string{
				"稍后重试当前请求",
				"检查网络连通性或代理配置",
			},
		}
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr != nil {
		return buildHTTPRetryDecision(httpErr, retryCodes)
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		if isContextOverflowMessage(strings.ToLower(strings.TrimSpace(apiErr.Message))) {
			return RetryDecision{}
		}
	}

	// Retry on common network errors (timeout, connection reset, broken pipe, etc.).
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil {
		if netErr.Timeout() {
			return RetryDecision{
				Retry:       true,
				Message:     "Request timed out",
				ReasonCode:  RetryReasonRequestTimeout,
				UserMessage: "请求超时，系统会自动重试。",
				SuggestedActions: []string{
					"稍后重试当前请求",
				},
			}
		}
		return RetryDecision{
			Retry:       true,
			Message:     "Connection error",
			ReasonCode:  RetryReasonConnectionIssue,
			UserMessage: "网络连接异常，系统会自动重试。",
			SuggestedActions: []string{
				"稍后重试当前请求",
				"检查网络连通性或代理配置",
			},
		}
	}

	// All explicitly non-retryable cases (Canceled, auth, quota, context overflow) are
	// handled above. Remaining unrecognized errors are assumed transient and retried.
	return RetryDecision{
		Retry:       true,
		Message:     fmt.Sprintf("Transient error: %v", err),
		ReasonCode:  RetryReasonProviderTransient,
		UserMessage: "遇到临时错误，系统会自动重试。",
		SuggestedActions: []string{
			"稍后重试当前请求",
		},
	}
}

func BuildRetryDecisionFromText(text string, retryCodes []int) RetryDecision {
	_ = retryCodes
	text = strings.TrimSpace(text)
	if text == "" {
		return RetryDecision{}
	}
	if strings.HasPrefix(text, "HTTP ") {
		if idx := strings.Index(text, ":"); idx > len("HTTP ") {
			codeText := strings.TrimSpace(strings.TrimPrefix(text[:idx], "HTTP"))
			if code, err := strconv.Atoi(codeText); err == nil {
				return buildHTTPRetryDecision(&HTTPError{
					StatusCode: code,
					Body:       strings.TrimSpace(text[idx+1:]),
				}, retryCodes)
			}
		}
	}

	haystack := strings.ToLower(text)
	if isProviderQuotaMessage(haystack) {
		return nonRetryableQuotaDecision(providerErrorDetails{})
	}
	if isProviderAuthMessage(0, haystack) {
		return nonRetryableAuthDecision(providerErrorDetails{})
	}
	if strings.Contains(haystack, "rate limit") || strings.Contains(haystack, "too many requests") || strings.Contains(haystack, " 429") || strings.HasPrefix(haystack, "429") {
		return retryableRateLimitDecision(http.StatusTooManyRequests, providerErrorDetails{})
	}
	return RetryDecision{}
}

func buildHTTPRetryDecision(httpErr *HTTPError, retryCodes []int) RetryDecision {
	if httpErr == nil {
		return RetryDecision{}
	}

	details := parseProviderErrorDetails(httpErr.Body)
	haystack := strings.ToLower(strings.Join([]string{
		details.Message,
		details.Code,
		details.Type,
		httpErr.Body,
	}, " "))

	if isContextOverflowMessage(haystack) {
		return RetryDecision{}
	}
	if isProviderQuotaMessage(haystack) {
		return nonRetryableQuotaDecision(details)
	}
	if isProviderAuthMessage(httpErr.StatusCode, haystack) {
		return nonRetryableAuthDecision(details)
	}

	if isRetryableHTTPStatus(httpErr.StatusCode, retryCodes, haystack) {
		if httpErr.StatusCode == http.StatusTooManyRequests || strings.Contains(haystack, "rate limit") || strings.Contains(haystack, "too many requests") {
			return retryableRateLimitDecision(httpErr.StatusCode, details)
		}
		return retryableProviderDecision(httpErr.StatusCode, details)
	}

	return RetryDecision{}
}

func parseProviderErrorDetails(body string) providerErrorDetails {
	body = strings.TrimSpace(body)
	if body == "" {
		return providerErrorDetails{}
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return providerErrorDetails{}
	}
	return providerErrorDetails{
		Message: strings.TrimSpace(payload.Error.Message),
		Code:    strings.TrimSpace(payload.Error.Code),
		Type:    strings.TrimSpace(payload.Error.Type),
	}
}

func isNonRetryableProviderError(statusCode int, haystack string) bool {
	return isProviderAuthMessage(statusCode, haystack) || isProviderQuotaMessage(haystack)
}

func isProviderQuotaMessage(haystack string) bool {
	for _, marker := range []string{
		"insufficient_quota",
		"usage_not_included",
		"billing",
		"subscription",
		"credit balance",
		"payment required",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func isProviderAuthMessage(statusCode int, haystack string) bool {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return true
	}
	for _, marker := range []string{
		"invalid_api_key",
		"invalid api key",
		"authentication",
		"unauthorized",
		"forbidden",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func isContextOverflowMessage(haystack string) bool {
	if haystack == "" {
		return false
	}
	markers := []string{
		"context_length_exceeded",
		"maximum context length",
		"context window",
		"context length",
		"prompt is too long",
		"input is too long",
		"token limit",
		"too many tokens",
		"context overflow",
	}
	for _, marker := range markers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func isRetryableHTTPStatus(statusCode int, retryCodes []int, haystack string) bool {
	switch statusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}

	retryableMarkers := []string{
		"rate limit",
		"too many requests",
		"temporarily unavailable",
		"overloaded",
		"capacity",
		"please retry",
		"server error",
	}
	for _, marker := range retryableMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}

	for _, code := range NormalizeRetryCodes(retryCodes) {
		if code == statusCode {
			return true
		}
	}
	return false
}

func retryStatusMessage(statusCode int, details providerErrorDetails) string {
	switch statusCode {
	case http.StatusTooManyRequests:
		return "Too Many Requests"
	case http.StatusInternalServerError:
		return "Internal Server Error"
	case http.StatusBadGateway:
		return "Bad Gateway"
	case http.StatusServiceUnavailable:
		return "Service Unavailable"
	case http.StatusGatewayTimeout:
		return "Gateway Timeout"
	}
	if text := strings.TrimSpace(http.StatusText(statusCode)); text != "" {
		return text
	}
	if text := strings.TrimSpace(details.Message); text != "" {
		return text
	}
	return "Request failed"
}

func nonRetryableQuotaDecision(details providerErrorDetails) RetryDecision {
	return RetryDecision{
		Retry:       false,
		Message:     firstNonEmptyStatusMessage("Insufficient quota", details),
		ReasonCode:  RetryReasonProviderQuota,
		UserMessage: "当前 provider 配额已耗尽，本次不会自动重试。",
		SuggestedActions: []string{
			"检查 provider 的 billing 或 quota 状态",
			"切换到仍有额度的 provider 或 model",
			"额度恢复后重新执行未完成步骤",
		},
	}
}

func nonRetryableAuthDecision(details providerErrorDetails) RetryDecision {
	return RetryDecision{
		Retry:       false,
		Message:     firstNonEmptyStatusMessage("Authentication failed", details),
		ReasonCode:  RetryReasonProviderAuth,
		UserMessage: "当前 provider 认证或权限异常，本次不会自动重试。",
		SuggestedActions: []string{
			"检查 API key、账号权限或 scope 配置",
			"确认当前 provider 账号仍可正常访问目标模型",
		},
	}
}

func retryableRateLimitDecision(statusCode int, details providerErrorDetails) RetryDecision {
	return RetryDecision{
		Retry:       true,
		Message:     retryStatusMessage(statusCode, details),
		ReasonCode:  RetryReasonRateLimit,
		UserMessage: "当前 provider 正在限流，系统会自动重试。",
		SuggestedActions: []string{
			"稍后重试当前请求",
			"必要时切换到负载更低的 provider 或 model",
		},
	}
}

func retryableProviderDecision(statusCode int, details providerErrorDetails) RetryDecision {
	return RetryDecision{
		Retry:       true,
		Message:     retryStatusMessage(statusCode, details),
		ReasonCode:  RetryReasonProviderTransient,
		UserMessage: "当前 provider 临时不可用，系统会自动重试。",
		SuggestedActions: []string{
			"稍后重试当前请求",
			"必要时切换到其他 provider 或 model",
		},
	}
}

func firstNonEmptyStatusMessage(fallback string, details providerErrorDetails) string {
	if text := strings.TrimSpace(details.Message); text != "" {
		return text
	}
	return fallback
}

func NormalizeRetryCodes(codes []int) []int {
	if len(codes) == 0 {
		return append([]int(nil), defaultRetryCodes...)
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
		return append([]int(nil), defaultRetryCodes...)
	}
	return normalized
}
