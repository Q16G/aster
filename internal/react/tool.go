package react

import (
	"aster/internal/builtin_tools"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Tool 工具接口
type Tool interface {
	Name() string
	Description() string
	Parameters() any
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// AgentTool 标记某个 Tool 本质上是一个 Agent（Agent-as-Tool）
// 用于在回调/日志/编排层识别"被调用的是 agent 还是普通工具"
type AgentTool interface {
	Tool
	IsAgent() bool
}

// IsAgentTool 判断工具是否为 Agent 工具
func IsAgentTool(t Tool) bool {
	if t == nil {
		return false
	}
	v, ok := t.(interface{ IsAgent() bool })
	if !ok {
		return false
	}
	return v.IsAgent()
}

func ParseToolArguments(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	switch v := raw.(type) {
	case map[string]any:
		return builtin_tools.CloneAnyMap(v), nil
	case string:
		return ParseToolArgumentsString(v)
	case []byte:
		return ParseToolArgumentsString(string(v))
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("unsupported tool arguments type: %T", raw)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[string]any{}
		}
		return out, nil
	}
}

func ParseToolArgumentsString(raw string) (map[string]any, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return map[string]any{}, nil
	}

	lastErr := fmt.Errorf("parse tool arguments failed")
	for _, candidate := range buildJSONCandidates(s) {
		if out, err := parseJSONStringMap(candidate); err == nil {
			return out, nil
		} else {
			lastErr = err
		}
	}

	if strings.HasPrefix(s, "\"") {
		quoted := ensureClosedQuote(s)
		if unquoted, err := strconv.Unquote(quoted); err == nil {
			for _, candidate := range buildJSONCandidates(unquoted) {
				if out, err := parseJSONStringMap(candidate); err == nil {
					return out, nil
				} else {
					lastErr = err
				}
			}
			fixed := repairJSONText(unquoted)
			if fixed != "" {
				if out, err := parseJSONStringMap(fixed); err == nil {
					return out, nil
				}
				lastErr = err
			}
		}
	}

	fixed := repairJSONText(s)
	if fixed != "" && fixed != s {
		if out, err := parseJSONStringMap(fixed); err == nil {
			return out, nil
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}

func parseJSONStringMap(s string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func ensureClosedQuote(s string) string {
	if s == "" {
		return s
	}
	if isQuoteClosed(s) {
		return s
	}
	return s + "\""
}

func isQuoteClosed(s string) bool {
	if len(s) == 0 || s[0] != '"' {
		return true
	}
	escaped := false
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return true
		}
	}
	return false
}

func repairJSONText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var stack []byte
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		switch ch {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == ch {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if inString {
		s += "\""
	}
	for i := len(stack) - 1; i >= 0; i-- {
		s += string(stack[i])
	}
	return s
}
