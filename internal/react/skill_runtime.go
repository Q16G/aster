package react

import (
	"encoding/json"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

type loadSkillsToolResult struct {
	Skills []struct {
		Name string `json:"name"`
	} `json:"skills"`
}

type deleteSkillToolResult struct {
	Name string `json:"name"`
}

func (a *Agent) handleSkillToolStateSync(toolName string, args map[string]any, out string, errText string) {
	if a == nil || strings.TrimSpace(errText) != "" {
		return
	}

	var snapshot builtin_tools.StateSnapshot
	switch strings.TrimSpace(toolName) {
	case builtin_tools.LoadSkillsToolName, builtin_tools.SkillToolName:
		names := parseLoadedSkillNames(out)
		if len(names) == 0 {
			return
		}
		snapshot = a.state.AddActiveSkillNames(names)
	case builtin_tools.DeleteSkillToolName:
		name := parseDeletedSkillName(args, out)
		if name == "" {
			return
		}
		snapshot = a.state.RemoveActiveSkillNames([]string{name})
	default:
		return
	}

	a.persistActiveSkillNames(snapshot.ActiveSkillNames)
}

func parseLoadedSkillNames(out string) []string {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var payload loadSkillsToolResult
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return nil
	}
	names := make([]string, 0, len(payload.Skills))
	for _, item := range payload.Skills {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return normalizeSkillNames(names)
}

func parseDeletedSkillName(args map[string]any, out string) string {
	if args != nil {
		if raw, ok := args["name"].(string); ok {
			if name := strings.TrimSpace(raw); name != "" {
				return name
			}
		}
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	var payload deleteSkillToolResult
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Name)
}

func (a *Agent) persistActiveSkillNames(names []string) {
	if a == nil || a.workspaceRuntime == nil {
		return
	}

	state, err := a.workspaceRuntime.LoadWorkspaceState()
	if err != nil || state == nil {
		return
	}

	state.SessionID = firstNonEmpty(strings.TrimSpace(state.SessionID), strings.TrimSpace(a.workspaceSessionID))
	state.ActiveSkillNames = builtin_tools.CloneStringSlice(normalizeSkillNames(names))
	state.UpdatedAt = time.Now()
	_ = a.workspaceRuntime.SaveWorkspaceState(state)
}
