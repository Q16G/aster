package structuredoutput

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"aster/internal/ai"
)

const DefaultRetryCount = 3

type Config struct {
	RetryCount          int  `json:"retry_count,omitempty"`
	LogErrorsToTerminal bool `json:"log_errors_to_terminal,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		RetryCount:          DefaultRetryCount,
		LogErrorsToTerminal: true,
	}
}

func NormalizeConfig(cfg Config) Config {
	if cfg == (Config{}) {
		return DefaultConfig()
	}
	if cfg.RetryCount <= 0 {
		cfg.RetryCount = DefaultRetryCount
	}
	return cfg
}

type ErrorType string

const (
	ErrorTypeModelCallFailed  ErrorType = "model_call_failed"
	ErrorTypeMissingJSON      ErrorType = "missing_json_object"
	ErrorTypeUnmarshalFailed  ErrorType = "unmarshal_failed"
	ErrorTypeValidationFailed ErrorType = "validation_failed"
)

type ParseError struct {
	Type ErrorType
	Err  error
}

func (e *ParseError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Type)
}

func (e *ParseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func MissingJSONObjectError(msg string) error {
	return &ParseError{Type: ErrorTypeMissingJSON, Err: errors.New(strings.TrimSpace(msg))}
}

func UnmarshalFailedError(err error) error {
	return &ParseError{Type: ErrorTypeUnmarshalFailed, Err: err}
}

func ValidationFailedError(err error) error {
	return &ParseError{Type: ErrorTypeValidationFailed, Err: err}
}

type AttemptFailure struct {
	Attempt         int       `json:"attempt"`
	ErrorType       ErrorType `json:"error_type"`
	Error           string    `json:"error"`
	ResponseExcerpt string    `json:"response_excerpt,omitempty"`
}

type ExhaustedError struct {
	Phase        string           `json:"phase"`
	MaxAttempts  int              `json:"max_attempts"`
	Attempts     []AttemptFailure `json:"attempts"`
	LastResponse string           `json:"last_response,omitempty"`
}

func (e *ExhaustedError) Error() string {
	if e == nil {
		return ""
	}
	last := e.LastAttempt()
	if last == nil {
		return fmt.Sprintf("%s structured output retry exhausted after %d attempts", strings.TrimSpace(e.Phase), e.MaxAttempts)
	}
	return fmt.Sprintf(
		"%s structured output retry exhausted after %d attempts: %s (%s)",
		strings.TrimSpace(e.Phase),
		e.MaxAttempts,
		strings.TrimSpace(last.ErrorType.String()),
		strings.TrimSpace(last.Error),
	)
}

func (e *ExhaustedError) LastAttempt() *AttemptFailure {
	if e == nil || len(e.Attempts) == 0 {
		return nil
	}
	last := e.Attempts[len(e.Attempts)-1]
	return &last
}

type LogEvent struct {
	Event           string `json:"event"`
	Phase           string `json:"phase"`
	Attempt         int    `json:"attempt,omitempty"`
	MaxAttempts     int    `json:"max_attempts,omitempty"`
	ErrorType       string `json:"error_type,omitempty"`
	Error           string `json:"error,omitempty"`
	ResponseExcerpt string `json:"response_excerpt,omitempty"`
}

type Logger func(event LogEvent)

type Result[T any] struct {
	Value       T
	RawResponse string
	Attempts    int
}

type ParseFunc[T any] func(raw string) (T, error)

func RunWithRetry[T any](ctx context.Context, client ai.ChatClient, phase string, prompt string, fallback Config, parse ParseFunc[T]) (Result[T], error) {
	var zero Result[T]
	if client == nil {
		return zero, fmt.Errorf("chat client is nil")
	}
	parse = ensureParseFunc(parse)
	cfg := ResolveConfig(ctx, fallback)
	logger := LoggerFromContext(ctx)

	phase = strings.TrimSpace(phase)
	prompt = strings.TrimSpace(prompt)
	if phase == "" {
		phase = "structured_output"
	}
	if prompt == "" {
		return zero, fmt.Errorf("prompt is empty")
	}

	emitLog(cfg, logger, LogEvent{
		Event:       "structured_retry_started",
		Phase:       phase,
		MaxAttempts: cfg.RetryCount,
	})

	currentPrompt := prompt
	failures := make([]AttemptFailure, 0, cfg.RetryCount)
	result := zero
	for attempt := 1; attempt <= cfg.RetryCount; attempt++ {
		rawResponse, err := ai.ChatTextWithOptions(ctx, client, currentPrompt, &ai.RequestOptions{PromptFamily: phase})
		rawResponse = strings.TrimSpace(rawResponse)
		result.RawResponse = rawResponse
		result.Attempts = attempt
		if err == nil {
			value, parseErr := parse(rawResponse)
			if parseErr == nil {
				result.Value = value
				if attempt > 1 {
					emitLog(cfg, logger, LogEvent{
						Event:       "structured_retry_succeeded",
						Phase:       phase,
						Attempt:     attempt,
						MaxAttempts: cfg.RetryCount,
					})
				}
				return result, nil
			}
			failures = append(failures, buildAttemptFailure(attempt, parseErr, rawResponse))
		} else {
			failures = append(failures, AttemptFailure{
				Attempt:         attempt,
				ErrorType:       ErrorTypeModelCallFailed,
				Error:           strings.TrimSpace(err.Error()),
				ResponseExcerpt: responseExcerpt(rawResponse),
			})
		}

		last := failures[len(failures)-1]
		emitLog(cfg, logger, LogEvent{
			Event:           "structured_retry_attempt_failed",
			Phase:           phase,
			Attempt:         attempt,
			MaxAttempts:     cfg.RetryCount,
			ErrorType:       string(last.ErrorType),
			Error:           last.Error,
			ResponseExcerpt: last.ResponseExcerpt,
		})
		if attempt >= cfg.RetryCount {
			break
		}
		currentPrompt = buildRetryPrompt(prompt, phase, attempt+1, last)
	}

	exhausted := &ExhaustedError{
		Phase:        phase,
		MaxAttempts:  cfg.RetryCount,
		Attempts:     failures,
		LastResponse: result.RawResponse,
	}
	last := exhausted.LastAttempt()
	errorType := ""
	errorText := ""
	responseText := ""
	if last != nil {
		errorType = string(last.ErrorType)
		errorText = last.Error
		responseText = last.ResponseExcerpt
	}
	emitLog(cfg, logger, LogEvent{
		Event:           "structured_retry_exhausted",
		Phase:           phase,
		Attempt:         cfg.RetryCount,
		MaxAttempts:     cfg.RetryCount,
		ErrorType:       errorType,
		Error:           errorText,
		ResponseExcerpt: responseText,
	})
	return result, exhausted
}

func buildAttemptFailure(attempt int, err error, rawResponse string) AttemptFailure {
	return AttemptFailure{
		Attempt:         attempt,
		ErrorType:       classifyErrorType(err),
		Error:           strings.TrimSpace(err.Error()),
		ResponseExcerpt: responseExcerpt(rawResponse),
	}
}

func classifyErrorType(err error) ErrorType {
	var parseErr *ParseError
	if errors.As(err, &parseErr) && parseErr != nil && parseErr.Type != "" {
		return parseErr.Type
	}
	return ErrorTypeValidationFailed
}

func buildRetryPrompt(basePrompt string, phase string, nextAttempt int, last AttemptFailure) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(basePrompt))
	b.WriteString("\n\n# Structured Output Retry\n")
	b.WriteString("上一次输出没有满足 JSON-schema / 结构化输出要求，请严格修正。\n")
	b.WriteString("你必须只返回满足要求的 JSON，不要输出 Markdown、解释、前后缀或多余文本。\n")
	b.WriteString(fmt.Sprintf("phase: %s\n", strings.TrimSpace(phase)))
	b.WriteString(fmt.Sprintf("next_attempt: %d\n", nextAttempt))
	b.WriteString(fmt.Sprintf("last_error_type: %s\n", strings.TrimSpace(string(last.ErrorType))))
	b.WriteString(fmt.Sprintf("last_error: %s\n", strings.TrimSpace(last.Error)))
	if strings.TrimSpace(last.ResponseExcerpt) != "" {
		b.WriteString(fmt.Sprintf("last_response_excerpt: %s\n", strings.TrimSpace(last.ResponseExcerpt)))
	}
	return b.String()
}

func responseExcerpt(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\n", " | ")
	raw = strings.ReplaceAll(raw, "\t", " ")
	const maxLen = 240
	if len(raw) <= maxLen {
		return raw
	}
	return strings.TrimSpace(raw[:maxLen]) + "..."
}

func ensureParseFunc[T any](parse ParseFunc[T]) ParseFunc[T] {
	if parse != nil {
		return parse
	}
	return func(string) (T, error) {
		var zero T
		return zero, fmt.Errorf("parse func is nil")
	}
}

func emitLog(cfg Config, logger Logger, event LogEvent) {
	cfg = NormalizeConfig(cfg)
	if !cfg.LogErrorsToTerminal || logger == nil {
		return
	}
	logger(event)
}

func (e ErrorType) String() string {
	return string(e)
}

type configKey struct{}
type loggerKey struct{}

func WithConfig(ctx context.Context, cfg Config) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, configKey{}, NormalizeConfig(cfg))
}

func ResolveConfig(ctx context.Context, fallback Config) Config {
	if ctx != nil {
		if cfg, ok := ctx.Value(configKey{}).(Config); ok {
			return NormalizeConfig(cfg)
		}
	}
	return NormalizeConfig(fallback)
}

func WithLogger(ctx context.Context, logger Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

func LoggerFromContext(ctx context.Context) Logger {
	if ctx == nil {
		return nil
	}
	logger, _ := ctx.Value(loggerKey{}).(Logger)
	return logger
}

func LastResponse(err error) string {
	var exhausted *ExhaustedError
	if !errors.As(err, &exhausted) || exhausted == nil {
		return ""
	}
	return strings.TrimSpace(exhausted.LastResponse)
}
