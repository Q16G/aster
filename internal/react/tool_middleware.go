package react

import (
	"context"
	"fmt"
	"time"

	"aster/internal/builtin_tools"
)

// 工具调用洋葱中间件：链只包「Execute 窗口」——Tool 实例已解析、callCtx 已装配好
// → 拿到规整化的执行结果（含截断）。编排逻辑（allowedTools 校验 / 参数解析 /
// human_confirm 分流 / EmitToolStart / 回填 history / timeline / EmitToolEnd）留在
// 宿主：它们持有跨调用顺序敏感的副作用，进链会让所有中间件被迫理解调度器控制流。
// 顺序与并发两条派发路径统一穿同一条 a.toolExecChain，builtin / MCP / sub_agent /
// skill 全部结构性覆盖。设计全文见 docs/context-governance-tool-middleware.md §5。

// toolExecCall 是 Execute 窗口的输入。宿主在解析 / 校验 / ctx 装配完成后构造，
// 契约：Tool 非 nil、Args 非 nil。PrevSnapshot 由宿主派发前捕获一次（并发路径
// 每批 isolate 一次、各 call 浅拷贝共享），链内只读、禁止 per-call 重新 Snapshot。
type toolExecCall struct {
	CallID       string
	ToolName     string
	Tool         Tool
	Args         map[string]any
	IsAgent      bool // Agent-as-Tool：base 据此跳过超时
	StackDepth   int
	Iter         int
	StepID       string                      // 供链内日志 / 过滤
	Phase        builtin_tools.AgentPhase    // 供 per-phase 自过滤
	PrevSnapshot builtin_tools.StateSnapshot // 宿主捕获，链内不读 a.state
}

// toolExecResult 是 Execute 窗口的产物。中间件可在 post 段改写 Out/ErrText（如截断）。
type toolExecResult struct {
	Out          string // 回填用输出（可能已截断）
	Err          error  // Execute 原始错误（含超时包装）
	ErrText      string // 截断后的错误文本
	OutTruncated bool
	ErrTruncated bool
	OutFullPath  string        // 截断时全量落盘路径
	Duration     time.Duration // 仅 Execute 本体耗时（不含截断等 post 段）
}

// toolExecHandler 是链节点。error 保留给链自身故障（base 恒返回 nil）；工具业务
// 错误进 result.Err/ErrText 回填给 LLM——工具失败不终止回合，与宿主现状语义一致。
//
// 中间件契约（并发安全底线）：链在并发工具 goroutine 里同时执行，中间件不得写宿主
// 可变状态（stepHistory / state / timeline），只允许加工 call/res 与调用并发安全设施
// （Emitter / 落盘 IO）。
type toolExecHandler func(ctx context.Context, call *toolExecCall) (*toolExecResult, error)

// toolMiddleware 包裹 next：next 之前为 pre 段、之后为 post 段。
type toolMiddleware func(next toolExecHandler) toolExecHandler

// chainToolMiddlewares 按 net/http 惯例装配：mws[0] 为最外层
// （pre 由外向内、post 由内向外执行）。
func chainToolMiddlewares(base toolExecHandler, mws ...toolMiddleware) toolExecHandler {
	h := base
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] == nil {
			continue
		}
		h = mws[i](h)
	}
	return h
}

// baseToolExecHandler 是最内层：超时装配 + tool.Execute + 超时错误包装。
// 超时不做成中间件——它是调用策略（依赖 cfg 与 isAgent 分支），留在 base
// 保住「isAgent 免超时」的现状语义。
func (a *Agent) baseToolExecHandler(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
	res := &toolExecResult{}
	execCtx := ctx
	cancelTimeout := func() {}
	var toolTimeout time.Duration
	if !call.IsAgent {
		toolTimeout = a.cfg.resolveToolTimeout(call.Args)
		execCtx, cancelTimeout = context.WithTimeout(ctx, toolTimeout)
	}
	defer cancelTimeout()

	out, err := call.Tool.Execute(execCtx, call.Args)
	if err != nil && !call.IsAgent && execCtx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("tool %q timed out after %s: %w", call.ToolName, toolTimeout, err)
	}
	res.Out = out
	res.Err = err
	if err != nil {
		res.ErrText = err.Error()
	}
	return res, nil
}

// toolOutputTruncateMiddleware 是默认链的 post 截断：复用 TruncateToolOutput 纯函数
// 截 Out/ErrText、全量落盘并记截断日志。放在耗时中间件外层，保证 Duration 只计
// Execute 本体（与旧内联窗口一致）；用户中间件在更外层，post 段看到的即截断后的
// 最终回填内容（与 LLM 看到的一致），需要原始全量时读 res.OutFullPath。
func (a *Agent) toolOutputTruncateMiddleware(next toolExecHandler) toolExecHandler {
	return func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
		res, err := next(ctx, call)
		if err != nil || res == nil {
			return res, err
		}
		res.Out, res.OutTruncated, res.OutFullPath = TruncateToolOutput(call.ToolName, res.Out, a.workspaceRootDir)
		res.ErrText, res.ErrTruncated, _ = TruncateToolOutput(call.ToolName+"-error", res.ErrText, a.workspaceRootDir)
		if res.OutTruncated || res.ErrTruncated {
			a.emitRuntimeLog("info", "tool output truncated", call.PrevSnapshot, map[string]any{
				"event":         "tool_output_truncated",
				"tool":          call.ToolName,
				"out_truncated": res.OutTruncated,
				"err_truncated": res.ErrTruncated,
			})
		}
		return res, nil
	}
}

// toolExecDurationMiddleware 紧贴 Execute 计时（最内层中间件）。
func toolExecDurationMiddleware(next toolExecHandler) toolExecHandler {
	return func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
		start := time.Now()
		res, err := next(ctx, call)
		if res != nil {
			res.Duration = time.Since(start)
		}
		return res, err
	}
}

// buildToolExecChain 装配 Agent 级链：用户中间件（WithToolMiddleware 注册序 =
// 由外向内）在最外层，默认链（截断 → 耗时）在内。构造期装配、运行期不可变——
// 链在并发工具 goroutine 里同时执行，不可变函数值是最便宜的并发安全。
func (a *Agent) buildToolExecChain() toolExecHandler {
	mws := make([]toolMiddleware, 0, len(a.cfg.toolMiddlewares)+2)
	mws = append(mws, a.cfg.toolMiddlewares...)
	mws = append(mws, a.toolOutputTruncateMiddleware, toolExecDurationMiddleware)
	return chainToolMiddlewares(a.baseToolExecHandler, mws...)
}
