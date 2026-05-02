package memory

import (
	"time"

	"aster/internal/utils"
)

// TimelineMemorySnapshot 时间线记忆快照
type TimelineMemorySnapshot struct {
	Summary string
	Items   map[string]*TimelineItem
}

// Snapshot 创建当前状态快照
func (tm *TimelineMemory) Snapshot() *TimelineMemorySnapshot {
	if tm == nil {
		return nil
	}

	snap := &TimelineMemorySnapshot{
		Summary: tm.summary,
		Items:   make(map[string]*TimelineItem),
	}

	tm.items.ForEach(func(key string, value *TimelineItem) {
		if value != nil {
			snap.Items[key] = &TimelineItem{
				CreateAt: value.CreateAt,
				Value:    value.Value,
			}
		}
	})

	return snap
}

// Restore 从快照恢复状态
func (tm *TimelineMemory) Restore(snap *TimelineMemorySnapshot) {
	if tm == nil || snap == nil {
		return
	}

	tm.summary = snap.Summary
	tm.items = utils.NewOrderMapx[string, *TimelineItem]()

	// 按时间排序恢复
	type kv struct {
		key  string
		item *TimelineItem
	}
	sorted := make([]kv, 0, len(snap.Items))
	for k, v := range snap.Items {
		sorted = append(sorted, kv{key: k, item: v})
	}
	// 简单冒泡排序（快照通常不大）
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].item.CreateAt.Before(sorted[i].item.CreateAt) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	for _, kv := range sorted {
		tm.items.Set(kv.key, kv.item)
	}
}

// GenerateItemKey 生成唯一的记忆项 key
func GenerateItemKey() string {
	return generateRandom(16) + "-" + time.Now().UTC().Format("20060102150405")
}
