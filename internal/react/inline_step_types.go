package react

import (
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// stepHistoryBucket 为单个 inline step 隔离一份 think_act 用的 stepHistory。
//
// 设计要点：
//   - 每桶由其自身的 think_act goroutine 单写——不需要桶级锁；并发安全靠
//     Agent.stepHistoryMu 仅保护 map 增删 + 调度者契约（同一 stepID 不并发起两个桶）。
//   - msgs 起点必须是 deep copy（见 NewStepHistoryBucket 注释），共享 slice header
//     会导致与主 scheduler 在主 history 上 append 时产生 race（go test -race 必爆）。
//   - 单桶 fallback：MaxParallelSteps=1 时 stepHistories 仍是 map 但仅 1 个 entry，
//     代码路径与多桶一致——不保留独立分叉，避免 fallback 变质成死代码。
//
// **immutable contract（R1-4 防线）**：
//
// peer 桶 seed 来自 `append([]*ai.MsgInfo(nil), a.stepHistory...)`——slice header 是
// 新数组，但元素是 *MsgInfo 指针，桶与主 history 共享同一组结构体。任何位置（含
// compaction / sanitize / 重写）若就地 mutate `*MsgInfo` 的字段（Content / ToolCalls
// / Role / ...），peer 与主路径会跨桶看见中间态——属于 R1 级污染。
//
// 修复模式：要重写一条 msg，clone 结构体 + 替换 slice 元素，绝不就地改：
//
//	newMsg := *msg                 // 值拷贝整个结构体
//	newMsg.Content = rewritten     // 在 clone 上写
//	stepHistory[idx] = &newMsg     // 替换 slice 元素，原 *MsgInfo 不动
//
// CI grep 守卫（PR 时确认）：
//
//	grep -rn 'msg\.Content\s*=\|msg\.ToolCalls\s*=' internal/react/ --include='*.go' \
//	  | grep -v _test.go | grep -v inline_step_types.go
//	# 应只命中：(a) 全新构造 ai.NewAIMsgInfo 后立即赋值（局部变量，未 enqueue）；
//	#         (b) shortenOldToolResults 的 clone 替换路径（newMsg.Content = ...）。
type stepHistoryBucket struct {
	stepID  string
	phase   builtin_tools.AgentPhase
	planVer int
	msgs    []*ai.MsgInfo
	seedLen int // ready 时刻从主 history clone 的起点长度，便于审计与诊断
}

// NewStepHistoryBucket 用主 history 当前末尾的 deep copy 作为种子，构造一个干净的桶。
// 调用方必须持有 Agent.stepHistoryMu 或确保自己在 scheduler 单写者上下文里。
//
// **必须 deep copy**：append 到 nil slice 强制分配新底层数组，与主 history 的底层数组
// 完全脱钩；这是并发安全的硬约束，不能优化为切片别名。
func NewStepHistoryBucket(stepID string, phase builtin_tools.AgentPhase, planVer int, seed []*ai.MsgInfo) *stepHistoryBucket {
	msgs := append([]*ai.MsgInfo(nil), seed...)
	return &stepHistoryBucket{
		stepID:  stepID,
		phase:   phase,
		planVer: planVer,
		msgs:    msgs,
		seedLen: len(msgs),
	}
}

// bucketFor 取出指定 stepID 的桶；不存在则返回 nil（调用方负责通过
// ensureBucket 创建或视为非 inline step 路径）。stepHistoryMu 仅在 map 增删时锁。
func (a *Agent) bucketFor(stepID string) *stepHistoryBucket {
	if a == nil || stepID == "" {
		return nil
	}
	a.stepHistoryMu.Lock()
	defer a.stepHistoryMu.Unlock()
	if a.stepHistories == nil {
		return nil
	}
	return a.stepHistories[stepID]
}

// ensureBucket 取桶或新建：已存在则返回原桶（幂等），否则用 seed 新建一个 deep-copy 桶。
// 入参 seed 通常是 a.history 末尾片段；调用方应在 scheduler 单写者上下文里读 a.history
// 后立即传入，避免读到正在 append 的中间态。
func (a *Agent) ensureBucket(stepID string, phase builtin_tools.AgentPhase, planVer int, seed []*ai.MsgInfo) *stepHistoryBucket {
	if a == nil || stepID == "" {
		return nil
	}
	a.stepHistoryMu.Lock()
	defer a.stepHistoryMu.Unlock()
	if a.stepHistories == nil {
		a.stepHistories = make(map[string]*stepHistoryBucket)
	}
	if existing, ok := a.stepHistories[stepID]; ok && existing != nil {
		return existing
	}
	bucket := NewStepHistoryBucket(stepID, phase, planVer, seed)
	a.stepHistories[stepID] = bucket
	return bucket
}

// historyMsgsFor 返回 inline step 当前活跃的 stepHistory 切片：
//   - runCtx != nil && runCtx.Bucket != nil → 返回桶内 msgs（inline 路径）
//   - 其余情况 → 退化为主 a.stepHistory（plan / step_replan / 单 step 主路径）
//
// **并发安全**：桶内 msgs 由该 stepID 自己的 goroutine 单写，外层调用者必须保证
// 同一 stepID 不并发——这是 runStepsConcurrently（commit 8）的调度契约。
func (a *Agent) historyMsgsFor(runCtx *InlineStepCtx) []*ai.MsgInfo {
	if a == nil {
		return nil
	}
	if runCtx != nil && runCtx.Bucket != nil {
		return runCtx.Bucket.msgs
	}
	return a.stepHistory
}

// appendHistoryMsgFor 把一条消息 append 到 inline step 的活跃 history（桶或主）。
// runCtx 路由规则与 historyMsgsFor 一致。
func (a *Agent) appendHistoryMsgFor(runCtx *InlineStepCtx, msg *ai.MsgInfo) {
	if a == nil || msg == nil {
		return
	}
	if runCtx != nil && runCtx.Bucket != nil {
		runCtx.Bucket.msgs = append(runCtx.Bucket.msgs, msg)
		return
	}
	a.stepHistory = append(a.stepHistory, msg)
}

// setHistoryMsgsFor 整体替换 inline step 的活跃 history（典型场景：compaction 后写回）。
// runCtx 路由规则与 historyMsgsFor 一致。
func (a *Agent) setHistoryMsgsFor(runCtx *InlineStepCtx, msgs []*ai.MsgInfo) {
	if a == nil {
		return
	}
	if runCtx != nil && runCtx.Bucket != nil {
		runCtx.Bucket.msgs = msgs
		return
	}
	a.stepHistory = msgs
}

// runCtxStepID 仅从 runCtx 取 StepID（不 fallback 到 snapshot）。供 EmitThink 等
// "没有 snapshot 在手" 的事件路由使用——peer 路径 → 桶 StepID；主路径 → ""（plan/replan
// 等没有"当前 step"概念的阶段也应是 ""）。
func runCtxStepID(runCtx *InlineStepCtx) string {
	if runCtx == nil {
		return ""
	}
	return strings.TrimSpace(runCtx.StepID)
}

// effectiveStepID 在 runCtx 优先于 snapshot.CurrentStepID 的顺序下取出当前真实 stepID。
//
// 关键路径：peer goroutine 跑时 a.state.Snapshot().CurrentStepID 永远是**主 step ID**
// （state 只有一个 CurrentStepID），所有从 snapshot 直接读 stepID 的代码点（tool
// timeline append、ToolRuntimeInfo.CurrentStepID、step file gate 等）必须先看 runCtx
// 才能拿到 peer 的真实 stepID，否则会把 peer 的工具调用记到主 step 的 timeline 文件、
// 让 ToolRuntimeInfo 的 stepID 错配。
//
// 主路径（runCtx == nil）退化到 snapshot.CurrentStepID，行为不变。
func effectiveStepID(runCtx *InlineStepCtx, snapshot builtin_tools.StateSnapshot) string {
	if runCtx != nil {
		if id := strings.TrimSpace(runCtx.StepID); id != "" {
			return id
		}
	}
	return strings.TrimSpace(snapshot.CurrentStepID)
}

// dropBucket 删除指定 stepID 的桶；调用方应在该 stepID 的 goroutine 结束之后调用，
// 避免 goroutine 仍在写 msgs 时桶被回收（map 删除本身受锁保护，但桶内 slice 写没锁）。
func (a *Agent) dropBucket(stepID string) {
	if a == nil || stepID == "" {
		return
	}
	a.stepHistoryMu.Lock()
	defer a.stepHistoryMu.Unlock()
	if a.stepHistories == nil {
		return
	}
	delete(a.stepHistories, stepID)
}

// InlineStepCtx 是显式传递的 inline step 运行上下文。
//
// **不**用 ctx.Value 注入：ctx.Value 在 Go 社区公认仅适用于 request-scoped 元数据
// （trace_id / auth），业务路由用它是反模式——任何调用栈忘传 ctx 都会让 FinalAnswerAllowed
// 等开关失效；最坏情况下 inline peer 桶可以调到 submit_final_answer 触发 state 双写。
// 改为显式参数后编译器全程兜底，新增调用方在编译期就被指认是否要传 runCtx。
type InlineStepCtx struct {
	StepID string
	Bucket *stepHistoryBucket
	// FinalAnswerAllowed 控制 BuildFunctionTools 是否注册 submit_final_answer。
	//   - current step 的 bucket：true（可调 final_answer 结束本轮主路径）
	//   - peer step 的 bucket：  false（peer 完成只翻 PlanItem 终态，禁止 finalize 整个 agent）
	FinalAnswerAllowed bool

	// LocalActiveSkillNames 是 peer 自治的 skill 集合 overlay（方案 B per-peer 隔离）。
	//
	// 为什么需要：旧实现 ActiveSkillNames 是 Agent.state 全局单一 list，3 peer 并发
	// 同时调 load_skill/eject_skill 会互相串扰（peer A 加的 skill peer B 也看见、peer A
	// 拔掉 peer B 在用的 skill 让其 prompt 失去 injected 内容）。
	//
	// 行为：
	//   - spawnInlinePeer 时 deep-copy 当时全局 snapshot.ActiveSkillNames 作为 baseline
	//   - peer 内 load_skill / eject_skill 只改本 overlay，不动全局 state（互不串扰）
	//   - peer 退出时整个 runCtx 丢弃，overlay 自然清理（不持久化、不污染主路径下一步）
	//   - 主路径 runCtx == nil 或 LocalActiveSkillNames == nil 时退化到原行为（读写全局 state）
	//
	// 用 *[]string 而非 []string：nil 区分「未初始化（应继承全局）」与「已分叉但空（peer 手动 eject 干净）」。
	LocalActiveSkillNames *[]string
}
