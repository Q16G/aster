package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

func IsRetryableError(err error, retryCodes []int) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Per-attempt deadline exceeded is retryable; caller's outer ctx cancellation is handled separately.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Retry on transient I/O failures that commonly happen on broken connections.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr != nil {
		if len(retryCodes) == 0 {
			retryCodes = defaultRetryCodes
		}
		for _, code := range retryCodes {
			if code == httpErr.StatusCode {
				return true
			}
		}
		return false
	}

	// Retry on common network errors.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil {
		return netErr.Timeout()
	}

	return false
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
