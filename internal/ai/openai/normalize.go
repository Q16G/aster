package openai

import (
	"strings"

	"aster/internal/ai"
	"aster/internal/jsonextractor"
)

const (
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

func extractJSONCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	stdjsons, rawCandidates := jsonextractor.ExtractJSONWithRaw(raw)
	candidates := make([]string, 0, len(stdjsons)+len(rawCandidates)+1)
	seen := make(map[string]struct{})
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, candidate := range stdjsons {
		add(candidate)
	}
	for _, candidate := range rawCandidates {
		add(candidate)
		add(string(jsonextractor.FixJson([]byte(candidate))))
	}
	if len(candidates) == 0 {
		add(raw)
	}
	return candidates
}

type thinkStreamState struct {
	inThink bool
	pending string
}

func (s *thinkStreamState) consume(input string) (string, string) {
	data := s.pending + input
	s.pending = ""
	var content strings.Builder
	var reasoning strings.Builder

	for i := 0; i < len(data); {
		if data[i] != '<' {
			if s.inThink {
				reasoning.WriteByte(data[i])
			} else {
				content.WriteByte(data[i])
			}
			i++
			continue
		}

		fragment := data[i:]
		switch {
		case strings.HasPrefix(fragment, thinkOpenTag):
			s.inThink = true
			i += len(thinkOpenTag)
			continue
		case strings.HasPrefix(fragment, thinkCloseTag):
			s.inThink = false
			i += len(thinkCloseTag)
			continue
		case isPartialTagPrefix(fragment, thinkOpenTag), isPartialTagPrefix(fragment, thinkCloseTag):
			s.pending = fragment
			return content.String(), reasoning.String()
		default:
			if s.inThink {
				reasoning.WriteByte(data[i])
			} else {
				content.WriteByte(data[i])
			}
			i++
		}
	}
	return content.String(), reasoning.String()
}

func (s *thinkStreamState) flush() (string, string) {
	if s.pending == "" {
		return "", ""
	}
	pending := s.pending
	s.pending = ""
	if s.inThink {
		return "", pending
	}
	return pending, ""
}

func isPartialTagPrefix(fragment string, tag string) bool {
	return len(fragment) < len(tag) && strings.HasPrefix(tag, fragment)
}

func appendAssistantContent(msg *ai.MsgInfo, delta string) {
	if msg == nil || delta == "" {
		return
	}
	if current, ok := msg.Content.(string); ok {
		msg.Content = current + delta
		return
	}
	msg.Content = delta
}

func normalizeStreamingTextDelta(existing string, incoming string) string {
	if incoming == "" {
		return ""
	}
	if existing == "" {
		return incoming
	}
	if strings.HasPrefix(incoming, existing) {
		return incoming[len(existing):]
	}
	return incoming
}

func normalizeAssistantMessage(msg *ai.MsgInfo) {
	if msg == nil {
		return
	}
	content, ok := msg.Content.(string)
	if !ok || content == "" {
		return
	}

	parser := &thinkStreamState{}
	cleanContent, taggedReasoning := parser.consume(content)
	contentTail, reasoningTail := parser.flush()
	cleanContent += contentTail
	taggedReasoning += reasoningTail

	msg.Content = cleanContent
	if strings.TrimSpace(msg.ReasoningOutput) == "" && taggedReasoning != "" {
		msg.ReasoningOutput = taggedReasoning
	}
}

type streamChoiceAggregate struct {
	choice                 *ai.ChatChoices
	think                  thinkStreamState
	explicitReasoning      bool
	explicitReasoningDelta string
	taggedReasoningDelta   string
}

func newStreamChoiceAggregate(index int) *streamChoiceAggregate {
	return &streamChoiceAggregate{
		choice: &ai.ChatChoices{
			Index: index,
			Message: &ai.MsgInfo{
				Role:      "assistant",
				ToolCalls: make([]*ai.FunctionTool, 0),
			},
		},
	}
}

func (a *streamChoiceAggregate) applyDelta(delta streamDelta) (string, string) {
	if a == nil || a.choice == nil || a.choice.Message == nil {
		return "", ""
	}
	if delta.Role != "" {
		a.choice.Message.Role = delta.Role
	}

	cleanContent, taggedReasoning := a.think.consume(delta.Content)
	appendAssistantContent(a.choice.Message, cleanContent)

	var emittedReasoning string
	if delta.ReasonContent != "" {
		a.explicitReasoning = true
		normalizedReasoning := normalizeStreamingTextDelta(a.explicitReasoningDelta, delta.ReasonContent)
		a.explicitReasoningDelta += normalizedReasoning
		emittedReasoning = normalizedReasoning
	} else if !a.explicitReasoning && taggedReasoning != "" {
		a.taggedReasoningDelta += taggedReasoning
		emittedReasoning = taggedReasoning
	}

	if a.explicitReasoning {
		a.choice.Message.ReasoningOutput = a.explicitReasoningDelta
	} else {
		a.choice.Message.ReasoningOutput = a.taggedReasoningDelta
	}

	return cleanContent, emittedReasoning
}

func (a *streamChoiceAggregate) finalize() {
	if a == nil || a.choice == nil || a.choice.Message == nil {
		return
	}
	contentTail, reasoningTail := a.think.flush()
	appendAssistantContent(a.choice.Message, contentTail)
	if !a.explicitReasoning && reasoningTail != "" {
		a.taggedReasoningDelta += reasoningTail
		a.choice.Message.ReasoningOutput = a.taggedReasoningDelta
	}
}
