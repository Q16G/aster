package argx

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func Text(value any) string {
	if isTypedNil(value) {
		return ""
	}
	switch v := value.(type) {
	case string:
		return normalizeText(v)
	case []byte:
		return normalizeText(string(v))
	case fmt.Stringer:
		return normalizeText(v.String())
	default:
		if value == nil {
			return ""
		}
		return normalizeText(fmt.Sprint(value))
	}
}

func IsTypedNil(value any) bool {
	return isTypedNil(value)
}

func OptionalText(args map[string]any, key string) string {
	if args == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	return Text(args[key])
}

func RequiredText(args map[string]any, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("key is required")
	}
	value := OptionalText(args, key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func StringSlice(value any) []string {
	if isTypedNil(value) {
		return nil
	}

	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := Text(item)
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := Text(item)
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		s := Text(v)
		if s == "" {
			return nil
		}
		// 兼容前端/Agent 把数组序列化成 JSON 字符串的情况，例如 ["java","go"]。
		// 仅在看起来像 JSON array 时尝试解析，失败则按单元素字符串处理。
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			var list []any
			if err := json.Unmarshal([]byte(s), &list); err == nil {
				out := make([]string, 0, len(list))
				for _, item := range list {
					itemText := Text(item)
					if itemText != "" {
						out = append(out, itemText)
					}
				}
				if len(out) == 0 {
					return nil
				}
				return out
			}
		}
		return []string{s}
	default:
		s := Text(v)
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

func RequiredArray(args map[string]any, key string) (any, error) {
	value, ok := readArg(args, key)
	if !ok || isTypedNil(value) {
		return nil, fmt.Errorf("%s is required", key)
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return nil, fmt.Errorf("%s must not be empty", key)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("%s must be an array", key)
	}
}

func OptionalArray(args map[string]any, key string) (any, bool, error) {
	value, ok := readArg(args, key)
	if !ok || isTypedNil(value) {
		return nil, false, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return nil, false, nil
		}
		return value, true, nil
	default:
		return nil, false, fmt.Errorf("%s must be an array", key)
	}
}

func RequiredObject(args map[string]any, key string) (any, error) {
	value, ok := readArg(args, key)
	if !ok || isTypedNil(value) {
		return nil, fmt.Errorf("%s is required", key)
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map, reflect.Struct:
		return value, nil
	default:
		return nil, fmt.Errorf("%s must be an object", key)
	}
}

func OptionalObject(args map[string]any, key string) (any, bool, error) {
	value, ok := readArg(args, key)
	if !ok || isTypedNil(value) {
		return nil, false, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map, reflect.Struct:
		return value, true, nil
	default:
		return nil, false, fmt.Errorf("%s must be an object", key)
	}
}

func normalizeText(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	return trimmed
}

func readArg(args map[string]any, key string) (any, bool) {
	if args == nil || strings.TrimSpace(key) == "" {
		return nil, false
	}
	value, ok := args[key]
	return value, ok
}

func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
