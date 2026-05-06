package openai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"aster/internal/ai"
)

func (c *Client) ParseStreamResponse(body io.Reader) ([]*ai.ChatChoices, time.Duration, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	choicesMap := make(map[int]*streamChoiceAggregate)
	var aggregatedUsage *ai.TokenUsage
	debugRawSSE := rawSSEDebugEnabled()
	lineNo := 0
	seenDataLine := false

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if debugRawSSE {
			log.Printf("[openai.raw_sse] line=%d raw=%s", lineNo, line)
		}

		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			if isStandardSSEField(line) {
				continue
			}
			return nil, 0, fmt.Errorf("invalid sse line %d: unrecognized non-data content", lineNo)
		}
		seenDataLine = true

		data := line
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimPrefix(line, "data:")
		}

		if data == "" || data == "[DONE]" {
			continue
		}

		parsedChunk := false
		candidates := extractJSONCandidates(data)
		if debugRawSSE {
			for idx, candidate := range candidates {
				log.Printf("[openai.raw_sse] line=%d candidate=%d json=%s", lineNo, idx+1, candidate)
			}
		}
		for _, candidate := range candidates {
			var chunk streamChunk
			if err := json.Unmarshal([]byte(candidate), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 && chunk.Usage == nil && (chunk.Error == nil || chunk.Error.Message == "") {
				continue
			}
			parsedChunk = true

			if chunk.Error != nil && chunk.Error.Message != "" {
				return nil, 0, &APIError{Message: chunk.Error.Message}
			}
			if chunk.Usage != nil {
				aggregatedUsage = normalizeTokenUsage(chunk.Usage.toTokenUsage())
			}

			for _, choice := range chunk.Choices {
				idx := choice.Index
				if choicesMap[idx] == nil {
					choicesMap[idx] = newStreamChoiceAggregate(idx)
				}

				existing := choicesMap[idx]
				contentDelta, reasoningDelta := existing.applyDelta(choice.Delta)
				var normalizedToolCalls []*ai.FunctionTool

				if len(choice.Delta.ToolCalls) > 0 {
					existing.choice.Message.ToolCalls, normalizedToolCalls = mergeToolCalls(
						existing.choice.Message.ToolCalls,
						choice.Delta.ToolCalls,
					)
				}

				if choice.FinishReason != "" {
					existing.choice.FinishReason = choice.FinishReason
				}

				if c.config.StreamFunc != nil && (contentDelta != "" || reasoningDelta != "" || len(normalizedToolCalls) > 0 || choice.FinishReason != "") {
					c.config.StreamFunc(&StreamEvent{
						Content:       contentDelta,
						ReasonContent: reasoningDelta,
						ToolCalls:     ai.NormalizeFunctionToolSlice(normalizedToolCalls),
						FinishReason:  choice.FinishReason,
						Done:          false,
					})
				}
			}
		}
		if !parsedChunk {
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	if !seenDataLine && len(choicesMap) == 0 {
		return nil, 0, fmt.Errorf("sse stream contained no data payload")
	}

	if c.config.StreamFunc != nil {
		c.config.StreamFunc(&StreamEvent{Done: true})
	}

	choices := make([]*ai.ChatChoices, 0, len(choicesMap))
	for i := 0; i < len(choicesMap); i++ {
		if aggregate, ok := choicesMap[i]; ok {
			if aggregate != nil {
				aggregate.finalize()
				ch := aggregate.choice
				if ch.Usage == nil {
					ch.Usage = aggregatedUsage
				} else {
					ch.Usage = normalizeTokenUsage(ch.Usage)
				}
				choices = append(choices, ch)
			}
		}
	}

	if len(choices) == 0 {
		choices = append(choices, &ai.ChatChoices{
			Index: 0,
			Message: &ai.MsgInfo{
				Role:    "assistant",
				Content: "",
			},
			Usage: aggregatedUsage,
		})
	}

	if len(choices) > 0 {
		last := choices[len(choices)-1]
		if last != nil && last.Usage != nil {
			c.lastUsage = last.Usage
		} else {
			c.lastUsage = aggregatedUsage
		}
	} else {
		c.lastUsage = aggregatedUsage
	}

	return choices, 0, nil
}

func isStandardSSEField(line string) bool {
	if strings.HasPrefix(line, ":") {
		return true
	}
	if strings.HasPrefix(line, "event:") {
		return true
	}
	if strings.HasPrefix(line, "id:") {
		return true
	}
	if strings.HasPrefix(line, "retry:") {
		return true
	}
	return false
}

func rawSSEDebugEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("SAST_DEBUG_RAW_SSE")))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type streamDelta struct {
	Role          string             `json:"role,omitempty"`
	Content       string             `json:"content,omitempty"`
	ReasonContent string             `json:"reasoning_content,omitempty"`
	ToolCalls     []*ai.FunctionTool `json:"tool_calls,omitempty"`
}

func (d *streamDelta) UnmarshalJSON(data []byte) error {
	type rawStreamDelta struct {
		Role          string             `json:"role,omitempty"`
		Content       string             `json:"content,omitempty"`
		ReasonContent string             `json:"reasoning_content,omitempty"`
		Reasoning     string             `json:"reasoning,omitempty"`
		ToolCalls     []*ai.FunctionTool `json:"tool_calls,omitempty"`
	}

	var raw rawStreamDelta
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*d = streamDelta{
		Role:          raw.Role,
		Content:       raw.Content,
		ReasonContent: normalizedInboundReasoning(raw.ReasonContent, raw.Reasoning),
		ToolCalls:     raw.ToolCalls,
	}
	return nil
}

func NormalizeToolArgumentDelta(existing string, incoming string) string {
	return normalizeStreamingTextDelta(existing, incoming)
}

func mergeToolCalls(existing []*ai.FunctionTool, incoming []*ai.FunctionTool) ([]*ai.FunctionTool, []*ai.FunctionTool) {
	normalized := make([]*ai.FunctionTool, 0, len(incoming))
	for _, tc := range incoming {
		tc = ai.NormalizeFunctionToolInPlace(tc)
		if tc == nil {
			continue
		}
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}

		for len(existing) <= idx {
			existing = append(existing, &ai.FunctionTool{
				Function: &ai.FunctionDetail{},
			})
		}

		if tc.Id != "" {
			existing[idx].Id = tc.Id
		}
		if tc.Type != "" {
			existing[idx].Type = tc.Type
		}
		existing[idx].Index = tc.Index

		normalizedCall := tc
		if normalizedCall.Function != nil {
			if args, ok := normalizedCall.Function.Arguments.(string); ok {
				existingArgs := ""
				if existing[idx].Function != nil {
					if currentArgs, ok := existing[idx].Function.Arguments.(string); ok {
						existingArgs = currentArgs
					}
				}
				normalizedCall.Function.Arguments = NormalizeToolArgumentDelta(existingArgs, args)
			}
		}
		normalized = append(normalized, normalizedCall)

		if tc.Function != nil {
			if existing[idx].Function == nil {
				existing[idx].Function = &ai.FunctionDetail{}
			}
			if normalizedCall.Function != nil && normalizedCall.Function.Name != "" {
				existing[idx].Function.Name += normalizedCall.Function.Name
			}
			if normalizedCall.Function != nil {
				if args, ok := normalizedCall.Function.Arguments.(string); ok && args != "" {
					if existingArgs, ok := existing[idx].Function.Arguments.(string); ok {
						existing[idx].Function.Arguments = existingArgs + args
					} else {
						existing[idx].Function.Arguments = args
					}
				}
			}
		}
	}
	return existing, normalized
}
