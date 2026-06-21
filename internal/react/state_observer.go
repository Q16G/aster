package react

import (
	"strings"

	"aster/internal/builtin_tools"
)

// PlanItem 状态变化的 actor 类别——observer 据此区分订阅副作用：
//   - planItemActorMain：主路径（runStepPhase → MarkCurrentStepInProgress/UpdateCurrentStep
//     / explicit update_current_step 工具）
//   - planItemActorPeer：inline peer 路径（spawnInlinePeer → MarkInlineStepInProgress；
//     peer auto-complete → UpdateInlineStep）
//   - planItemActorSystem：结构性 plan 替换（UpdatePlan / ApplyStepReplan / Replace），
//     一次可能产出多条变化
const (
	planItemActorMain   = "main"
	planItemActorPeer   = "peer"
	planItemActorSystem = "system"
)

// PlanItemChange 描述一次 PlanItem 状态翻转。由 StateTracker 在持锁段产出
// （prev/post diff 在同一 mutator 内完成），随后由 fanoutPlanItemChangesLocked
// 串行调用 observer。Item 是 post-mutation 的 deep copy，observer 可任意使用
// 不需要再加锁。
//
// PrevStatus 为空字符串表示该 PlanItem 在 prev 快照中不存在（plan 替换新增项）。
// 对应 fanout 顺序是「mutator 进入临界区的顺序」，因为整个 prev→mutate→diff→
// fanout 都在同一把 writer 锁里——不会与并发 mutator 错位。
type PlanItemChange struct {
	StepID     string
	Index      int
	PrevStatus builtin_tools.PlanStepStatus
	NextStatus builtin_tools.PlanStepStatus
	Item       *builtin_tools.PlanItem
	Actor      string
	Reason     string
	// CurrentStepID 是 mutator 完成时 state.CurrentStepID 的快照值。
	// observer 用它判定「新建的 PlanItem 是否是当前选中 step」——保留旧
	// emitTaskItemDiffs 的「全 plan 创建时仅 emit 当前选中条目」语义。
	CurrentStepID string
}

// StateObserver 接收 PlanItem 状态变化。回调在 StateTracker writer 锁内被调用，
// observer 不可回调任何 StateTracker mutator（会死锁）；observer 内部走外部 sink
// （emitter.Emit、文件 IO）即可，不应回拨 state。
type StateObserver interface {
	OnPlanItemChange(change PlanItemChange)
}

// snapshotPlanStatusesLocked 抓 plan 当前 PlanItem.Status 的 ID→Status 映射，
// 用于 mutator 末尾 diff 计算。调用方持锁。
func (t *StateTracker) snapshotPlanStatusesLocked() map[string]builtin_tools.PlanStepStatus {
	if t == nil || t.state == nil || len(t.state.Plan) == 0 {
		return nil
	}
	out := make(map[string]builtin_tools.PlanStepStatus, len(t.state.Plan))
	for _, item := range t.state.Plan {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		out[id] = item.Status
	}
	return out
}

// diffPlanStatusesLocked 比对 prev 快照与当前 t.state.Plan，产出 PlanItemChange 列表。
// 调用方持锁。仅产出 status 实际变化的条目（含 prev 不存在的新增项 → PrevStatus=""）。
// Item 字段是 post-mutation 的深拷贝（含 DependsOn 副本，ResolvedDependsOn 置 nil
// 让 observer 不读未持锁字段）。
func (t *StateTracker) diffPlanStatusesLocked(prev map[string]builtin_tools.PlanStepStatus, actor, reason string) []PlanItemChange {
	if t == nil || t.state == nil || len(t.state.Plan) == 0 {
		return nil
	}
	currentID := strings.TrimSpace(t.state.CurrentStepID)
	var changes []PlanItemChange
	for idx, item := range t.state.Plan {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		prevStatus, existed := prev[id]
		if existed && prevStatus == item.Status {
			continue
		}
		clone := *item
		if len(item.DependsOn) > 0 {
			clone.DependsOn = make([]string, len(item.DependsOn))
			copy(clone.DependsOn, item.DependsOn)
		}
		clone.ResolvedDependsOn = nil
		changes = append(changes, PlanItemChange{
			StepID:        id,
			Index:         idx,
			PrevStatus:    prevStatus,
			NextStatus:    item.Status,
			Item:          &clone,
			Actor:         actor,
			Reason:        strings.TrimSpace(reason),
			CurrentStepID: currentID,
		})
	}
	return changes
}

// fanoutPlanItemChangesLocked 在 mutator writer 锁内串行调用 observer。
// observer 回调禁止反向锁 state；fanout 顺序与 mutator 临界区顺序一致。
func (t *StateTracker) fanoutPlanItemChangesLocked(changes []PlanItemChange) {
	if t == nil || len(changes) == 0 || len(t.observers) == 0 {
		return
	}
	observers := append([]StateObserver(nil), t.observers...)
	for _, change := range changes {
		for _, obs := range observers {
			if obs == nil {
				continue
			}
			obs.OnPlanItemChange(change)
		}
	}
}

// RegisterObserver 注册 PlanItem 状态变化订阅者。多个 observer 按注册顺序被调用。
// 在 Agent.Execute 启动前注册；运行中注册不报错但可能错过早期 mutation。
func (t *StateTracker) RegisterObserver(obs StateObserver) {
	if t == nil || obs == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.observers = append(t.observers, obs)
}
