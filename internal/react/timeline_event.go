package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TimelineEvent 是 step 执行期间事件日志（shared/<stepID>/timeline.jsonl）的单行记录。
// 工具事件使用一等字段；Result 保留完整文本（超长输出已由截断层落盘并内联指针），
// 是按需回读的无损载体，ResultDigest 仅供规则归约扫描。
type TimelineEvent struct {
	TS   time.Time `json:"ts"`
	Type string    `json:"type"` // tool_call | human_confirm | human_confirm_cancelled | human_confirm_resolved | subagent_event
	Key  string    `json:"key,omitempty"`

	Tool         string `json:"tool,omitempty"`
	ArgsDigest   string `json:"args_digest,omitempty"`
	Result       string `json:"result,omitempty"`
	ResultDigest string `json:"result_digest,omitempty"`
	ResultFile   string `json:"result_file,omitempty"`
	Error        string `json:"error,omitempty"`
	Risk         string `json:"risk,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`

	Payload map[string]any `json:"payload,omitempty"`
}

const timelineDigestMaxRunes = 200

func newToolCallTimelineEvent(callID, toolName string, argsMap map[string]any, out, errText, resultFile string, duration time.Duration) *TimelineEvent {
	risk := ""
	if argsMap != nil {
		if v, ok := argsMap["risk"].(string); ok {
			risk = strings.TrimSpace(v)
		}
	}
	return &TimelineEvent{
		TS:           time.Now().UTC(),
		Type:         "tool_call",
		Key:          callID,
		Tool:         toolName,
		ArgsDigest:   digestToolArgs(argsMap),
		Result:       out,
		ResultDigest: digestText(out, timelineDigestMaxRunes),
		ResultFile:   resultFile,
		Error:        errText,
		Risk:         risk,
		DurationMS:   duration.Milliseconds(),
	}
}

func digestToolArgs(argsMap map[string]any) string {
	if len(argsMap) == 0 {
		return ""
	}
	for _, key := range []string{"path", "file", "pattern", "command", "query", "url"} {
		if v, ok := argsMap[key]; ok {
			return truncateByRunes(fmt.Sprintf("%s=%v", key, v), timelineDigestMaxRunes)
		}
	}
	data, err := json.Marshal(argsMap)
	if err != nil {
		return ""
	}
	return truncateByRunes(string(data), timelineDigestMaxRunes)
}

func digestText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		first := strings.TrimSpace(s[:idx])
		if first != "" {
			s = first
		} else {
			s = strings.Join(strings.Fields(s), " ")
		}
	}
	return truncateByRunes(s, maxRunes)
}

const stepTimelineDigestMaxEntries = 200

// reduceStepTimelineToolCallsDigest 对 step timeline 做规则归约，产出工具调用摘要：
// [工具] args_digest → result_digest（错误时 → error: 摘要）。runtime 归约是 tool_calls_digest
// 的权威来源（模型自报仅作兜底）。旧格式事件（无 Tool 一等字段）跳过。
func reduceStepTimelineToolCallsDigest(sharedDir, stepID string) []string {
	if sharedDir == "" || stepID == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(sharedDir, stepID, "timeline.jsonl"))
	if err != nil {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	var out []string
	seen := make(map[string]struct{})
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev TimelineEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type != "tool_call" || strings.TrimSpace(ev.Tool) == "" {
			continue
		}
		result := ev.ResultDigest
		if strings.TrimSpace(ev.Error) != "" {
			result = "error: " + digestText(ev.Error, 120)
		}
		entry := fmt.Sprintf("[%s] %s → %s", ev.Tool, ev.ArgsDigest, result)
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
		if len(out) >= stepTimelineDigestMaxEntries {
			break
		}
	}
	return out
}

func appendStepTimeline(sharedDir, stepID string, event *TimelineEvent) error {
	if sharedDir == "" || stepID == "" || event == nil {
		return nil
	}
	dir := filepath.Join(sharedDir, stepID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(filepath.Join(dir, "timeline.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func stepTimelineRelPath(stepID string) string {
	return fmt.Sprintf("shared/%s/timeline.jsonl", stepID)
}

func stepTimelineExists(sharedDir, stepID string) bool {
	if sharedDir == "" || stepID == "" {
		return false
	}
	p := filepath.Join(sharedDir, stepID, "timeline.jsonl")
	info, err := os.Stat(p)
	return err == nil && info.Size() > 0
}
