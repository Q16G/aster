# 大项目分批扫描协议

> 被 [SKILL.md](../SKILL.md) §8 引用。本文件**只承载大项目分批的操作细节**——主能力骨架、判定上限、产物契约仍以 SKILL.md 为准。

## 何时触发

执行扫描前先用 `list_files` 估算扫描面文件数。`list_files` 默认上限 5000、最高 20000，超出会被截断——这本身就是信号。

**触发条件**：扫描面文件数 > 5000 → 进入分批模式。

## 切分原则

按**顶层模块 / 目录边界**切分，**不要机械地按"每 5000 文件一片"切**。优先沿项目已有的模块边界走：

- maven 多模块的各 module 目录
- go 的各子 module / 子服务目录
- monorepo 下的 `service-a/` / `service-b/` / `web/` 等顶层目录

每个子目录作为一次独立 `semgrep scan` 的 `<target_path>`。

## 每批命令

复用 SKILL.md §8 的标准命令，只把 `<target_path>` 换成子目录；`--json --timeout 600 --max-memory 4096 --jobs 4` 与排除项保持不变；bash 工具显式传 `timeout_ms`：

```bash
semgrep scan --config "$HOME/.aster/rules/<lang>" <module_path> \
  --json --timeout 600 --max-memory 4096 --jobs 4 \
  --exclude .git --exclude node_modules --exclude vendor \
  --exclude dist --exclude build --exclude out --exclude target
```

## 并发约束（必须遵守）

> **why**：单进程内 `--jobs 4` 已充分利用多核；多进程并行起多个 `semgrep` 会让 N 个 semgrep-core 同时驻留导致内存爆炸（`--max-memory 4096` × N 进程 ≈ 不可控峰值），同时规则集解析 / AST 缓存无法共享降低效率。

**硬规则**：
- 分批 = **串行执行**，不是并发分发
- 同一时刻 ≤ 1 个 semgrep 进程驻留
- 由子 Agent 执行分批时，主 Agent 必须按串行（同步 `sub_agent` 或队列式后台）派发
- **禁止** `run_in_background=true` 同时起多个分片子 Agent 各自跑 semgrep

## 单批失败隔离

某批超时或 OOM 时：
- 把该模块明确记入"扫描缺口"
- 其余批的结果仍然有效，**不得**因为一批失败就放弃全部
- 缺口模块按 SKILL.md §9 闭环要求，触发 `partial-coverage` 结论降级

## 结果归并（关键）

分批是手段，最终仍必须输出**一份**报告：

| 维度 | 归并方式 |
|---|---|
| 覆盖声明 | 扫描面统计（文件数 / XML mapper / 配置 / 模板）= 各批之和；并列出本次实际分了哪几批、每批对应哪个目录 |
| 三个分桶 | `high_confidence` / `needs_dataflow_confirmation` / `high_noise_patterns` 跨批统一归并、统一去重后再输出 |
| 产物 jsonl | 各批的命中按 SKILL.md §9 字段约束统一 append 到 `shared/coverage-ledger/findings/sast-scan.jsonl`；`id` 跨批保持全局唯一（`sast-001` / `sast-002` ... 递增） |

仍受 SKILL.md §9 全部硬约束：禁止聚合计数、每个 finding 独占一行、不得用"等 / 略"省略。**分批不是省略 finding 的借口。**
