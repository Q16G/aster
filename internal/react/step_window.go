package react

type StepWindow struct {
	StepID string `json:"step_id,omitempty"`

	// RawTimelineDiff: TimelineMemoryDiffer 的 raw diff（可能较长）。
	// 仅用于审计/调试与 reducer 输入；不要把它当作长期知识单元直接消费。
	RawTimelineDiff string `json:"raw_timeline_diff,omitempty"`

	RawApproxTokens int `json:"raw_approx_tokens,omitempty"`
	BudgetTokens    int `json:"budget_tokens,omitempty"`
	ExcerptTokens   int `json:"excerpt_tokens,omitempty"`

	Reduced bool                     `json:"reduced,omitempty"`
	Reducer *StepWindowReducerOutput `json:"reducer,omitempty"`
}

type StepWindowReducerOutput struct {
	StatusSummary    string   `json:"status_summary"`
	WindowSummary    string   `json:"window_summary"`
	NewFacts         []string `json:"new_facts"`
	ImportantChanges []string `json:"important_changes"`
	References       []string `json:"references"`
	ArtifactChanges  []string `json:"artifact_changes"`
	OpenQuestions    []string `json:"open_questions"`
	NoiseRemoved     []string `json:"noise_removed"`
}
