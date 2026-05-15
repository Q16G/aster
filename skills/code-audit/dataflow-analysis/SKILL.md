---
name: dataflow-analysis
description: 数据流分析与退化复核指南，使用 SyntaxFlow MCP 做 topdef/bottomUse/注解查找；当 SSA 不可用时，执行固定 fallback checklist。
tags: code-audit,dataflow,syntaxflow,mcp
when-to-use: 当需要对 semgrep 候选集做数据流确认，或需要分析 request -> session、cookie -> auth decision、owner -> mapper 等跨函数链路时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path] [--lang java|go|python|js|php|c]"
arguments:
  - target_path
  - lang
---

# 数据流分析（SyntaxFlow MCP + Fallback）

## 目标

这个 skill 的职责不是“看到污点就追一遍”，而是把 `sast-scan` 产出的下列线索做成**结构化确认**：

- request-derived value -> session write
- cookie-derived value -> branch / auth decision
- owner/operator arg -> service -> mapper/query
- controller -> service -> repository / mapper 的权限或 ownership 丢失

如果 `SyntaxFlow` 可用，则用 SSA 做 topdef / bottomUse。  
如果不可用，则必须执行固定 fallback checklist，不允许只写“转人工审计”。

## 何时使用

优先用于以下场景：

- `needs_dataflow_confirmation: true`
- 规则命中了 `session`、`cookie`、`request attribute`
- 怀疑存在 ownership / IDOR / authz 问题
- 需要确认 controller/service/mapper 是否丢失操作者约束

## SSA 可用时的工作流

### 1. 编译目标

先确认 `syntaxflow` MCP 状态；若正常，编译目标项目：

- `ssa_compile(target, language, program_name)`

推荐把 `program_name` 命名成与项目或样本相关的稳定名字，便于复用查询。

### 2. 使用通用查询模板

不要围绕单个项目字段名写查询。优先从以下通用模板出发。

#### 模板 A：request-derived value -> session

适用于：

- `request.getParameter/getHeader/getCookies/getReader`
- `request.getAttribute(...)`
- filter / decoder / middleware 派生值

查询目标：

- 是否流向 `getSession().setAttribute(...)`
- 是否经由 `HttpSession session = ...` 的别名 sink

#### 模板 B：cookie-derived value -> branch/auth decision

查询目标：

- cookie 值是否参与 `if/else`、权限比对、身份相等判断
- cookie 值之后是否进入管理操作、敏感 service 调用、对象创建/删除/更新

#### 模板 C：owner/operator -> service -> mapper/query

查询目标：

- 方法是否同时接收操作者身份参数和目标对象参数
- service / mapper 调用是否丢弃 owner/operator 约束
- 最终 query 是否只保留 target/resource ID

### 3. 对输出做判定

对每个链路给出之一：

- `Confirmed`
- `Needs Review`
- `False Positive`

并补充：

- source
- intermediate vars
- sink / query call
- 是否仍需业务语义复核

## Java Web 的固定查询主题

对于 Java Web 项目，至少覆盖以下五类主题：

1. `request` / `request attribute` -> `session`
2. `Cookie` -> branch / auth decision
3. `controller(owner, target)` -> `service(owner, target)` -> `mapper(target)`
4. `request.getAttribute()` -> permission `if` branch（filter 解密数据入权限判断）
5. `service method params` -> `mapper XML #{}/$ {}` 绑定完整性

如果时间有限，也必须优先保证前三类，而不是随机追踪单个 sink。

## SSA 查询模板（扩展）

以下模板是通用 SSA 查询的扩展。模板 A/B/C 见上文，以下是阶段2新增的 D/E/F。

### 模板 D：跨层 ownership 分析

适用于：Semgrep `idor-ownership-drop` 命中后的深度确认。

- 输入：controller 方法签名（含 operator 和 target 参数）
- 查询步骤：
  1. 从 controller 方法入口开始 `bottomUse` 追踪 operator 参数
  2. 检查 operator 是否传递给 service 层方法
  3. 检查 service 是否继续传递给 mapper/repository 方法
  4. 检查最终 SQL/query 是否在 WHERE 中使用了 operator
- 输出：operator 参数在每层边界的传递状态（`preserved` / `dropped` / `transformed`）

### 模板 E：session 注入认证绕过链路

适用于：Semgrep `session-taint-*` 命中后的深度确认。

- 输入：`request.getAttribute()` 调用点或 filter 派生值
- 查询步骤：
  1. `topdef` 追溯 attribute 的来源（哪个 filter 设置的）
  2. `bottomUse` 追踪该值是否进入 `session.setAttribute()`
  3. 同时追踪该值是否进入鉴权判断分支（`if` / `equals`）
  4. 如果进入 session，追踪后续哪些 handler 从 session 读取该值
- 输出：完整污点链（filter -> attribute -> session/authz branch）

### 模板 F：MyBatis 参数绑定完整性

适用于：Semgrep `mapper-missing-operator-constraint` 命中后的交叉确认。

- 输入：service 方法及其参数列表
- 查询步骤：
  1. 枚举 service 方法的全部参数
  2. 标注每个参数的语义角色（operator/target/other）
  3. 追踪每个参数是否传递给 mapper/repository 调用
  4. 对照 mapper XML 的 `#{}` / `${}` 使用情况
- 输出：哪些 service 参数未进入最终 SQL 查询（可能的 ownership drop）

## SyntaxFlow 不可用时的 fallback（必须执行）

当出现以下任一情况：

- `SyntaxFlow MCP` 未连接
- `yak` 未安装
- `ssa_compile` 失败
- 语言暂不支持或编译结果明显不完整

则必须执行 fallback，而不是直接结束：

### fallback step 1：入口盘点

用 `rg` 枚举：

- controller / handler / route
- login / auth / signin / authenticate
- session / cookie / getAttribute / setAttribute
- mapper / repository / query / find / select / load

### fallback step 2：参数角色盘点

对候选函数标出参数角色：

- owner/operator/principal/account/tenant
- target/object/resource/id

### fallback step 3：固定检查

至少完成以下 checklist：

- 是否存在 request-derived value 写入 session
- 是否存在 cookie-derived value 参与权限判断
- 是否存在登录函数语义反转线索
- 是否存在 owner/operator 在下游 query 中丢失
- 是否存在只依赖 body/query/cookie 而不依赖 server-side auth context 的权限判断

### fallback step 4：输出声明

报告中必须显式写出：

- `ssa_available: false`
- `fallback_used: true`
- `fallback_checklist_completed: true`

若某项未完成，也要明确写出未完成原因，而不是留空。

## 输出要求

每次调用该 skill，最终至少补出：

- `ssa_available`
- `fallback_used`
- `fallback_checklist_completed`
- `confirmed_flows`
- `needs_review_flows`
- `unresolved_gaps`

每条流必须标注入口点（controller method + HTTP method + URL pattern）。当同一 sink 从多个入口点可达时，拆成多条流分别列出，每条标注各自的入口点。

## 结果分类建议

三个等级均须在输出中列出，不允许只报 Confirmed 而省略其他：

- `Confirmed`
  - 数据流或结构链已完整成立
- `Needs Review`
  - 数据流接近成立，但仍需业务语义判断
- `False Positive`
  - source 不可控、sink 不可达或已有明确防护

全量输出的目的：让审核者能看到完整审计覆盖范围，并对边界 case 做二次判断。

## 重要限制

- 不要为单一样本项目的字段名定制 SSA 查询
- 不要把“SSA 不可用”当成结束条件
- 不要只围绕已有扫描命中追踪，必须补 owner/auth/session 这三类固定主题
