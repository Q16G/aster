# Changelog

本项目所有显著变更记录于此文件。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本遵循语义化版本（预发布阶段）。

## [v1.1.0-beta-5] - 2026-06-21

本版本的主线是 **inline_step 多 peer 并发重构**：把原 `remote_step` 自动 fan-out
重写为同进程内多 peer 真并发执行，并围绕「多个并发 step 的状态隔离与事件归属」
做了系统性收敛——状态副作用收口到 observer、prompt/skill/流式缓冲全部按
`(agent, stepID)` 实体隔离，配套大量并发红线测试与一批稳定性修复。

### 新增 (Features)

- **react**：peer per-step skill overlay 隔离——并发 peer 的 `load/eject_skill`
  走 `LocalActiveSkillNames` overlay 而非全局 state，多个 peer 互不串扰。
- **react/emitter**：流式 token 携带 stepID 归属，新增显式 `EmitStreamEnd` /
  `EventTypeStreamEnd` 结束信号，下游不再靠结构事件猜测该 flush 谁。
- **tui**：下区 peer tile bar 渲染并发 peer；新增 step detail 全屏焦点态；
  流式、思考、卡片展开态全部按 stepID / part 实体隔离。
- **react**：`remote_step` automated fan-out 原始实现（重构前 baseline）。
- **react/step_replan**：事实板治理纪律，仅指针化阻塞阅读老段并保留事实演进。

### 性能 (Performance)

- **react**：frozen step prompt 缓存按 `(stepID, planVer)` 分桶，消除多 peer
  并发派生 prompt 时的 race 与缓存抖动。

### 重构 (Refactor)

- **react**：state observer 收口 plan_item 副作用，observer 接管原先散落各处的
  `emitTaskItemDiffs` 手 emit 与 `ensureStepFileScaffold` 手调。
- **全仓命名**：`remote_step` → `inline_step` 统一收口。
- **react**：`runStepPhase` 接入 `runStepsConcurrently` 实现真并发；抽出
  `spawnInlinePeer` / `runInlineStep` think_act 本体；删除二次 fan-out 与
  `step_fanout.go`。
- **react**：新增 `stepHistories` 多桶 + `InlineStepCtx` 类型，`dispatchToolCalls`
  /`AICallProxy`/`BuildFunctionTools` 链路 plumbing `*InlineStepCtx`。
- **react**：`UpdateRemotePlanItem` → `UpdateInlineStep` 命名重构。
- **react/workspace_runtime**：共享 ledger 文件加 per-file RWMutex 兜底，读取统一
  走 workspaceRuntime；配套并发 ledger 所有权 + per-step OI 命名空间纪律。
- **tui**：event 路由切到 `InlineStepPart` 独立类型；`ExpandableCardPart` 改接口；
  `parts_store` 加 `idxByStepID` + `InlineStepsThisTurn` 索引。

### 修复 (Fixes)

- **tui**：修正 Ollama `name:tag` 模型名（如 `qwen3.6:35b-a3b-coding-mxfp8`）被误当
  `model:variant` 语法截断成 `qwen3.6`，导致请求 Ollama 返回 404。
- **react**：A4 守卫按 Kind 过滤，修复死锁（P0-1）。
- **react/agent_execute**：peer 桶屏蔽 `await_subagents` / `update_current_step`
  防最坏死锁；peer 桶 compaction 不写主 transcript blob。
- **react**：peer terminal 持久化 `persistBucketTranscriptBlob`；tool timeline /
  runtime info 优先用 runCtx stepID，修复 timeline 错配（P0-2）。
- **react/step_history_compact**：compaction 改 immutable 重写，不修改原 MsgInfo。
- **react/runtime_scheduler**：`submit_plan` schema 加 `minItems`、错误消息附 plan
  item 样板；粒度校验 3 次未收敛降级放行。
- **react/state**：`UpdateInlineStep` 加 item 已终态守卫。
- **react/step_inline**：`spawnInlinePeer` 兜底 spawn 段失败。
- **react/emitter**：`EmitToolStart/End` 补 stepID，`ToolPart.StepID` 正确填入。
- **react/prompts**：OI 编号 per-step 命名空间规则修正。
- **react/phase_step_replan**：`readSharedFileOptional` 短路顺序回归旧版语义。

### 测试 (Tests)

- inline_step 多 peer 并发红线测试覆盖（state observer、frozen cache、peer skill
  overlay、emitter 分组、stream_end、thinking 隔离、step detail、tile bar）。
- 补回此前 commit message 声称却漏写的红线测试。

### 文档 (Docs)

- `AICallProxy/Stream` 与 `sharedFileLocks` 注释据实更新。

[v1.1.0-beta-5]: https://github.com/Q16G/aster/compare/v1.1.0-beta-4...v1.1.0-beta-5
