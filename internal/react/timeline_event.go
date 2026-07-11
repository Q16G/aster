package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aster/internal/workspacefs"
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
// 去重后超过 stepTimelineDigestMaxEntries 时截断并追加标记条目——否则截断的 digest 看起来
// 仍然「丰富」，下游「digest 不足才回读 timeline」的判据会漏掉超限部分的调用。
func reduceStepTimelineToolCallsDigest(rt WorkspaceRuntime, stepID string) []string {
	if rt == nil {
		return nil
	}
	rel := rt.Layout().StepTimelineRel(stepID)
	if rel == "" {
		return nil
	}
	data, err := rt.Store().Read(rel)
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
	}
	if len(out) > stepTimelineDigestMaxEntries {
		total := len(out)
		out = out[:stepTimelineDigestMaxEntries]
		out = append(out, fmt.Sprintf("（[截断] 工具调用归约共 %d 条，仅内联前 %d 条；完整事件见 timeline 文件）", total, stepTimelineDigestMaxEntries))
	}
	return out
}

// stepTimelineToolCallCount 统计 step timeline 中 tool_call 事件数；数到 limit 即提前返回，
// 供闸门类只需「执行量是否达阈值」的判定使用。
func stepTimelineToolCallCount(rt WorkspaceRuntime, stepID string, limit int) int {
	if rt == nil || limit <= 0 {
		return 0
	}
	rel := rt.Layout().StepTimelineRel(stepID)
	if rel == "" {
		return 0
	}
	data, err := rt.Store().Read(rel)
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	count := 0
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
		count++
		if count >= limit {
			return count
		}
	}
	return count
}

func appendStepTimeline(rt WorkspaceRuntime, stepID string, event *TimelineEvent) error {
	if rt == nil || event == nil {
		return nil
	}
	rel := rt.Layout().StepTimelineRel(stepID)
	if rel == "" {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// Store.Append 自动建父目录；无 fsync 维持现状（timeline 非崩溃安全流）。
	return rt.Store().Append(rel, data)
}

func stepTimelineRelPath(stepID string) string {
	return workspacefs.Layout{}.StepTimelineRel(stepID)
}

func stepTimelineExists(rt WorkspaceRuntime, stepID string) bool {
	if rt == nil {
		return false
	}
	rel := rt.Layout().StepTimelineRel(stepID)
	if rel == "" {
		return false
	}
	info, err := rt.Store().Stat(rel)
	return err == nil && info.Size() > 0
}
