package memory

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"aster/internal/ai"
	"aster/internal/runtimelog"

	"aster/internal/utils"
)

//go:embed prompts/timeline_memory_prompt.prompt
var memoryPrompt string

const (
	defaultTimelineTriggerBytes  = 70 * 1024
	defaultTimelineKeepLastItems = 30
	timelineCompressMaxLoops     = 20
	defaultAIOutputMaxRetries    = 3
)

// TimelineMemory AI记忆层，旨在AI-REACT执行的过程中，记录AI的一些基础记忆信息
// 包含了工具调用和环境调用结果、感知层结果、AI思考过程、用户输入、中间推理、反思结
// 果等一些内容，是会注入到prompt中作后续的决策去使用, 是运行时记忆
type TimelineMemory struct {
	extraInfo func() string
	ctxMu     sync.Mutex
	ctx       context.Context
	aiClient  ai.ChatClient
	items     *utils.OrderMapx[string, *TimelineItem]
	summary   string
	trigger   int
	keepLast  int

	memoryTemplate *template.Template
}

// TimelineOption 时间线记忆配置选项
type TimelineOption func(*TimelineMemory)

// WithTriggerBytes 设置触发压缩的字节阈值
func WithTriggerBytes(bytes int) TimelineOption {
	return func(m *TimelineMemory) {
		if m != nil {
			m.trigger = bytes
		}
	}
}

// WithKeepLastItems 设置保留最近N条记录
func WithKeepLastItems(n int) TimelineOption {
	return func(m *TimelineMemory) {
		if m != nil {
			m.keepLast = n
		}
	}
}

// NewTimeLine 创建时间线记忆
func NewTimeLine(ctx context.Context, client ai.ChatClient, extraInfo func() string, opts ...TimelineOption) *TimelineMemory {
	if extraInfo == nil {
		extraInfo = func() string { return "" }
	}
	m := &TimelineMemory{
		extraInfo:      extraInfo,
		ctx:            ctx,
		aiClient:       client,
		items:          utils.NewOrderMapx[string, *TimelineItem](),
		trigger:        defaultTimelineTriggerBytes,
		keepLast:       defaultTimelineKeepLastItems,
		memoryTemplate: template.Must(template.New("memory").Parse(memoryPrompt)),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	if m.keepLast < 0 {
		m.keepLast = 0
	}
	if m.trigger < 0 {
		m.trigger = 0
	}
	return m
}

func (tm *TimelineMemory) RebindContext(ctx context.Context) {
	if tm != nil && ctx != nil {
		tm.ctxMu.Lock()
		tm.ctx = ctx
		tm.ctxMu.Unlock()
	}
}

func (tm *TimelineMemory) getCtx() context.Context {
	tm.ctxMu.Lock()
	c := tm.ctx
	tm.ctxMu.Unlock()
	return c
}

// AddItem 添加记忆项
func (tm *TimelineMemory) AddItem(key string, value TimelineItemValue) error {
	item := &TimelineItem{
		CreateAt: time.Now(),
		Value:    value,
	}
	tm.items.Set(key, item)
	return nil
}

// GetItem 获取记忆项
func (tm *TimelineMemory) GetItem(key string) (*TimelineItem, bool) {
	return tm.items.Get(key)
}

// CreateTimeLineMemory 基于当前配置创建新的时间线记忆
func (tm *TimelineMemory) CreateTimeLineMemory(extraInfo func() string) (*TimelineMemory, error) {
	if tm == nil {
		return nil, fmt.Errorf("timeline memory is nil")
	}
	if extraInfo == nil {
		extraInfo = func() string { return "" }
	}
	return &TimelineMemory{
		extraInfo:      extraInfo,
		ctx:            tm.getCtx(),
		aiClient:       tm.aiClient,
		items:          utils.NewOrderMapx[string, *TimelineItem](),
		trigger:        tm.trigger,
		keepLast:       tm.keepLast,
		memoryTemplate: tm.memoryTemplate,
	}, nil
}

// DeleteItem 删除记忆项
func (tm *TimelineMemory) DeleteItem(key string) bool {
	if _, exists := tm.items.Get(key); !exists {
		return false
	}
	tm.items.Delete(key)
	return true
}

// GetAllItems 获取所有记忆项（按时间顺序）
func (tm *TimelineMemory) GetAllItems() []*TimelineItem {
	var items []*TimelineItem
	tm.items.ForEach(func(key string, value *TimelineItem) {
		items = append(items, value)
	})
	return items
}

// Compress 由外部控制触发的压缩：压缩除最近 keepLast 条外的所有时间线记忆
func (tm *TimelineMemory) Compress() error {
	if tm == nil || tm.aiClient == nil || tm.memoryTemplate == nil {
		return nil
	}

	keepLast := tm.keepLast
	if keepLast < 0 {
		keepLast = 0
	}

	keys := tm.items.Keys()
	if len(keys) == 0 || len(keys) <= keepLast {
		return nil
	}

	compressKeys := keys[:len(keys)-keepLast]
	toCompress := make([]*TimelineItem, 0, len(compressKeys))
	for _, key := range compressKeys {
		item, ok := tm.items.Get(key)
		if !ok || item == nil || item.Value == nil {
			continue
		}
		toCompress = append(toCompress, item)
	}
	if len(toCompress) == 0 {
		for _, key := range compressKeys {
			tm.items.Delete(key)
		}
		return nil
	}

	next, err := tm.summarize(toCompress)
	if err != nil {
		runtimelog.LogJSON("error", map[string]any{
			"event":             "timeline_memory_compress_failed",
			"compress_items":    len(toCompress),
			"compress_keys_len": len(compressKeys),
			"error":             err.Error(),
		})
		return err
	}
	tm.summary = strings.TrimSpace(next)
	for _, key := range compressKeys {
		tm.items.Delete(key)
	}
	return nil
}

// CompressOldMemories 自动压缩旧记忆
// 采用分半批量压缩策略：每次将最老的一半记忆合并压缩成一个摘要，直到总大小满足要求
func (tm *TimelineMemory) CompressOldMemories() error {
	if tm == nil || tm.aiClient == nil || tm.memoryTemplate == nil || tm.trigger <= 0 {
		return nil
	}

	for loops := 0; loops < timelineCompressMaxLoops; loops++ {
		if !tm.ShouldCompress() {
			return nil
		}

		keys := tm.items.Keys()
		if len(keys) == 0 || len(keys) <= tm.keepLast {
			return nil
		}

		compressable := len(keys) - tm.keepLast
		batch := compressable / 2
		if batch < 1 {
			batch = 1
		}
		compressKeys := keys[:batch]

		toCompress := make([]*TimelineItem, 0, len(compressKeys))
		for _, key := range compressKeys {
			item, ok := tm.items.Get(key)
			if !ok || item == nil || item.Value == nil {
				continue
			}
			toCompress = append(toCompress, item)
		}
		if len(toCompress) == 0 {
			return nil
		}

		next, err := tm.summarize(toCompress)
		if err != nil {
			runtimelog.LogJSON("error", map[string]any{
				"event":             "timeline_memory_compress_old_failed",
				"loop":              loops + 1,
				"compress_items":    len(toCompress),
				"compress_keys_len": len(compressKeys),
				"error":             err.Error(),
			})
			return err
		}
		tm.summary = strings.TrimSpace(next)
		for _, key := range compressKeys {
			tm.items.Delete(key)
		}
	}
	return nil
}

// GetTotalSize 计算当前记忆总大小
func (tm *TimelineMemory) GetTotalSize() int {
	if tm == nil {
		return 0
	}
	totalSize := len(strings.TrimSpace(tm.summary))
	tm.items.ForEach(func(key string, value *TimelineItem) {
		if value == nil || value.Value == nil {
			return
		}
		totalSize += len(value.Value.String())
	})
	return totalSize
}

// ShouldCompress 判断是否需要压缩
func (tm *TimelineMemory) ShouldCompress() bool {
	if tm == nil || tm.trigger <= 0 || tm.aiClient == nil {
		return false
	}
	return tm.GetTotalSize() > tm.trigger
}

// Summary 返回当前压缩摘要
func (tm *TimelineMemory) Summary() string {
	if tm == nil {
		return ""
	}
	return strings.TrimSpace(tm.summary)
}

// GetMemoryByType 按类型获取记忆
func (tm *TimelineMemory) GetMemoryByType(itemType TimelineItemType) []*TimelineItem {
	var result []*TimelineItem
	tm.items.ForEach(func(key string, value *TimelineItem) {
		if value.Value.Type() == itemType {
			result = append(result, value)
		}
	})
	return result
}

// TryCompressAsync 异步尝试压缩旧记忆
func (tm *TimelineMemory) TryCompressAsync() {
	if tm == nil || !tm.ShouldCompress() {
		return
	}
	go func() {
		_ = tm.CompressOldMemories()
	}()
}

// Clear 清空所有记忆
func (tm *TimelineMemory) Clear() {
	tm.items = utils.NewOrderMapx[string, *TimelineItem]()
	tm.summary = ""
}

// Len 返回当前记忆项数量
func (tm *TimelineMemory) Len() int {
	return tm.items.Len()
}

// String 返回时间线记忆的字符串表示
func (tm *TimelineMemory) String() string {
	var buf bytes.Buffer

	summary := strings.TrimSpace(tm.summary)
	if summary != "" {
		buf.WriteString("【时间线摘要】\n")
		buf.WriteString(summary)
		buf.WriteString("\n")
	}

	tm.items.ForEach(func(key string, item *TimelineItem) {
		if item == nil || item.Value == nil {
			return
		}
		buf.WriteString(item.Value.String())
		buf.WriteString("\n")
	})

	return buf.String()
}

// Dump 以稳定文本格式转储当前时间线内容，便于做增量 diff
func (tm *TimelineMemory) Dump() string {
	if tm == nil {
		return ""
	}

	var buf bytes.Buffer

	summary := strings.TrimSpace(tm.summary)
	if summary != "" {
		buf.WriteString("timeline_memory_summary:\n")
		for _, line := range strings.Split(summary, "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" {
				continue
			}
			buf.WriteString("  ")
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}

	if tm.items == nil || tm.items.Len() == 0 {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			return ""
		}
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out
	}

	buf.WriteString("timeline_memory_items:\n")
	tm.items.ForEach(func(key string, item *TimelineItem) {
		if item == nil || item.Value == nil {
			return
		}
		ts := item.CreateAt.UTC().Format(time.RFC3339Nano)
		buf.WriteString(fmt.Sprintf("--[%s] key: %s type: %s\n", ts, key, item.Value.Type()))

		raw := strings.TrimRight(item.Value.String(), "\n")
		raw = strings.TrimRight(raw, "\r")
		if raw == "" {
			buf.WriteString("  (empty)\n")
			return
		}
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimRight(line, "\r")
			buf.WriteString("  ")
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	})

	out := buf.String()
	if strings.TrimSpace(out) == "" {
		return ""
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func (tm *TimelineMemory) summarize(items []*TimelineItem) (string, error) {
	if tm == nil || tm.aiClient == nil || tm.memoryTemplate == nil {
		return "", fmt.Errorf("timeline memory summarizer is nil")
	}
	if len(items) == 0 {
		return strings.TrimSpace(tm.summary), nil
	}

	bufMemory := bytes.NewBuffer(nil)
	err := tm.memoryTemplate.Execute(bufMemory, map[string]any{
		"EXTRA_CONTEXT":  tm.extraInfo(),
		"PREV_SUMMARY":   strings.TrimSpace(tm.summary),
		"COMPRESS_ITEMS": items,
		"NONCE":          generateRandom(8),
	})
	if err != nil {
		return "", fmt.Errorf("execute memory template failed: %w", err)
	}

	prompt := bufMemory.String()
	ctx := tm.getCtx()
	var lastErr error
	for attempt := 0; attempt <= defaultAIOutputMaxRetries; attempt++ {
		if ctx != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}
		if attempt > 0 {
			if err := sleepWithContext(ctx, retryDelay(attempt)); err != nil {
				return "", err
			}
		}

		resp, err := tm.aiClient.ChatText(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf("chat text failed: %w", err)
		}
		summary := strings.TrimSpace(resp)
		if summary != "" {
			return summary, nil
		}
		lastErr = fmt.Errorf("summary is empty")
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("summary is empty")
	}
	return "", lastErr
}
