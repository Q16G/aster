package ui

import (
	"sort"
	"strings"
	"unicode"
)

type fuzzyResult struct {
	index int
	score int
}

func FuzzyMatch(pattern, text string) (bool, int) {
	pattern = strings.ToLower(pattern)
	text = strings.ToLower(text)
	if pattern == "" {
		return true, 0
	}
	pi := 0
	score := 0
	lastMatch := -1
	runes := []rune(text)

	patRunes := []rune(pattern)
	consecutive := 0
	for ti, r := range runes {
		if pi < len(patRunes) && r == patRunes[pi] {
			score += 1
			if lastMatch >= 0 && ti == lastMatch+1 {
				consecutive++
				score += consecutive * 4
			} else {
				consecutive = 0
			}
			if ti == 0 || !unicode.IsLetter(runes[ti-1]) && !unicode.IsDigit(runes[ti-1]) {
				score += 2
			}
			lastMatch = ti
			pi++
		}
	}

	if pi < len([]rune(pattern)) {
		return false, 0
	}
	return true, score
}

func FuzzyFilter(pattern string, options []SelectOption) []SelectOption {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return options
	}

	var results []fuzzyResult
	for i, opt := range options {
		if opt.Disabled {
			continue
		}
		matched, score := FuzzyMatch(pattern, opt.Label)
		if !matched {
			matched, score = FuzzyMatch(pattern, opt.Value)
		}
		if !matched && opt.Description != "" {
			matched, score = FuzzyMatch(pattern, opt.Description)
		}
		if matched {
			results = append(results, fuzzyResult{index: i, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	out := make([]SelectOption, 0, len(results))
	for _, r := range results {
		out = append(out, options[r.index])
	}
	return out
}
