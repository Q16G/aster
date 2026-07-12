# Agent Profile 编写规范

> 适用范围：所有走 ReAct 调度（5 个 phase prompt）的 agent。
>
> 三要素：`role` / `background` / `instruction`。语义性质不同，渲染位置不同；混写会让模型把指令当身份、把环境当背景，影响阶段行为稳定性。

---

## 1. 三要素定义

| 字段 | 性质 | 一句话判据 | 渲染位置 |
|---|---|---|---|
| `role` | 身份/职业定位 | "**是什么人**" | 各 phase prompt 顶部 `# Role` 段（占位） |
| `background` | 背景知识/熟悉度 | "**懂什么 / 见过什么**" | 各 phase prompt 顶部 `# Background` 段（占位） |
| `instruction` | 任务级纪律/交付约定 | "**按什么规矩干**" | 仅任务级 phase 顶部 `# Instruction` 段（占位） |

**instruction 按 phase 分层渲染**：它是任务级纪律（规划范围/交付口径/任务域），只在 4 个任务级 phase（planner / step_replan / final_answer / intent_classification）渲染；执行级 phase（think_act / sub_agent）的职责半径由 role/background 界定，不渲染 instruction——执行级注入全局任务纪律会诱导其越过 step/委派边界追全局目标。

env / repo / task_context 是**运行时上下文**，不属于 agent profile，留在 identity_env 的 `<env>` 块，作者不应在 profile 里手写这些。

### 1.1 role

写"职业身份 + 专业域"。一句话画出**这是个什么角色**，限定其判断的语义维度。

- ✅ "资深 Web 应用安全审计专家，专长后端鉴权与会话管理"
- ✅ "Go 后端架构师，主要审视高并发场景下的资源与同步问题"
- ❌ "你必须遵循所有规则并输出 JSON" —— 这是 instruction，不是 role
- ❌ "请在 `/tmp/x.md` 写报告" —— 这是 env / 任务参数，不是 role

### 1.2 background

写"**这位角色懂什么、做过什么、对什么有偏好**"。用于让模型把 phase 规则按这位角色的常识去解读。

- ✅ "10 年 Web 渗透经验，熟悉 OWASP Top 10、常见后端框架的 RCE/SSRF 路径"
- ✅ "曾主导多个金融系统的合规审计，对越权与数据脱敏边界敏感"
- ❌ "本次任务要审计 `/repo/foo`" —— 这是 task_context，不是 background
- ❌ "如果发现高危必须立即上报" —— 这是 instruction

### 1.3 instruction

写"**按什么规矩干 / 偏好 / 强约定**"。是任务级的指令性约束（规划范围、交付口径、任务域），只在任务级 phase 顶部以 `# Instruction` 段渲染。

- ✅ "只输出结构化结果，不写散文式总结；所有发现必须带证据路径"
- ✅ "优先调用现有 skill，不要自造工具流程"
- ❌ "你是安全审计专家" —— 已经在 role 里写过，不要重复
- ❌ "项目路径在 …" —— 这是 task_context

---

## 2. 反例对照

| 错误写法 | 误把…当成… | 正确归位 |
|---|---|---|
| `role: "审计专家，必须遵守 X、Y、Z 规则"` | instruction 写进 role | 拆：role 留身份，规则进 instruction |
| `background: "目标项目位于 /repo/foo，使用 Gin 框架"` | task_context 写进 background | 留 background 的通用领域知识；项目细节由 task_context.md 承载 |
| `instruction: "你是安全审计专家"` | role 重复进 instruction | 仅在 role 写一次 |
| `role: "审计专家。请使用中文。报告格式为 markdown"` | 把多种偏好压进 role | 偏好/约定全部进 instruction |
| `background: "请记得查看 /repo/foo/config.yaml"` | env/runtime 误入 background | 路径与运行时数据走 task_context，不进 background |

---

## 3. 渲染位置图

### 3.1 phase prompt 顶部（共通结构）

```text
# 当前阶段
<stage_word>（短描述）：<阶段职责>……
{{- if .HAS_AGENT_ROLE}}

# Role
{{.AGENT_ROLE}}
{{- end}}
{{- if .HAS_AGENT_BACKGROUND}}

# Background
{{.AGENT_BACKGROUND}}
{{- end}}
{{- if .HAS_AGENT_INSTRUCTION}}

# Instruction
{{.AGENT_INSTRUCTION}}
{{- end}}

# Goals
…（phase 自有正文）
```

`# Instruction` 段仅任务级 phase（planner / step_replan / final_answer / intent_classification）有；执行级 phase（think_act / sub_agent）顶部只有 Role / Background 两段。

阶段词与 prompt 文件一一对应：

| Prompt 文件 | 阶段词 |
|---|---|
| `planning_system.prompt` | `planner` |
| `think_act_system.prompt` | `step` |
| `step_replan_system.prompt` | `step_replan` |
| `final_answer_system.prompt` | `final_answer` |
| `intent_classification_system.prompt` | `intent_classification` |

### 3.2 identity_env 块（agent 启动一次，跨 phase 缓存复用）

```text
<env>
workspace 路径: …
共享工作区: …
（仓库上下文 / 任务上下文）
</env>
```

纯 `<env>` 块：身份三要素均已下沉至各 phase prompt 顶部，本块只承载运行时上下文。

---

## 4. 模板占位变量清单

5 个 phase system prompt 模板中可用：

| 变量 | 含义 | 渲染时机 |
|---|---|---|
| `{{.AGENT_ROLE}}` | 经 trim 的 role | `HAS_AGENT_ROLE=true` 时 |
| `{{.AGENT_BACKGROUND}}` | 经 trim 的 background | `HAS_AGENT_BACKGROUND=true` 时 |
| `{{.HAS_AGENT_ROLE}}` | bool | 条件包住 `# Role` 段 |
| `{{.HAS_AGENT_BACKGROUND}}` | bool | 条件包住 `# Background` 段 |
| `{{.AGENT_INSTRUCTION}}` | 经 trim 的 instruction | 仅 4 个任务级 phase 模板可用 |
| `{{.HAS_AGENT_INSTRUCTION}}` | bool | 条件包住 `# Instruction` 段（仅任务级） |

`agent_identity_env.prompt` 模板中可用：

| 变量 | 含义 |
|---|---|
| `<env>` 相关：`WORKSPACE_ROOT_DIR` / `WORKSPACE_SHARED_DIR` / 仓库上下文 / 任务上下文 | … |

注入入口：

| Phase | 注入点 |
|---|---|
| think_act | `agent_prompt.go` `BuildThinkActPrompt` |
| step_replan / final_answer | `phase_prompt_build.go` |
| task_planner | `runtime_scheduler.go` `plannerInput := TaskPlannerPromptInput{…}` |
| intent_classification | `phase_intent_classification.go` `runIntentClassificationPhase` |
| identity_env | `agent_prompt.go` `identityEnvBlock()` |

---

## 5. 修改 prompt 模板的边界

- **「# 当前阶段」段不可改、不可省**，与 phase identifier（planner / step / step_replan / final_answer / intent_classification）一一对应。这是模型识别当前 phase 的锚点。
- **`# Role` / `# Background` 段只放占位，不写硬编码身份**。硬编码会让 phase prompt 与 agent profile 解耦失败。
- **identity_env 块不得渲染任何身份闭合块**（`<AGENT_ROLE>` / `<AGENT_BACKGROUND>` / `<AGENT_INSTRUCTION>`），三要素均已下沉至各 phase 顶部；如发现历史代码仍在 identity_env 渲染，按本 README 调整。
- **instruction 分层不靠字段有无，靠渲染层**：`AgentProfile` 三字段在全部 phase Input 上都存在（call site 统一补齐），但只有任务级 phase 的 Build 方法映射 `AGENT_INSTRUCTION` key、只有任务级模板引用它。给执行级 phase（think_act / sub_agent）的 Build 或模板加 instruction 渲染即破坏分层（`TestAgentInstruction_PhaseGates` 会拦截）。
- **缓存稳定性**：`AGENT_ROLE` / `AGENT_BACKGROUND` / `AGENT_INSTRUCTION` 在单次 agent run 内必须为常量（来自 `a.cfg`），不要在运行时动态改写——会破坏 phase prompt 的字节级稳定，导致缓存命中率下降。
- 新增 phase 时，按本 README 结构在顶部加段（当前阶段 + 条件 # Role + 条件 # Background；任务级 phase 再加条件 # Instruction），并在对应 `BuildXxxPrompt` 函数的 systemData map 注入对应 key（role/background 4 个；任务级再加 `AGENT_INSTRUCTION` / `HAS_AGENT_INSTRUCTION`）。新 phase 属任务级还是执行级，以「消费任务级纪律（规划/交付/任务域）还是在既定边界内执行」判定。
