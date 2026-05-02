package react

import (
	"strings"
	"unicode/utf8"

	"aster/internal/builtin_tools"
)

const (
	timelineDiffMaxBytes = 10 * 1024
	timelineDiffItemMax  = 260
)

func (a *Agent) buildTimelineDiffForStep(outcome *builtin_tools.StepOutcome, references []string, artifactDir, summaryFile, resultFile string) string {
	if outcome == nil {
		return ""
	}

	newFacts := make([]string, 0, 8)
	addFact := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		newFacts = append(newFacts, truncateForTimelineDiff(label+": "+value, timelineDiffItemMax))
	}
	addFact("summary", outcome.Summary)
	addFact("display_result", outcome.DisplayResult)
	addFact("error", outcome.Error)
	if strings.TrimSpace(outcome.Result) != "" {
		newFacts = append(newFacts, truncateForTimelineDiff("result: "+outcome.Result, timelineDiffItemMax))
	}
	if len(newFacts) > 8 {
		newFacts = newFacts[:8]
	}

	artifactChanges := make([]string, 0, 8)
	for _, p := range []string{artifactDir, summaryFile, resultFile} {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		artifactChanges = append(artifactChanges, truncateForTimelineDiff(p, timelineDiffItemMax))
		if len(artifactChanges) >= 8 {
			break
		}
	}

	refChanges := make([]string, 0, 8)
	for _, ref := range references {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		refChanges = append(refChanges, truncateForTimelineDiff(ref, timelineDiffItemMax))
		if len(refChanges) >= 8 {
			break
		}
	}

	var buf strings.Builder
	buf.WriteString("New Facts:\n")
	writeBullets(&buf, newFacts)
	buf.WriteString("\nArtifact Changes:\n")
	writeBullets(&buf, artifactChanges)
	buf.WriteString("\nReference Changes:\n")
	writeBullets(&buf, refChanges)

	out := strings.TrimSpace(buf.String())
	if len(out) <= timelineDiffMaxBytes {
		return out
	}

	// 超长时优先截断尾部（保留最近部分通常更关键）
	out = out[:timelineDiffMaxBytes]
	out = strings.TrimRight(out, "\n\r\t ")
	return out
}

func writeBullets(buf *strings.Builder, items []string) {
	if buf == nil {
		return
	}
	if len(items) == 0 {
		buf.WriteString("- (none)\n")
		return
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		buf.WriteString("- ")
		buf.WriteString(item)
		buf.WriteString("\n")
	}
}

func truncateForTimelineDiff(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n", " | ")
	text = strings.TrimSpace(text)
	if maxChars <= 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return strings.TrimSpace(string(runes[:maxChars])) + "..."
}
