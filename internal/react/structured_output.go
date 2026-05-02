package react

import (
	"context"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/structuredoutput"
)

func (a *Agent) resolveStructuredOutputConfig(execCfg *ExecuteConfig) structuredoutput.Config {
	cfg := structuredoutput.DefaultConfig()
	if a != nil && a.cfg != nil {
		cfg = structuredoutput.NormalizeConfig(a.cfg.StructuredOutput)
	}
	if execCfg != nil && execCfg.structuredOutputRetryCount != nil && *execCfg.structuredOutputRetryCount > 0 {
		cfg.RetryCount = *execCfg.structuredOutputRetryCount
	}
	return structuredoutput.NormalizeConfig(cfg)
}

func (a *Agent) structuredOutputLogger(snapshot builtin_tools.StateSnapshot) structuredoutput.Logger {
	return func(event structuredoutput.LogEvent) {
		if a == nil {
			return
		}
		level := "info"
		switch strings.TrimSpace(event.Event) {
		case "structured_retry_attempt_failed":
			level = "warning"
		case "structured_retry_exhausted":
			level = "error"
		}
		a.emitRuntimeLog(level, "structured output retry", snapshot, map[string]any{
			"event":                   strings.TrimSpace(event.Event),
			"structured_output_phase": strings.TrimSpace(event.Phase),
			"attempt":                 event.Attempt,
			"max_attempts":            event.MaxAttempts,
			"error_type":              strings.TrimSpace(event.ErrorType),
			"error":                   strings.TrimSpace(event.Error),
			"response_excerpt":        strings.TrimSpace(event.ResponseExcerpt),
		})
	}
}

func runStructuredOutputWithRetry[T any](a *Agent, ctx context.Context, snapshot builtin_tools.StateSnapshot, client ai.ChatClient, phase string, prompt string, parse structuredoutput.ParseFunc[T]) (structuredoutput.Result[T], error) {
	retryCtx := structuredoutput.WithLogger(ctx, a.structuredOutputLogger(snapshot))
	return structuredoutput.RunWithRetry(retryCtx, client, phase, prompt, a.resolveStructuredOutputConfig(nil), parse)
}
