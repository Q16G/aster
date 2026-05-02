package builtin_tools

import (
	"encoding/json"
	"strings"
)

const defaultTruncatedMessage = "工具输出已截断，后续内容以 ... 省略。"

func NormalizeToolStructuredOutput(toolName string, output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return output
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return output
	}

	NormalizeToolTruncatedPayload(toolName, payload)

	out, err := json.Marshal(payload)
	if err != nil {
		return output
	}
	return string(out)
}

func NormalizeToolTruncatedPayload(toolName string, payload map[string]any) {
	if len(payload) == 0 {
		return
	}
	canonicalTool := normalizeToolName(toolName)
	if !payloadIsTruncated(canonicalTool, payload) {
		return
	}

	payload["truncated"] = true

	if content, ok := payload["content"].(string); ok {
		payload["content"] = AppendTruncationMarker(content)
	}

	if msg, _ := payload["message"].(string); strings.TrimSpace(msg) == "" {
		payload["message"] = buildTruncationMessage(canonicalTool, payload)
	}
}

func payloadIsTruncated(toolName string, payload map[string]any) bool {
	if b, ok := payload["truncated"].(bool); ok && b {
		return true
	}
	return false
}

func normalizeToolName(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return ""
	}
	return strings.TrimSuffix(name, "-error")
}

func buildTruncationMessage(toolName string, payload map[string]any) string {
	switch toolName {
	case ReadFileToolName:
		if large, _ := payload["large_file"].(bool); large {
			threshold := readFileLargeThreshold
			if n, ok := asInt64Any(payload["threshold_bytes"]); ok && n > 0 {
				threshold = n
			}
			preview := int64(0)
			if n, ok := asInt64Any(payload["preview_bytes"]); ok && n >= 0 {
				preview = n
			}
			omitted := int64(0)
			if n, ok := asInt64Any(payload["omitted_bytes"]); ok && n >= 0 {
				omitted = n
			}
			return ReadFileLargeTruncationMessage(threshold, int(preview), omitted)
		}
		maxBytes := int64(0)
		if n, ok := asInt64Any(payload["max_bytes"]); ok && n > 0 {
			maxBytes = n
		}
		return ReadFileTruncationMessage(maxBytes)
	case RgToolName:
		captureLimitBytes := int64(0)
		if n, ok := asInt64Any(payload["capture_limit_bytes"]); ok && n > 0 {
			captureLimitBytes = n
		}
		return RgTruncationMessage(captureLimitBytes)
	case ListFilesToolName:
		maxOutputBytes := int64(0)
		if n, ok := asInt64Any(payload["max_output_bytes"]); ok && n > 0 {
			maxOutputBytes = n
		}
		return ListFilesTruncationMessage(maxOutputBytes)
	default:
		return defaultTruncatedMessage
	}
}
