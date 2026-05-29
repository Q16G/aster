package tui

import "strings"

type TurnType string

const (
	TurnTypeUser      TurnType = "user"
	TurnTypeAssistant TurnType = "assistant"
)

type IndexedPart struct {
	Index int
	Part  DisplayPart
}

type Turn struct {
	Type  TurnType
	Parts []IndexedPart
}

func groupPartsIntoTurns(parts []DisplayPart) []Turn {
	if len(parts) == 0 {
		return nil
	}

	var turns []Turn
	var assistantParts []IndexedPart

	flushAssistant := func() {
		if len(assistantParts) > 0 {
			turns = append(turns, Turn{Type: TurnTypeAssistant, Parts: assistantParts})
			assistantParts = nil
		}
	}

	for i, part := range parts {
		if part.Type == PartTypeUser {
			flushAssistant()
			turns = append(turns, Turn{
				Type:  TurnTypeUser,
				Parts: []IndexedPart{{Index: i, Part: part}},
			})
		} else {
			assistantParts = append(assistantParts, IndexedPart{Index: i, Part: part})
		}
	}
	flushAssistant()
	return turns
}

func mergeTextRun(parts []IndexedPart, localIdx int) (string, int) {
	var sb strings.Builder
	count := 0
	var agentName string
	for i := localIdx; i < len(parts); i++ {
		if parts[i].Part.Type != PartTypeText || parts[i].Part.Text == nil {
			break
		}
		// Only merge a contiguous run that belongs to the same agent, so
		// concurrent sub-agents' streamed text stays visually separated.
		if count == 0 {
			agentName = parts[i].Part.Text.AgentName
		} else if parts[i].Part.Text.AgentName != agentName {
			break
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(parts[i].Part.Text.Content)
		count++
	}
	return sb.String(), count
}
