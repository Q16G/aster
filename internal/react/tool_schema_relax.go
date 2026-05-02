package react

import "strings"

func relaxToolParametersSchema(raw any) any {
	root, ok := relaxSchemaNode(raw, true).(map[string]any)
	if !ok || root == nil {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": true,
		}
	}
	if strings.TrimSpace(anyText(root["type"])) == "" {
		root["type"] = "object"
	}
	root["additionalProperties"] = true
	return root
}

func relaxSchemaNode(raw any, topLevel bool) any {
	switch typed := raw.(type) {
	case map[string]any:
		out := make(map[string]any)
		if desc := strings.TrimSpace(anyText(typed["description"])); desc != "" {
			out["description"] = desc
		}
		if title := strings.TrimSpace(anyText(typed["title"])); title != "" {
			out["title"] = title
		}
		if topLevel {
			out["type"] = "object"
		} else if typ := strings.TrimSpace(anyText(typed["type"])); typ != "" {
			// 保留属性类型信息，否则模型很容易把 array/integer 当成 string 传入。
			out["type"] = typ
		}
		if properties, ok := typed["properties"].(map[string]any); ok && len(properties) > 0 {
			next := make(map[string]any, len(properties))
			for key, value := range properties {
				if child, ok := relaxSchemaNode(value, false).(map[string]any); ok && child != nil {
					next[key] = child
				} else {
					next[key] = map[string]any{}
				}
			}
			out["properties"] = next
			out["additionalProperties"] = true
		}
		if items, ok := typed["items"]; ok {
			out["items"] = relaxSchemaNode(items, false)
		}
		// 顶层 required 对工具调用质量影响很大，保留；但不强制 additionalProperties=false，以兼容运行时注入字段。
		if topLevel {
			if required, ok := typed["required"]; ok {
				out["required"] = required
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, relaxSchemaNode(item, false))
		}
		return out
	default:
		return raw
	}
}

func anyText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
