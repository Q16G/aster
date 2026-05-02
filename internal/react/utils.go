package react

import (
	"aster/internal/jsonextractor"
	"crypto/rand"
	"regexp"
	"strings"
	"time"
)

// ==================== 随机字符串生成 ====================

// generateRandomString 生成指定长度的随机字符串
func generateRandomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = letters[b[i]%byte(len(letters))]
	}
	return string(b)
}

func generateAgentRunID() string {
	// 例：run-20260403-150405-abc123
	return "run-" + time.Now().UTC().Format("20060102-150405") + "-" + generateRandomString(6)
}

func buildJSONCandidates(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	stdjsons, rawCandidates := jsonextractor.ExtractJSONWithRaw(content)
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
		add(content)
	}
	return candidates
}

var continuationInputPattern = regexp.MustCompile(`(?i)^\s*(继续(一下|吧)?|请继续|接着来?|接着|继续上次|继续刚才|continue|resume|go\s+on|carry\s+on|keep\s+going)\b`)

func isContinuationInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(trimmed, "继续") || strings.HasPrefix(trimmed, "接着") {
		return true
	}
	switch {
	case strings.HasPrefix(lower, "continue"),
		strings.HasPrefix(lower, "resume"),
		strings.HasPrefix(lower, "go on"),
		strings.HasPrefix(lower, "carry on"),
		strings.HasPrefix(lower, "keep going"):
		return true
	default:
	}
	return continuationInputPattern.MatchString(trimmed)
}
