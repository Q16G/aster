package tui

type SidebarSnapshot struct {
	SessionTitle string
	AgentName    string
	ProviderName string
	ModelID      string

	TokenCount   string
	CostEstimate string
	RunStatus    string

	InputTokens      string
	OutputTokens     string
	ReasoningTokens  string
	CacheReadTokens  string
	CacheWriteTokens string

	MCPServers []MCPStatusEntry

	PlanItems []PlanItemView

	ModifiedFiles []string

	ActiveSkills []string
	ActiveMCPs   []string

	HasProvider             bool
	DismissedGettingStarted bool

	Workdir         string
	Version         string
	UpdateAvailable string
}

type MCPStatusEntry struct {
	Name      string
	Status    string
	ToolCount int
	Active    bool
}
