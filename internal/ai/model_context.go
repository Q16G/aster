package ai

import "strings"

type ModelContextInfo struct {
	ModelName           string
	ContextWindowTokens int
	InputTokenLimit     int
	OutputTokenLimit    int
}

func (m ModelContextInfo) Normalize() ModelContextInfo {
	m.ModelName = strings.TrimSpace(m.ModelName)
	if m.ContextWindowTokens < 0 {
		m.ContextWindowTokens = 0
	}
	if m.InputTokenLimit < 0 {
		m.InputTokenLimit = 0
	}
	if m.OutputTokenLimit < 0 {
		m.OutputTokenLimit = 0
	}
	return m
}

type ModelContextProvider interface {
	ModelContextInfo() ModelContextInfo
}
