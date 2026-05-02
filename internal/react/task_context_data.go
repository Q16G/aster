package react

import "strings"

// TaskContextData holds caller-provided execution context injected into the ThinkAct prompt.
// It uses a generic ordered KV structure so the platform layer carries no domain-specific fields.
// Domain concepts (e.g. "项目路径", "编译状态") are expressed by callers as entries, not by
// the platform as struct fields.
type TaskContextData struct {
	Entries []TaskContextEntry
}

// TaskContextEntry is a single labeled context value rendered into the prompt.
type TaskContextEntry struct {
	Label       string // display key, e.g. "项目路径"
	Value       string // the value, e.g. "/repo/project"
	Description string // optional explanation of what this entry means and how the agent should use it
}

func (d *TaskContextData) HasVisibleData() bool {
	if d == nil {
		return false
	}
	for _, e := range d.Entries {
		if strings.TrimSpace(e.Label) != "" && strings.TrimSpace(e.Value) != "" {
			return true
		}
	}
	return false
}

// VisibleEntries returns entries with non-empty label and value.
func (d *TaskContextData) VisibleEntries() []TaskContextEntry {
	if d == nil {
		return nil
	}
	out := make([]TaskContextEntry, 0, len(d.Entries))
	for _, e := range d.Entries {
		if strings.TrimSpace(e.Label) != "" && strings.TrimSpace(e.Value) != "" {
			out = append(out, e)
		}
	}
	return out
}
