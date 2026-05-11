package react

import (
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// turnInterruptRaised is a sentinel error used to unwind the scheduler loop when
// a durable human-in-the-loop interrupt is raised.
//
// It is not a failure: the current turn ends in "interrupted" and the session
// transitions to WAITING_FOR_HUMAN.
type turnInterruptRaised struct {
	pending  *builtin_tools.PendingInterrupt
	toolCall *ai.FunctionTool
}

func (e *turnInterruptRaised) Error() string {
	if e == nil || e.pending == nil {
		return "interrupt raised"
	}
	q := strings.TrimSpace(e.pending.Question)
	if q == "" {
		return fmt.Sprintf("interrupt raised: %s", strings.TrimSpace(e.pending.InterruptID))
	}
	return "interrupt raised: " + q
}

func (e *turnInterruptRaised) Pending() *builtin_tools.PendingInterrupt {
	if e == nil {
		return nil
	}
	return e.pending
}

func (e *turnInterruptRaised) ToolCall() *ai.FunctionTool {
	if e == nil {
		return nil
	}
	return e.toolCall
}

func isTurnInterruptRaised(err error) (*turnInterruptRaised, bool) {
	if err == nil {
		return nil, false
	}
	tri, ok := err.(*turnInterruptRaised)
	return tri, ok
}

func parseHumanConfirmArgs(args map[string]any) (question string, inputType string, options []string, ctxMap map[string]any) {
	if args == nil {
		return "", "text", nil, nil
	}
	if v, ok := args["question"].(string); ok {
		question = strings.TrimSpace(v)
	}
	inputType = "text"
	if v, ok := args["input_type"].(string); ok && strings.TrimSpace(v) != "" {
		inputType = strings.TrimSpace(v)
	}
	switch inputType {
	case "text", "single_choice", "multi_choice", "structured":
	default:
		inputType = "text"
	}

	options = nil
	if raw := args["options"]; raw != nil {
		switch v := raw.(type) {
		case []string:
			for _, s := range v {
				s = strings.TrimSpace(s)
				if s != "" {
					options = append(options, s)
				}
			}
		case []any:
			for _, it := range v {
				s, _ := it.(string)
				s = strings.TrimSpace(s)
				if s != "" {
					options = append(options, s)
				}
			}
		}
		if len(options) == 0 {
			options = nil
		}
	}
	if raw := args["context"]; raw != nil {
		if m, ok := raw.(map[string]any); ok && len(m) > 0 {
			ctxMap = builtin_tools.CloneAnyMap(m)
		}
	}
	return question, inputType, options, ctxMap
}

func buildHumanConfirmToolResultJSON(interruptID, inputType, answer string) string {
	answer = strings.TrimSpace(answer)
	interruptID = strings.TrimSpace(interruptID)
	inputType = strings.TrimSpace(inputType)
	if inputType == "" {
		inputType = "text"
	}

	var value any = answer
	switch inputType {
	case "multi_choice", "structured":
		var parsed any
		if err := json.Unmarshal([]byte(answer), &parsed); err == nil {
			value = parsed
		}
	}

	out, _ := json.Marshal(map[string]any{
		"ok":           true,
		"interrupt_id": interruptID,
		// Keep legacy naming so existing prompts/tool consumers remain stable.
		"request_id": interruptID,
		"type":       inputType,
		"answer":     answer,
		"value":      value,
	})
	return string(out)
}
