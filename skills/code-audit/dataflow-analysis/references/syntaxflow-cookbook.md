# SyntaxFlow Cookbook（dataflow-analysis 工具配套）

> 本文件是 `code-audit/dataflow-analysis/SKILL.md` 的扩展参考——SyntaxFlow DSL 完整语法、查询模板库、fallback 协议、Java Web 固定主题。
> 仅在 SKILL.md §8 触发"需要具体查询语法 / 完整模板"时按需读，不进 frontmatter 默认上下文。

## 目录

- [1. SyntaxFlow DSL 完整语法](#1-syntaxflow-dsl-完整语法)
- [2. TopDef Cookbook（复制即用）](#2-topdef-cookbook复制即用)
- [3. 通用查询模板（按追踪目标分类）](#3-通用查询模板按追踪目标分类)
- [4. Java Web 固定查询主题](#4-java-web-固定查询主题)
- [5. SSA 不可用时的 fallback 协议](#5-ssa-不可用时的-fallback-协议)
- [6. 输出字段规范](#6-输出字段规范)

---

## 1. SyntaxFlow DSL 完整语法

### 1.1 运算符速查

**TopDef（向上追溯定义链）**：

- `#>`：追踪到**直接定义点**（one-hop def）
- `#->`：追踪到**定义链起点**（递归 topdef）

**BottomUse（向下追踪使用链）**：

- `->`：追踪到**下一个使用点**（one-hop use）
- `-->`：追踪到**使用链结束**（递归 bottomuse）

### 1.2 限深控爆炸

首次调试默认加限深：

- TopDef 限深：`$x #{depth: 5}-> $y;`
- BottomUse 限深：`$x -{depth: 3}-> $y;`

why：大项目 SSA 图节点数可能上万；不限深的递归追踪会爆炸式匹配且不可读。从小 depth 起步（3~5），按需放宽。

### 1.3 Alert 输出规范（硬要求）

- **每条规则必须至少包含 1 个 `alert`**——否则视为"无可见输出"
- `check` 可选：用于在锚点未命中时输出提示，不是硬要求
- 推荐写法：`alert $var for { message: "...", level: info }`（`message/msg/content` 字段兼容识别）

### 1.4 查询泛化的取舍（why-based）

写规则时常遇"宽 vs 窄"取舍：

- **过宽**（如 `*.* #-> ...` / 仅 `*` 作 anchor 不带任何收敛）：召回大量噪声、SSA 图爆炸、不可读输出
- **过窄**（anchor 只锚单一调用形态）：漏掉项目自有 wrapper、漏掉等价 sink、漏掉注解处理器生成代码

**why**：太宽噪声压住真信号，太窄漏报放过真洞。按上下文调整——首次粗查询用收敛 anchor + depth 限制，命中后看是否需要扩展 anchor 集合。

收敛方式（至少满足其一）：

- anchor 包含**明确的方法 / 调用链片段**（如 `Runtime.getRuntime().exec` / `Files.write` / `.parse` / `.evaluate`）
- 或对追踪加 `depth`（首次调试建议 `depth: 3~8`）
- 或使用条件过滤收敛（仅在明确知道语义时使用）

### 1.5 写规则固定套路

每次写 `rule_text` 都按以下套路组织——这是"维度递进"不是"步骤编号"：

1. **锚定（Anchor）**：先选具体调用点或方法链作 anchor
2. **捕获（Capture）**：用 `* as $arg` 把关心的参数 / 值抓出来
3. **追踪（Trace）**：默认用 `#->`（必要时先 `#>` 粗定位），首次调试加 `depth`
4. **输出（Alert）**：对最终要看的变量 `alert`

---

## 2. TopDef Cookbook（复制即用）

> 约定：以下片段中的 `<ANCHOR_CALL>` 是占位符，替换为具体的调用链或方法名。

### 片段 A：先抓参数，再 TopDef（推荐默认）

```sf
<ANCHOR_CALL>(* as $arg) as $call;
$arg #-> * as $topdef;
alert $topdef for { message: "TopDef($arg) from <ANCHOR_CALL>", level: info };
```

### 片段 B：在调用点内联 TopDef（更短）

```sf
<ANCHOR_CALL>(* #-> * as $source) as $call;
alert $source for { message: "TopDef(inline) from <ANCHOR_CALL>", level: info };
```

### 片段 C：只追一层（快速定位直接来源）

```sf
<ANCHOR_CALL>(* as $arg) as $call;
$arg #> * as $direct_def;
alert $direct_def for { message: "DirectDef($arg) from <ANCHOR_CALL>", level: info };
```

### 片段 D：TopDef 限深（先小后大，防爆炸）

```sf
<ANCHOR_CALL>(* as $arg) as $call;
$arg #{depth: 5}-> * as $bounded_topdef;
alert $bounded_topdef for { message: "TopDef depth=5 from <ANCHOR_CALL>", level: info };
```

### 片段 E：TopDef + BottomUse（同时回答"从哪来 / 到哪去"）

```sf
<ANCHOR_CALL>(* as $arg) as $call;
$arg #-> * as $topdef;
$arg -{depth: 3}-> * as $bottomuse;
alert $topdef for { message: "TopDef from <ANCHOR_CALL>", level: info };
alert $bottomuse for { message: "BottomUse depth=3 from <ANCHOR_CALL>", level: info };
```

### 片段 F：从一个变量开始追（已定位到 `$x`）

```sf
$x #-> * as $topdef;
alert $topdef for { message: "TopDef($x)", level: info };
```

---

## 3. 通用查询模板（按追踪目标分类）

不要围绕单个项目字段名写查询；优先从以下通用模板出发。

### 模板 A：request-derived value → session

适用于：

- `request.getParameter` / `getHeader` / `getCookies` / `getReader`
- `request.getAttribute(...)`
- filter / decoder / middleware 派生值

查询目标：

- 是否流向 `getSession().setAttribute(...)`
- 是否经由 `HttpSession session = ...` 的别名 sink

### 模板 B：cookie-derived value → branch / auth decision

查询目标：

- cookie 值是否参与 `if/else`、权限比对、身份相等判断
- cookie 值之后是否进入管理操作、敏感 service 调用、对象创建 / 删除 / 更新

### 模板 C：owner / operator → service → mapper / query

查询目标：

- 方法是否同时接收操作者身份参数和目标对象参数
- service / mapper 调用是否丢弃 owner / operator 约束
- 最终 query 是否只保留 target / resource ID

### 模板 D：跨层 ownership 分析

适用于：Semgrep `idor-ownership-drop` 命中后的深度确认。

- 输入：controller 方法签名（含 operator 和 target 参数）
- 查询动作：
  - 从 controller 方法入口开始 `BottomUse` 追踪 operator 参数
  - 检查 operator 是否传递给 service 层方法
  - 检查 service 是否继续传递给 mapper / repository 方法
  - 检查最终 SQL / query 是否在 WHERE 中使用了 operator
- 输出：operator 参数在每层边界的传递状态（`preserved` / `dropped` / `transformed`）

### 模板 E：session 注入认证绕过链路

适用于：Semgrep `session-taint-*` 命中后的深度确认。

- 输入：`request.getAttribute()` 调用点或 filter 派生值
- 查询动作：
  - `TopDef` 追溯 attribute 的来源（哪个 filter 设置的）
  - `BottomUse` 追踪该值是否进入 `session.setAttribute()`
  - 同时追踪该值是否进入鉴权判断分支（`if` / `equals`）
  - 如果进入 session，追踪后续哪些 handler 从 session 读取该值
- 输出：完整污点链（filter → attribute → session / authz branch）

### 模板 F：MyBatis 参数绑定完整性（XML + SSA 联合）

适用于：Semgrep `mapper-missing-operator-constraint` 命中后的交叉确认。

XML 模板（`*Mapper.xml` 里的 `${}` / `#{}`）不在 SSA IR 内，需 grep XML + SSA 联合：

- 输入：service 方法及其参数列表
- 查询动作：
  - 枚举 service 方法的全部参数（SSA）
  - 标注每个参数的语义角色（operator / target / other）
  - 追踪每个参数是否传递给 mapper / repository 调用（SSA）
  - 对照 mapper XML 的 `#{}` / `${}` 使用情况（rg 文本扫描）
- 输出：哪些 service 参数未进入最终 SQL 查询（可能的 ownership drop）

---

## 4. Java Web 固定查询主题

对于 Java Web 项目，至少覆盖以下五类主题。优先保证前三类：

1. `request` / `request attribute` → `session`
2. `Cookie` → branch / auth decision
3. `controller(owner, target)` → `service(owner, target)` → `mapper(target)`
4. `request.getAttribute()` → permission `if` branch（filter 解密数据入权限判断）
5. `service method params` → `mapper XML #{}/${}` 绑定完整性

---

## 5. SSA 不可用时的 fallback 协议

当出现以下任一情况：

- `SyntaxFlow MCP` 未连接
- `yak` 未安装
- `ssa_compile` 失败
- 语言暂不支持或编译结果明显不完整

必须执行 fallback，而不是直接结束。

### fallback 维度 1：入口盘点

用 `rg` 枚举：

- controller / handler / route
- login / auth / signin / authenticate
- session / cookie / getAttribute / setAttribute
- mapper / repository / query / find / select / load

### fallback 维度 2：参数角色盘点

对候选函数标出参数角色：

- owner / operator / principal / account / tenant
- target / object / resource / id

### fallback 维度 3：基线检查项（按三态标注 `[x]` / `[-]` / `[+]`）

至少完成：

- [ ] 是否存在 request-derived value 写入 session
- [ ] 是否存在 cookie-derived value 参与权限判断
- [ ] 是否存在登录函数语义反转线索
- [ ] 是否存在 owner / operator 在下游 query 中丢失
- [ ] 是否存在只依赖 body / query / cookie 而不依赖 server-side auth context 的权限判断

### fallback 维度 4：输出声明

报告中必须显式写出：

- `ssa_available: false`
- `fallback_used: true`
- `fallback_checklist_completed: true`

若某项未完成，明确写出未完成原因，不留空。

---

## 6. 输出字段规范

每次调用本能力，最终至少补出：

- `ssa_available`
- `fallback_used`
- `fallback_checklist_completed`
- `confirmed_flows`（静态可达链路证明集合）
- `needs_review_flows`（链路可达但中间过滤需业务判定）
- `unresolved_gaps`（标了 `static-unknown` 的边界场景）

每条流必须标注入口点（controller method + HTTP method + URL pattern）。当同一 sink 从多个入口点可达时，拆成多条流分别列出，每条标注各自的入口点。

### 三态结果分类

三个等级均须在输出中列出，不允许只报 Confirmed 而省略其他：

- `Confirmed`（本能力上限：`static-confirmed`）
  - 数据流或结构链已完整成立
- `Needs Review`
  - 数据流接近成立，但仍需业务语义判断（中间过滤完整性）
- `False Positive`
  - source 不可控、sink 不可达或已有明确防护

全量输出的目的：让审核者能看到完整审计覆盖范围，并对边界 case 做二次判断。

### 与依赖闭源的协作

污点传播 / sink 进入**无可读源码依赖**（编译 jar / war / class、混淆代码）时，先走 [dependency-decompile](../../dependency-decompile/SKILL.md)，由其 triage 决定反不反编译：

- 落"安全 / sink 决策在调用方"（如 JDBC 驱动，拼 SQL 决策在调用方）→ 按已知语义续追、不留缺口
- 落"决策在依赖内"→ 反编译恢复源码再续追，看不了则留显式缺口、不要因看不到实现就直接判 `unresolved_gaps`

反编译恢复后续追时，若污点跨入产物内部新引用的无源码依赖，**同样回 dependency-decompile triage，别停在第一层**。

---

## 相关文件

- 入口 SKILL：[../SKILL.md](../SKILL.md)
- 闭环契约：[../../../common/closure-verification.md](../../../common/closure-verification.md)
- 上游粗筛：[../../sast-scan/SKILL.md](../../sast-scan/SKILL.md)
- 反编译协作：[../../dependency-decompile/SKILL.md](../../dependency-decompile/SKILL.md)
