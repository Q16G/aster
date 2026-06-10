package builtin_tools

import (
	"bytes"
	"encoding/json"
	"strings"
)

// AxisItem 是三轴（补齐①/深挖②/扩面③）的结构化条目。
// evidence 承载 evidence-grounded 泛化判据（触发该条目的观测事实）；ledger_id 是与
// open_items 账本的对账键（OI-id）；produces/consumes 支撑 planner 的产物-消费依赖排序。
type AxisItem struct {
	Item      string   `json:"item"`
	Dimension string   `json:"dimension,omitempty"`
	Evidence  string   `json:"evidence,omitempty"`
	LedgerID  string   `json:"ledger_id,omitempty"`
	Produces  string   `json:"produces,omitempty"`
	Consumes  []string `json:"consumes,omitempty"`
}

// UnmarshalJSON 兼容字符串形态（旧 prompt 输出 / 旧持久化）："x" 解析为 {item: "x"}。
func (a *AxisItem) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = AxisItem{Item: s}
		return nil
	}
	type axisItemAlias AxisItem
	var v axisItemAlias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*a = AxisItem(v)
	return nil
}

// NewAxisItems 把字符串条目升格为结构化条目（旧模型输出边界用）。
func NewAxisItems(items []string) []*AxisItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]*AxisItem, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, &AxisItem{Item: item})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AxisItemStrings 投影为人类可读字符串（item + 证据附注），供旧 prompt 注入、计数与展示。
func AxisItemStrings(items []*AxisItem) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.Item) == "" {
			continue
		}
		text := strings.TrimSpace(item.Item)
		if ev := strings.TrimSpace(item.Evidence); ev != "" {
			text += "（证据: " + ev + "）"
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func CloneAxisItems(in []*AxisItem) []*AxisItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]*AxisItem, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		clone := *item
		clone.Consumes = CloneStringSlice(item.Consumes)
		out = append(out, &clone)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeAxisItems 去空、按 item 文本去重，保持顺序。
func NormalizeAxisItems(in []*AxisItem) []*AxisItem {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]*AxisItem, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		text := strings.TrimSpace(item.Item)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		clone := *item
		clone.Item = text
		clone.Consumes = CloneStringSlice(item.Consumes)
		out = append(out, &clone)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MaintenanceDirective 是 step_replan 输出的落盘维护指令，由 runtime 维护执行器
// 在进入下一节点之前机械执行（append 类操作）；单条失败记 warning 注入下游，不阻塞。
type MaintenanceDirective struct {
	Type     string `json:"type"` // archive_item | ledger_add | ledger_update | merge_staging | context_bake
	Target   string `json:"target,omitempty"`
	Content  string `json:"content,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

const (
	MaintenanceDirectiveArchiveItem  = "archive_item"
	MaintenanceDirectiveLedgerAdd    = "ledger_add"
	MaintenanceDirectiveLedgerUpdate = "ledger_update"
	MaintenanceDirectiveMergeStaging = "merge_staging"
	MaintenanceDirectiveContextBake  = "context_bake"
)
