package tui

import (
	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react"
)

type AgentEventMsg struct {
	Event *react.AgentOutputEvent
}

type AgentDoneMsg struct {
	Result  *builtin_tools.RunResult
	RunID   string
	History []*ai.MsgInfo
	Err     error
}

type UserSubmitMsg struct {
	Text string
}

type HumanRequestMsg struct {
	RequestID string
	Question  string
	InputType string   // "text" / "single_choice" / "multi_choice" / "structured"
	Options   []string // choices for single_choice / multi_choice
	Context   map[string]any
}

type StatusTextMsg struct {
	Text string
}

type AgentSwitchMsg struct {
	Definition react.AgentDefinition
}

type SlashCommandMsg struct {
	Command string
}

type SessionSwitchMsg struct {
	SessionID string
}

type SessionCreateMsg struct{}

type SkillToggleMsg struct {
	Name    string
	Enabled bool
}

type MCPToggleMsg struct {
	Name    string
	Connect bool
}

type RefreshSidebarMsg struct{}

type ThemeToggleMsg struct{}

type ProviderSwitchMsg struct {
	Name string
}

type ModelSwitchMsg struct {
	ModelID string
}

type ModelPickerLoadedMsg struct {
	Models []ModelOption
}

type ModelPickerFailedMsg struct {
	Err error
}

type QuitConfirmMsg struct{}

type BatchedEventsMsg struct {
	Events []TuiEvent
}

type CommandPickerRequestMsg struct{}

type FilePickerRequestMsg struct{}
