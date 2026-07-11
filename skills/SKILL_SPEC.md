# SKILL.md 编写规范（sastx）

> 本文件是 `skills/` 目录下所有 SKILL.md 的编写权威规范。新增或改造 skill 前必读。
> 立项依据见飞书计划文档（内部记录），核心原则对齐 [Anthropic Claude Code Skills 官方规范](https://code.claude.com/docs/en/skills) 与 [anthropics/skills 官方 repo](https://github.com/anthropics/skills) 中的 `skill-creator/SKILL.md`。

## 一、核心写作特点（必读）

本项目 SKILL.md 与"逐项执行硬清单"流派的根本区别——**「基线 checklist + 自适应」**，所有具体风格规则都为它服务：

- **必写 checklist**：把该领域已知的检查角度沉淀成具体条目，给模型一个可落脚的起点而不是抽象口号。没有 checklist 的 SKILL.md 等于没把领域知识交付出去
- **checklist 只保基础面**：列出已知高频项，不追求穷举边界，也不限定覆盖范围。规范"已知"，不规范"全部"
- **不强制按 checklist 执行**：模型结合目标代码与上下文自由裁剪 / 补充——适用 `[x] done`、不适用 `[-] n/a (原因)`、清单外的真实发现 `[+] added (来源)`。三态标注让"动态调整"可追溯，不变成黑盒

这条特点同时约束写作者和模型：写作者不要把基线写成硬清单（会诱导凑数），模型不要把基线当 todo list 逐项打勾（会漏掉真实发现也会硬套不适用的检查）。措辞模板与三态标注协议详见 4.3 节。

## 二、目录与文件结构

```
skills/
├── <category>/                  # 一级分类：code-audit / pentest / host-defense / common / ctf / vuln-repro
│   └── <skill-name>/             # 二级目录：skill 名（kebab-case），目录名即 skill 标识
│       ├── SKILL.md              # 必需：skill 入口
│       ├── references/           # 可选：案例库、详细规则、模板
│       ├── scripts/              # 可选：辅助脚本（lint、转换、模板生成）
│       └── assets/               # 可选：静态资源（图、payload 样本）
└── common/                       # 共享口径（如 closure-verification.md），不是独立 skill
```

**二级目录结构是硬约束**：`skills/skill_extractor.go` 按 `category/skill-name/SKILL.md` 路径抽取，扁平化或三级嵌套不被识别。

## 三、Frontmatter 规范

字段命名统一 **kebab-case**（小写 + 连字符）。

### 必填字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string | skill 标识，等于目录名，kebab-case |
| `description` | string | **只写"本能力做什么"** —— 一句话客观描述能力本身（覆盖什么维度 / 产出什么 / 用什么机制）。**目标 ≤ 200 字 / 1-2 句**（上限 1536 字符，仅作硬边界）。**禁止**把 §1 触发线索（"X 时使用"、grep pattern、文件名约定枚举）写进 description，**禁止**写"归属 / 边界 / 与 Y skill 协作"等跨 skill 路由话术——这些信息在 §1 / §5 / §11 段。多行折叠用 `>-` YAML 风格 |
| `when-to-use` | string | **何时触发 / 适用信号** —— 写可被路由面直接使用的场景、入口类型、代码/流量信号；与 `description` 正交，不重复"做什么" |
| `allowed-tools` | csv | 允许调用的工具集（如 `bash,read_file,list_files,rg,list_skills`） |
| `user-invocable` | bool | 是否允许用户用 `/<name>` 直接触发；子 skill 设 `false`，父/独立 skill 通常 `true` |

> `when-to-use` 是技能表路由面字段，新增 / 改造 skill 必须填写；不要把"何时用"折叠进 `description`。

### 可选字段

| 字段 | 何时填 | 示例 |
|---|---|---|
| `argument-hint` | skill 接收 CLI 参数时 | `"[target_path]"` |
| `arguments` | 同上，列出位置参数名 | `- target_path` |
| `mcp` | 调用 MCP 工具时（如 dataflow-analysis、yak）| 见现有 mcp 类 skill |
| `disable-model-invocation` | 仅人工触发、禁止模型自动调用 | `true` |

**无参数 skill（含所有子 skill）可省略 `argument-hint` 和 `arguments`**，不要为对齐而填空字段。

### 元 skill 例外

少量「元 skill」（被所有 agent 共用、贯穿整个 ReAct 闭环的产物或基础设施，如 `common/result-with-file`）允许使用一组不同的字段：

| 字段 | 说明 |
|---|---|
| `agent: all` | 标记可被任意 agent 加载，等价于跳过用户/模型分发逻辑 |
| `context: inline` | 标记按 inline 注入而非懒加载（提示词阶段就在场） |
| `version: "x.y"` | 元 skill 通常自带版本号便于追踪格式演进 |

元 skill 可省略 `tags` / `allowed-tools` / `user-invocable`。判断准则：**只有真正被所有 agent / 所有任务无差别消费的产物层 skill 才用元 skill 形式**，新建普通 skill 时不要套用这套字段。当前唯一元 skill 是 `common/result-with-file`。

### Frontmatter 示例

```yaml
---
name: csp-audit
description: >-
  Content Security Policy 策略静态审计——分解 directive、识别 unsafe-inline / unsafe-eval / 过宽
  source-list / 缺 frame-ancestors，比对最小权限基线。
when-to-use: 当项目设置了 CSP header 或 CSP meta 标签时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
---
```

**反例**（前 v3.1 版本的常见写法，已禁止）：

```yaml
# ❌ 把触发线索堆进 description
description: >-
  Content Security Policy 策略静态审计——分解 directive、识别 unsafe-inline / unsafe-eval。
  代码里出现 `Content-Security-Policy` 响应头声明、Spring `Headers().contentSecurityPolicy()`、
  Express `helmet.csp()`、Django `CSP_*` 设置、nginx `add_header CSP` 时使用。# ← 触发枚举属于 §1

# ❌ 写跨 skill 边界
description: >-
  危险配置静态值审计……凭据/密钥归 secret-detection、HTTP 响应头归 security-header-audit。# ← 路由属于 §1 / §5
```

## 四、正文写作风格（核心）

### 4.1 反模式（yellow flags，看到要重写）

Anthropic skill-creator 原文："If you find yourself writing ALWAYS or NEVER in all caps, or using super rigid structures, that's a yellow flag — reframe and explain the reasoning."

本项目内的具体反模式：

- ❌ "**必须逐项执行**"、"**严格按下列顺序**"、"**不得跳过**"用于检查建议（用于交付契约可以，见 4.4）
- ❌ 大段 ALL CAPS 的 MUST / NEVER / ALWAYS
- ❌ 罗列 30+ 条无解释的硬规则，不告诉模型为什么这么做
- ❌ "按以下 checklist 逐项执行，确保覆盖完整。每项标注 [x] done 或 [-] n/a"——这是**检查建议**而非交付契约，硬性措辞会诱导模型在不适用场景凑数、不敢补充清单外的真实发现
- ❌ 把任务步骤当作 step 1 / step 2 / step 3 的固定流水线，丧失模型的上下文自适应能力
- ❌ 节标题或前缀写"写给 Planner / Step / Step Replan"，或正文里直接命令 phase（"Planner 不应/应/必须..."、"replan 应/不应..."、"step 应/必须..."）——SKILL.md 不规定 ReAct phase 行为，详见 4.5

### 4.2 正面要点

- ✅ 用 imperative 短句给方向，附一句"为什么"。模型懂 why 才会判断边界
- ✅ checklist 写成「基线 + 自适应」：基线规范已知项，模型可基于代码事实裁剪/补充（详见 4.3）
- ✅ 不变量优先于步骤：写「最终产物应满足 X」比「先做 A 再做 B 最后做 C」更鲁棒
- ✅ 主文件目标 < 500 行（Anthropic 官方建议）；超出走 `references/` 拆分；reference > 300 行带 TOC
- ✅ description **只写能力本身**（"做什么 / 覆盖什么 / 产出什么"），1-2 句话客观陈述，目标 ≤ 200 字。触发线索 / grep pattern / 文件名枚举属于 §1，**不**塞进 description；跨 skill 边界 / 路由话术属于 §1 / §5 / §11，**不**塞进 description

### 4.3 基线 checklist 措辞模板

所有"检查角度"类列表统一用「基线检查项」语义，**禁止**用「固定检查项」「必检项」「强制检查」「按以下 checklist 逐项执行」。

**段落标题**：`## 基线检查项`（父 skill 和子 skill 一致；子 skill 不再用 `## 检查项`）

**三态标注协议**（父/子 skill 通用）：

- 适用且已完成 → `[x] done`
- 明确不适用 → `[-] n/a (原因)`，原因要具体到代码事实（例如"项目无 CSP 配置"），不要笼统写"不适用"
- 基线未列出但实际发现的相关问题 → `[+] added (来源)`，来源指代码位置或上下文线索

不要为了凑齐基线而硬套不适用的检查；也不要因基线没列就漏掉真实发现。基线只是规范已知项，不限定覆盖边界。

**父 skill 引导语**（详写，建立期望）：

```markdown
以下是已知的常见检查角度，作为**基线起点**而非必检硬清单。结合目标代码与上下文动态调整，按上文三态标注协议处置。
```

**子 skill 引导语**（简写，承接父 skill）：

```markdown
以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。
```

### 4.4 交付契约（保留「必须」的边界）

「交付契约」与「检查建议」语义边界不同，**保留**强制措辞的场景仅限三类：

1. **产物格式 / 落库结构**：jsonl 字段、coverage-ledger 落行规则、findings 索引计数闸门
2. **安全边界**：破坏性动作的"哨兵自证 / 非破坏差分 / 停手降级"协议（见 `common/closure-verification.md`）
3. **闭环验证**：完整证据链才判 confirmed、中间信号最多 suspected、取证完整性

这三类直接关系到下游机器消费、安全合规、审计可追溯，缺失会让整条链路失效，因此为**刚性要求**。在标题旁标注「必须遵守」并紧跟一句解释 why。

### 4.5 ReAct phase 边界（不越权）

**SKILL.md 描述本能力做什么、怎么做、何时用、产出什么；不规定 plan / step / step_replan 三个 phase 自身的行为**。Phase 行为约束由对应 prompt 唯一定义（`internal/react/prompts/planning_system.prompt` / `think_act_system.prompt` 等），SKILL 越权直接导致 phase 行为被反向锁死。

判据：

| 类别 | 处置 |
|---|---|
| ✅ 本能力的方法论 / 工作流（主语是能力本身） | 写在 `## 工作流` / `## 方法` 节 |
| ✅ 本能力的产出与触发信号 | 写 "是什么"，不写 "由谁消费" |
| ✅ 本能力的去重 / 复用原则 | 描述事实约束，不写"哪个 phase 该怎么处理" |
| ❌ "写给 Planner / Step / Step Replan" 节标题或前缀 | 删除节框，正文改主语为能力 |
| ❌ "Planner / replan / step 应/不应/必须..." 句式 | 删除或改写成主语为能力的客观描述 |
| ❌ 直接规定 plan 步数 / step 拆分粒度 / replan 触发条件 | 删除——这是 prompt 的事 |

反例：
> ❌ "Planner 不应把能力索引展开成 plan steps — 初始计划只需包含侦察阶段"
> ❌ "侦察完成后由 replan 根据发现的信号展开适用的能力"

正例（同语义合规改写）：
> ✅ "本能力分两阶段：侦察先行 → 按信号展开。侦察输出 X / Y / Z，展开阶段按侦察信号匹配子能力。"

### 4.6 禁止纯路由型「父 skill」

**SKILL.md 不承担"按输入条件挑选下游 skill"的路由职责**。这类纯路由内容（"白盒/黑盒/灰盒分流"、"按用户诉求半径裁剪图谱"、"角色 + 工作流 phase 编排"）属于 **agent profile** 的职责（`internal/tui/config.go` 的 `defaultAgentFiles["<agent>.yaml"]` 的 `role` / `background` / `instruction`），不应进 SKILL.md。

判据：

| 信号 | 处置 |
|---|---|
| SKILL.md 通篇都是 `if 输入条件 then 加载 skill X` 的分诊树 | 这是 agent profile 伪装成 skill，搬到 profile |
| SKILL.md 维护一张"skillPath → 子 skill"静态索引表 | 用 `list_skills` 描述驱动发现替代，索引表至多作为 references/ 知识文件，不作为路由 |
| SKILL.md 写"我是 X 专家，我的职责是..." 的角色定义 | profile 已有 `role`，删除 |
| SKILL.md 没有自己的领域 checklist / source-sink 模型 / 检测方法论，全是协调话术 | 不构成独立 skill，迁 profile |

历史遗留的「父 skill / 路由 skill」（如曾经的 `common/graybox-p0`、`pentest/web-security-testing`、`code-audit/security-code-analysis`）按 v3 重构已淘汰，不允许新增同类形态。**所有 SKILL.md 都应是"领域原子能力"** —— 能被任意 agent 独立 `load_skill` 调用、有自己的领域知识沉淀、不依赖某个特定 agent 编排背景才能读懂。

## 五、章节骨架建议（仅限 common/ 共享文档与 vuln-repro 类）

> **原子 skill 不适用本节** —— 所有 `<category>/<skill>/SKILL.md` 都走 §6 的 12 段强制骨架。本节只用于 `common/` 下的共享文档（如 `closure-verification.md` / `web-vuln-cause-map.md`）和 `vuln-repro/` 等非"漏洞检测能力"形态。

下列章节是项目现有共性的高频结构，**按需选用**：

- `## 目标` — 这个文档解决什么问题，**不要复述 description**
- `## 适用信号` — 出现哪些代码模式或上下文时加载本文档
- `## 基线检查项` — 见 4.3
- `## 闭环验证要求（必须遵守）` — 见 4.4 与 `common/closure-verification.md`
- `## 结论口径` — 判定语义、jsonl 字段、按入口点 / 按 (source, sink) 组织等
- `## 发现即落行` — append-only jsonl 规则
- `## 框架模式库` — 不同框架（Spring / Django / Gin 等）的鉴权模式与缺口

## 六、原子 skill 标准骨架（v3.1 — 黑/白盒分骨架）

v3 重构后，**所有 SKILL.md 都是原子 skill**（不再有父 / 子 / 路由分层）。每个原子 skill 必须满足**独立可读 + 独立可触发**——任意 agent 不依赖任何外部 SKILL.md 上下文，仅靠 `list_skills` 描述命中本 skill，并能从本 SKILL.md 单文件读懂完整的检测能力。

v3.1（2026-06-13）按检测方式拆为两套 12 段骨架：

- **黑盒骨架** — 适用 `pentest/` 下所有 skill。基于 HAR / 流量 / 响应观察 / payload 探测。
- **白盒骨架** — 适用 `code-audit/` 下所有 skill。基于代码 pattern / 数据流 / 项目结构。

12 段顺序相同，**5 段共享 + 7 段差异**——§2 / §4 / §7 / §10 / §12 共享语义；§1 / §3 / §5 / §6 / §8 / §9 / §11 按检测方式差异化。**每段允许标 `本能力 n/a (原因)` 省略，但不允许颠倒顺序**。

### 6.1 黑盒原子 skill 12 段骨架（`pentest/`）

| # | 章节标题 | 内容定义 | 是否必填 |
|---|---|---|---|
| 1 | `## 触发线索 / 适用信号` | 出现哪些**流量 / HAR / 响应特征**时本 skill 命中：**漏洞-specific 的响应特征**（错误关键字 / 延时 / 跳转 / 内容差异），可附入口类型粗筛示例。**不**枚举参数位置（query / body / header / cookie / path 是 HTTP 通识，agent 从 HAR 推导，本节不重复）。按 sink 语义分类，不按业务命名。**这一节决定 `list_skills` 能否命中**——和 frontmatter `description` 同步 | 必填 |
| 2 | `## 造成原因` | **共享**。本漏洞的成因核心，独立可读、不引用任何其他 SKILL.md。**禁止**写"详见 X skill 漏洞图谱" | 必填 |
| 3 | `## 响应信号映射` | 列出本漏洞专属的**响应观察通道集合**（observation-channel）——攻击效果可被黑盒观察的侧信道（响应 body / status / 耗时 / 错误信息 / 带外通道等本漏洞特有形态）。**不**枚举输入位置（HTTP 通识，agent 从 HAR 推导） | 必填 |
| 4 | `## 常见类型` | **共享**。本漏洞的主流攻击变体（如 SQLi 的 boolean / time / error / UNION；XSS 的 reflected / stored / DOM）。按已知主流覆盖，不追求穷举 | 必填 |
| 5 | `## 侦察输入` | 如何在 **HAR / recon 端点账本 / 业务场景**里找候选——按入口类型 + 参数位置粗筛、按业务场景按惯例位置补充。**列出的业务场景仅作类似场景示例 不限于此；以 page-ledger 实际输出为准**。不写"项目结构 / Mapper / DAO"（属白盒视角） | 必填 |
| 6 | `## 框架 / DBMS 响应指纹` | 通过响应（错误关键字 / header / 行为）推断后端框架 / DBMS / 中间件。例：`"You have an error in your SQL syntax"` → MySQL；`Set-Cookie: PHPSESSID` → PHP；`X-Powered-By: Express` → Node.js。**响应指纹**用于优化 payload 选择 | 强烈建议 |
| 7 | `## 思考检查点` | **共享**。3-5 条引导按 sink 语义思考的问题（按 sink 语义、不按业务命名）——本漏洞专属展开 | 必填 |
| 8 | `## 检测方法论 / 决策树` | **黑盒决策树**：Step 0 基线采集 → Step 1 闭合 / 上下文探测 → Step 2 指纹 → Step 3 策略选择 → Step 4 防误报确认。含**全局请求预算**（每参数最多 N 次、并发上限、延时阈值）+ payload 范式 + **编码绕过库**（payload-heavy 漏洞必备） | 必填 |
| 9 | `## 闭环要求（必须遵守）` | 引用 `common/closure-verification.md`（不复述契约），写**本漏洞特有的可观测效果证据**：命令回显形态 / 带外回连形态 / 数据回读形态 / 稳定二态差异 + confirmed / suspected / not_vulnerable 判据。**「判定标准」合并到本节** | 必填 |
| 10 | `## 具象化反例库` | **共享**。「抽象规则 → 具体场景 → 识别特征 → 排除/确认」四步模板。FP / FN / 易混淆 | 必填 |
| 11 | `## 测试安全边界` | 引用 `common/closure-verification.md` 破坏性动作章节，**只写本漏洞特有**破坏性风险（如 SQLi 的 DROP/DELETE、文件上传的 webshell 落地）+ 哨兵自证 / 非破坏差分 / 带外通道等本漏洞可用的非破坏验证手段 | 必填 |
| 12 | `## 修复建议` | **共享**。"源头治理 / 边界过滤 / 兜底拒绝"分层；引用主流框架安全 API | 必填 |

### 6.2 白盒原子 skill 12 段骨架（`code-audit/`）

| # | 章节标题 | 内容定义 | 是否必填 |
|---|---|---|---|
| 1 | `## 触发线索 / 适用信号` | 出现哪些**代码 pattern / 路由声明 / 项目结构特征**时本 skill 命中：grep 命中模式（如 `fmt.Sprintf` + SQL）、文件名约定（`*Mapper.xml` / `*DAO.java`）、中间件 / 注解（`@Query` / `@Transactional`）。按 sink 语义分类。**这一节决定 `list_skills` 能否命中**——和 frontmatter `description` 同步 | 必填 |
| 2 | `## 造成原因` | **共享**。同 6.1 §2 | 必填 |
| 3 | `## 领域 source-sink 数据流模型` | 代码层 source / sink 集合 + 数据流追踪规则。**只列本漏洞专属的 source-func / sink-func 集合**——把"用户可控输入流到危险 sink"的 source-sink 框架在白盒视角下具象化 | 必填 |
| 4 | `## 常见类型` | **共享**。同 6.1 §4 | 必填 |
| 5 | `## 入口点定位` | 如何在**项目结构**里找本类漏洞的 source / sink：路由声明文件、Controller / Mapper / DAO / Repository 命名约定、依赖 (`pom.xml` / `package.json` / `go.mod`) 里的危险库。**列出的框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准** | 必填 |
| 6 | `## 跨框架代码变体` | Spring / Django / Gin / Express / Express+Sequelize 等的代码 pattern 差异表。**"安全形态 vs 危险形态"对照**，含 ORM Raw 通道。这是白盒原子 skill 的复利资产，决定它跨语言跨框架可用 | 强烈建议 |
| 7 | `## 思考检查点` | **共享**。同 §6.1 §7 | 必填 |
| 8 | `## 检测方法论 / 数据流追踪` | **白盒方法论**：找 source → 跨函数追数据流 → 到 sink → 判可达性 / 参数化绑定 / 白名单存在性。含**反编译 / 依赖追踪**约定（闭源库怎么处理）+ 静态分析工具调用（sast-scan / dataflow-analysis） | 必填 |
| 9 | `## 闭环要求（必须遵守）` | 引用 `common/closure-verification.md`，**强调白盒判定上限**：本能力只能到 `static-confirmed`（数据流可达性已证），不等于动态 confirmed；要升级到 confirmed 必须靠黑盒端验证或 graybox 流程。**「判定标准」合并到本节** | 必填 |
| 10 | `## 具象化反例库` | **共享**。同 6.1 §10。白盒 FP 高发点：被忽视的中间过滤层、ORM 自动转义、框架自带白名单等 | 必填 |
| 11 | `## 静态分析边界` | 数据流分析的能力边界：反射调用 / 闭源依赖（无源码可达性追踪）/ 动态字符串构造 / 配置驱动的运行时行为——这些情形必须标注 `static-unknown`，不能默认为 not_vulnerable。**白盒底线 = 不假装看到看不到的代码** | 必填 |
| 12 | `## 修复建议` | **共享**。同 6.1 §12 | 必填 |

### 6.3 共享章节说明

§2 / §4 / §7 / §10 / §12 共 5 段在两套骨架中**语义完全一致**，下面给一次性定义，原子 skill 写作时不需要在黑/白盒版本之间分裂内容：

- **§2 造成原因**：漏洞成因本身与检测方式无关。一段话讲清"什么样的 source 流到什么样的 sink 就构成本漏洞 + 为什么"。
- **§4 常见类型**：攻击变体（如 SQLi 的 boolean / time / error / UNION）是漏洞自身的属性，黑白盒都需要知道。
- **§7 思考检查点**：3-5 条引导按 sink 语义（而非业务命名）思考的问题——本漏洞专属展开。
- **§10 具象化反例库**：「抽象规则 → 具体场景 → 关键识别特征 → 如何排除/确认」四步模板。**黑盒 FP**：响应突变来源识别（WAF / 网络抖动 / cache）；**白盒 FP**：ORM 自动转义、中间过滤、框架默认白名单。两侧反例都收，不强行拆分。
- **§12 修复建议**：按"源头治理 / 边界过滤 / 兜底拒绝"分层。修复方法对黑白盒读者都相同。

### 6.4 与 common/ 共享层的引用规范

`common/closure-verification.md` 闭环契约 / 破坏性动作协议、`common/web-vuln-cause-map.md` 漏洞成因索引、`common/result-with-file` 产物规范——**原子 skill 不复述、只引用**。引用措辞：

- "本能力的 X 遵循 `common/<file>` 的 Y 节"
- 或在 §9 / §11 段开头写一句 `> 闭环判定 / 安全边界 / 取证完整性以 common/closure-verification.md 为准，下面只列本漏洞特有的可观测信号。`

**禁止**在原子 skill 里抄一份 closure-verification 的状态机正文，否则违背 progressive disclosure（被加载者已经在场，重复污染上下文）。

### 6.5 骨架模板与示范

- 黑盒：`skills/PENTEST_SKILL_TEMPLATE.md`（黑盒 12 段 + SQLi 风格示范填法）
- 白盒：`skills/CODE_AUDIT_SKILL_TEMPLATE.md`（白盒 12 段 + SAST/dataflow 风格示范填法）

新增 / 改造 `pentest/<name>/SKILL.md` 复制黑盒模板；新增 / 改造 `code-audit/<name>/SKILL.md` 复制白盒模板。**禁止跨方向复制**——黑盒 skill 不写 source-sink 数据流模型，白盒 skill 不写响应指纹。

## 七、关于历史「父 skill / 路由 skill」（已淘汰）

v2 及更早版本曾用过"父 skill 分发 + 子 skill 执行"的两层结构（典型如 `common/graybox-p0`、`pentest/web-security-testing`、`code-audit/security-code-analysis`），父 skill 承担"角色 + 工作流 + 漏洞成因图谱 + skillPath 索引"。v3 重构判定该形态**违反 progressive disclosure**（路由属于 agent profile，不属于 SKILL.md），已**整体淘汰**：

- 父 skill 的「角色 + 工作流 + 范围判断」→ 迁到 `internal/tui/config.go` `defaultAgentFiles["<agent>.yaml"]` profile
- 父 skill 的「漏洞成因图谱 / 能力索引表」→ 迁到 `skills/common/` 下作为领域知识 reference 文件（如 `web-vuln-cause-map.md`），由 agent profile 按需 `read_file` 加载
- 父 skill 的「闭环契约复述」→ 删除，agent profile + 原子 skill 都直接引用 `common/closure-verification.md`
- 原子 skill 之间的「成因引用 → 父 skill」依赖 → 切断，每个原子 skill 本地写完整的「§2 造成原因」

**禁止新增任何"按输入条件分诊下游 skill"的 SKILL.md**——这类需求一律落 agent profile。

## 八、轻量校验脚本

`scripts/skill-lint.sh`（不在本次范围，预留位置）。最小实现：

```bash
#!/usr/bin/env bash
# 反模式 grep 自检
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
fail=0

for pat in "固定检查项" "按以下 checklist 逐项执行" "确保覆盖完整"; do
  hits=$(grep -rln "$pat" "$ROOT" --include="SKILL.md" || true)
  if [ -n "$hits" ]; then
    echo "❌ 反模式「$pat」命中："
    echo "$hits"
    fail=1
  fi
done

[ "$fail" -eq 0 ] && echo "✅ skill-lint pass"
exit $fail
```

后续提交独立 PR 接入 CI。

v3 新增反模式 grep 项（待加入 lint）：

```bash
# 父 skill / 路由 skill 残留检测
for pat in "你是.*专家" "范围 = 用户诉求" "漏洞成因图谱" "skillPath" "load_skill 调度" "按以下 checklist 逐项执行"; do
  # 命中即提示该 SKILL.md 是否还是 v2 父 skill 残留
done
```

## 九、附录：与 Anthropic 官方规范的差异

| 项 | Anthropic 官方 | 本项目 | 理由 |
|---|---|---|---|
| frontmatter 必填字段 | name / description（或 description + when-to-use）/ 可选 allowed-tools | name / description / when-to-use / allowed-tools / user-invocable 共 5 字段 | aster 额外需要独立 when-to-use 路由面、allowed-tools 系统级权限管控、user-invocable 分发机制；其他字段对齐 Anthropic |
| 主文件行数上限 | 建议 < 500 | 同 | 直接采纳 |
| description 字符上限 | description + when_to_use 合计 1536 | description 与 when-to-use 分列维护，合计不超过运行时上限 | 直接采纳上限，同时保留独立路由触发面 |
| 编号步骤 vs 指南 | 任务型 skill 用编号步骤，知识型用"指南 + why" | 同 | 直接采纳 |
| 三态标注 | 官方未规定 | `[x] done` / `[-] n/a (原因)` / `[+] added (来源)` | 项目特有"基线 + 上下文自适应"的可追溯落地形式 |
| 父子 skill 分层 | Anthropic 鼓励 progressive disclosure 但未强制分层 | v3 重构淘汰父 skill / 路由 skill，所有 SKILL.md 都是原子能力 | 父 skill 实质是 agent profile，分层违背 progressive disclosure |
| 原子 skill 12 段骨架 | 未规定 | §6 强制 12 段顺序，v3.1 按黑/白盒拆两套 | 让原子 skill 独立可读 + 独立可触发；黑白盒检测方式差异显著，强行共用骨架会让两侧都写不到位 |

## 十、改造检查清单（提交前自检）

新增或修改 SKILL.md 时，发起 PR 前过一遍：

**通用项**：
- [ ] frontmatter 5 必填字段齐全（name / description / when-to-use / allowed-tools / user-invocable），全 kebab-case
- [ ] description + when-to-use 合计 ≤ 1536 字符（中文按字数估算）；description 写"做什么"，when-to-use 写"何时用"
- [ ] 主文件 < 500 行；超长内容拆到 `references/`
- [ ] 无「固定检查项」「必检项」「按以下 checklist 逐项执行」「确保覆盖完整」字样
- [ ] 含 checklist 的章节使用三态标注（`[x]` / `[-]` / `[+]`）说明
- [ ] 「必须遵守」仅用于交付契约（产物格式 / 安全边界 / 闭环验证），紧跟 why 解释
- [ ] 无参数 skill 不强填 `argument-hint` / `arguments`
- [ ] 引用其他 skill 用相对路径 `[name](../other-skill/SKILL.md)`，不用绝对路径

**v3.1 原子 skill 骨架（§6）通用项**：
- [ ] 12 段标题齐全且顺序正确；缺段需在该位置明文写"本能力 n/a (原因)"
- [ ] §2 造成原因独立可读，无"详见 X skill 漏洞图谱"之类外部依赖
- [ ] §9 闭环要求只列本漏洞特有可观测信号，引用而非复述 closure-verification
- [ ] §10 反例库按「抽象规则 → 具体场景 → 识别特征 → 排除/确认」四步模板
- [ ] 不含 v2 路由型残留：无"我是 X 专家""范围 = 用户诉求"角色定义、无 skillPath 索引表、无 `load_skill` 子方向调度

**`pentest/`（黑盒）特有项**：
- [ ] §1 触发线索按流量 / HAR / 响应特征分类，不写"代码 pattern / 项目结构"
- [ ] §3 是「响应信号映射」（输入位置 → 响应观察点），不是「source-sink 数据流模型」
- [ ] §5 是「侦察输入」（HAR / 端点账本 / 业务场景），不写「项目结构 / Mapper / DAO」
- [ ] §6 是「框架 / DBMS 响应指纹」（从响应推断后端），不是「跨框架代码 pattern 表」
- [ ] §8 黑盒决策树形态：Step 0/1/2/3/4 + 请求预算 + payload 范式 + 编码绕过
- [ ] §11 写黑盒破坏性动作（DROP / DELETE / 写盘）+ 非破坏验证手段

**`code-audit/`（白盒）特有项**：
- [ ] §1 触发线索按代码 pattern / 路由声明 / 项目结构分类，不写"流量 / HAR"
- [ ] §3 是「source-sink 数据流模型」（代码层 source/sink 集合），不是「响应信号映射」
- [ ] §5 是「入口点定位」（项目结构 / Mapper / DAO），不写「HAR / 端点账本」
- [ ] §6 是「跨框架代码变体表」（安全 vs 危险形态对照），不是「响应指纹」
- [ ] §8 白盒数据流追踪形态：找 source → 跨函数追到 sink → 判可达性 / 参数化绑定
- [ ] §9 上限只到 `static-confirmed`，明确说明动态升级路径
- [ ] §11 是「静态分析边界」（反射 / 闭源 / 动态构造的 unknown 处置），不是「破坏性动作」

## 十一、相关文件

- `common/closure-verification.md` — 闭环验证 / 破坏性动作 / 取证完整性共享口径
- `common/result-with-file` — 元 skill：产物落库规范
- `common/web-vuln-cause-map.md` — 漏洞成因图谱（领域参考，agent profile 按需读）
- `skills/PENTEST_SKILL_TEMPLATE.md` — 黑盒原子 skill 模板（v3.1）
- `skills/CODE_AUDIT_SKILL_TEMPLATE.md` — 白盒原子 skill 模板（v3.1）
- `skills/skill_extractor.go` — skill 抽取逻辑，定义目录结构硬约束
- `internal/tui/config.go` `defaultAgentFiles["<agent>.yaml"]` — agent profile 层（承载 role / background / instruction / skill_names）
