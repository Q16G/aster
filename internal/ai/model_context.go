package ai

import "strings"

type ModelContextInfo struct {
	ModelName           string
	ContextWindowTokens int
	InputTokenLimit     int
	OutputTokenLimit    int
	SupportsVision      *bool
	SupportsAudio       *bool
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

func BoolPtr(v bool) *bool { return &v }

func (m ModelContextInfo) GetSupportsVision() bool {
	return m.SupportsVision != nil && *m.SupportsVision
}

func (m ModelContextInfo) GetSupportsAudio() bool {
	return m.SupportsAudio != nil && *m.SupportsAudio
}

type ModelContextProvider interface {
	ModelContextInfo() ModelContextInfo
}
