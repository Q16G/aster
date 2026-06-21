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

type ejectSkillToolResult struct {
	Name string `json:"name"`
}

// handleSkillToolStateSync 在 skill 工具（load_skill / eject_skill）执行成功后同步
// agent 的 active skill 集合。
//
// runCtx 路由（方案 B per-peer 隔离）：
//   - runCtx != nil 且 runCtx.LocalActiveSkillNames != nil（peer 路径）→ 改 overlay 不动全局
//   - 其他情况（主路径 / 旧测试调用）→ 走全局 a.state.AddActiveSkillNames / Remove，并持久化
//
// peer 路径**不持久化** —— peer 内加载的 skill 仅在本 peer 跑的过程中生效，peer
// 退出即丢弃。详见 InlineStepCtx.LocalActiveSkillNames doc。
func (a *Agent) handleSkillToolStateSync(toolName string, args map[string]any, out string, errText string, runCtx *InlineStepCtx) {
	if a == nil || strings.TrimSpace(errText) != "" {
		return
	}

	if runCtx != nil && runCtx.LocalActiveSkillNames != nil {
		applySkillToolToOverlay(runCtx.LocalActiveSkillNames, toolName, args, out)
		return
	}

	var snapshot builtin_tools.StateSnapshot
	switch strings.TrimSpace(toolName) {
	case builtin_tools.SkillToolName:
		names := parseLoadedSkillNames(out)
		if len(names) == 0 {
			return
		}
		snapshot = a.state.AddActiveSkillNames(names)
	case builtin_tools.EjectSkillToolName:
		name := parseEjectedSkillName(args, out)
		if name == "" {
			return
		}
		snapshot = a.state.RemoveActiveSkillNames([]string{name})
	default:
		return
	}

	a.persistActiveSkillNames(snapshot.ActiveSkillNames)
}

// applySkillToolToOverlay 在 peer 的 LocalActiveSkillNames overlay 上执行 load/eject。
// 不动全局 state，不持久化——peer 退出后 overlay 自然被 GC 清理。
func applySkillToolToOverlay(overlay *[]string, toolName string, args map[string]any, out string) {
	if overlay == nil {
		return
	}
	switch strings.TrimSpace(toolName) {
	case builtin_tools.SkillToolName:
		names := parseLoadedSkillNames(out)
		if len(names) == 0 {
			return
		}
		merged := append([]string(nil), *overlay...)
		merged = append(merged, names...)
		*overlay = normalizeSkillNames(merged)
	case builtin_tools.EjectSkillToolName:
		name := parseEjectedSkillName(args, out)
		if name == "" {
			return
		}
		next := make([]string, 0, len(*overlay))
		for _, n := range *overlay {
			if strings.TrimSpace(n) == name {
				continue
			}
			next = append(next, n)
		}
		*overlay = next
	}
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

func parseEjectedSkillName(args map[string]any, out string) string {
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
	var payload ejectSkillToolResult
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
