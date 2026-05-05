package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	Retry   bool
	Message string
}

type providerErrorDetails struct {
	Message string
	Code    string
	Type    string
}

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
		return RetryDecision{Retry: true, Message: "Request timed out"}
	}

	// Retry on transient I/O failures that commonly happen on broken connections.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return RetryDecision{Retry: true, Message: "Connection interrupted"}
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

	// Retry on common network errors.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil && netErr.Timeout() {
		return RetryDecision{Retry: true, Message: "Request timed out"}
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
	if isNonRetryableProviderError(httpErr.StatusCode, haystack) {
		return RetryDecision{}
	}

	if isRetryableHTTPStatus(httpErr.StatusCode, retryCodes, haystack) {
		return RetryDecision{
			Retry:   true,
			Message: retryStatusMessage(httpErr.StatusCode, details),
		}
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
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return true
	}

	nonRetryableMarkers := []string{
		"insufficient_quota",
		"usage_not_included",
		"invalid_api_key",
		"invalid api key",
		"authentication",
		"unauthorized",
		"billing",
		"subscription",
		"credit balance",
		"payment required",
	}
	for _, marker := range nonRetryableMarkers {
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
