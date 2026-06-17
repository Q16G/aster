---
name: dataflow-analysis
description: >-
  跨函数白盒数据流追踪——基于 SyntaxFlow / yak MCP 的 SSA 引擎，对已知 source / sink 证明跨函数
  source→sink 静态可达性，产出链路证据。source-sink 集合按调用者意图动态决定，不绑定具体漏洞
  类型。
when-to-use: 当需要对 SAST 候选集做数据流确认、验证污点传播可达性，或需要分析 request→session、cookie→auth、owner→mapper 等跨函数调用链路时
allowed-tools: bash,read_file,list_files,rg
mcp: [syntaxflow, yak]
user-invocable: true
argument-hint: "[target_path] [--lang java|go|python|js|php|c]"
arguments:
  - target_path
  - lang
---

# 跨漏洞数据流追踪（SyntaxFlow MCP）

## 1. 触发线索 / 适用信号

本能力是 Cat-B-Cross 跨漏洞数据流工具，按"上游候选 + 链路不可见性 + 跨端点共源"识别命中场景。

**承接 sast-scan 候选维度**：
- [sast-scan](../sast-scan/SKILL.md) 产出 `needs_dataflow_confirmation` 桶条目——sink pattern 已命中但跨函数 source 可达性未证
- sast-scan 命中 `Runtime.exec(String[])` / `JdbcTemplate.queryForObject(sql, args)` 等"同名 API 既能拼也能参数化"的弱 sink，需追上游确认实际形态
- 同一 source pool 被多个不同类型 sink 消费（用户输入既流入 SQL 又流入命令执行，需共用追踪起点）

**已定位 source/sink 但中间链路问题维度**：
- 调用栈多层（Controller → Service → DAO / Mapper）+ 中间过滤层 / wrapper / AOP 拦截不可见
- ORM 自定义封装层（项目自有 `BaseRepository` / `CommonMapper` / `CmdUtil.run` 等）把标准 sink 藏在 wrapper 内
- 二阶链路：source 先 INSERT 后另一段 SELECT 回读拼接，需跨文件追写-读关系
- 反编译来的依赖（参 [dependency-decompile](../dependency-decompile/SKILL.md) 产物）需在恢复源码上重新追内部数据流

**交叉端点维度**：
- 多个端点（admin / user / api）handler 复用同一 service 方法——单点确认后需横扫所有入口
- 同 `pattern_id` 的端点集合需统一证明可达性（避免逐 handler 重复手工追）

**反向信号**（不命中本能力）：
- source 和 sink 都在同一函数内、且无 wrapper——直接看代码即可
- 单类漏洞已有动态可观测效果证据——本能力只做"静态链路证明"，不再补做
- 项目无 SSA 编译产物且语言不被 SyntaxFlow 支持——走对应单漏洞 skill 的 grep + 人读 fallback

---

## 2. 造成原因

**本能力 n/a**（原因：跨多漏洞类型，单类漏洞成因归对应单漏洞 skill 的 §2。SQLi 成因看 `code-audit/sast-scan` 协作的 SQL 注入审计能力；命令注入成因看对应能力；反序列化成因看对应能力。本能力只提供「跨函数链路证明」工具，不承担成因解释）。

---

## 3. 领域 source-sink 数据流模型

本能力作为跨漏洞数据流工具，**source-sink 集合按调用者意图动态决定**（由 sast-scan 候选或用户给定的 source / sink 模式驱动）；本节只描述通用框架与 SyntaxFlow 表达方式，**不固化某类漏洞的 source-sink 列表**——单漏洞专属集合归对应单漏洞 skill 的 §3。

### 通用 source 类别（按白盒视角）

- **HTTP 请求字段**：query / body / header / cookie / path 参数（具体 API 因框架而异，见 §6）
- **外部输入**：文件读取 / 环境变量 / 命令行参数 / 配置文件 / 消息队列消费
- **数据库回读**：先入库后再读出来的字段（二阶 source，写入点参数化 ≠ 回读点安全）
- **反序列化产物**：JSON / XML / pickle / Java 序列化对象的字段
- **跨服务边界返回值**：RPC / HTTP 调用返回的字段（信任级别取决于下游可信度）

### 通用 sink 类别（按白盒视角）

- **代码 / 命令执行**：`Runtime.exec` / `eval` / `exec` / `os.system` / template engine `evaluate`
- **SQL 上下文**：直接拼接 / ORM Raw / MyBatis `${}` / 字段名 / 排序方向位置
- **文件 / 路径**：`new File` / `Path.resolve` / 上传写盘 / 模板加载
- **反序列化触发**：`ObjectInputStream.readObject` / `pickle.loads` / Jackson polymorphic
- **网络发送**：`HttpClient.execute` / `requests.get` / SSRF 出站调用
- **响应输出**：模板渲染 / `innerHTML` / 直接写 response body（XSS sink）

### 跨函数 / 跨文件追踪规则

- **跨函数**：source 流向其他函数参数、流向类成员、流向闭包捕获变量；从 sink 反向追到 source 入口
- **跨文件**：DAO / Repository / Service / Controller 调用链；按项目命名约定（`*Mapper` / `*DAO` / `*Service`）建立调用图
- **跨框架边界**：Spring `@Service` / Django middleware / Express middleware / AOP 拦截层是否引入过滤
- **跨依赖边界**：闭源依赖内部数据流不可见，走 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译后再续追（参 §11）
- **跨服务边界**：本服务范围内追到出站 RPC / 消息发送即停，下游服务由对应 skill 独立审计

### SyntaxFlow 表达方式（基础语法）

SyntaxFlow DSL 是 SSA 上的图查询语言。基本运算符：

- **TopDef（向上追溯定义）**：`#>`（一跳直接定义） / `#->`（递归到定义链起点）
- **BottomUse（向下追踪使用）**：`->`（一跳直接使用） / `-->`（递归到使用链终点）
- **限深控爆炸**：`$x #{depth: 5}-> $y;` / `$x -{depth: 3}-> $y;`（首次调试建议加，避免大项目 SSA 图爆炸）
- **锚定 + 捕获**：`Method.parameter() as $sink;`（先选具体调用 / 方法链作 anchor，再用 `* as $arg` 捕获关心的参数）
- **Alert 输出**：`alert $var for { message: "...", level: info };`（每条规则至少一个 alert，否则视为无可见输出）

完整 DSL 语法 / Cookbook / 各类 sink 查询模板见 [references/syntaxflow-cookbook.md](references/syntaxflow-cookbook.md)。

---

## 4. 常见类型

**本能力 n/a**（原因：跨多漏洞类型，攻击变体属漏洞自身属性，归对应单漏洞 skill 的 §4。本能力只对"任意 source → 任意 sink 链路"做静态可达性证明，不分变体）。

---

## 5. 入口点定位

本能力的"入口点"是「已知 source / sink 端点 + 中间链路问题」——不像单漏洞 skill 从项目结构盘点 sink，本能力直接从上游候选或用户给定端点起步。

### 起点来源

按优先级三选一作为追踪起点：

1. **sast-scan 候选承接**：读 `shared/coverage-ledger/findings/sast-scan.jsonl` 里 `confidence=needs_dataflow_confirmation` 的条目，每条带 `file_location` 即 sink 位置；source 由本能力跨函数追溯
2. **用户给定端点**：用户直接传入"某 Controller 方法 + 某 sink 形态"，按代码 pattern 定位
3. **单漏洞 skill 委托**：单漏洞 skill 已自定位 sink 但跨函数链路追不动，转交本能力补链路证明

### 项目 SSA IR 准备

> 下列项目类型仅作示例，以目标实际栈为准。

- 用 `yak` MCP 加载目标项目，`ssa_compile(target, language, program_name)` 编译到 SSA IR
- `program_name` 用项目稳定名（如 `petclinic-main`），便于复用查询
- 编译失败 / 语言不支持时走 [references/syntaxflow-cookbook.md](references/syntaxflow-cookbook.md) 的 fallback 流程

### 查询起点选择

追踪方向有两个维度，按上下文选：

- **从 sink 反向追**（推荐默认）：sink 已知具体形态时——锚定 sink 调用，用 TopDef（`#->`）追上游定义链。why：sink pattern 更具体、anchor 收敛性强，比从 source 正向追的 SSA 图爆炸面小
- **从 source 正向追**：source 已知具体形态（如某 filter 输出值）、需要看流向哪些 sink 时——锚定 source 表达式，用 BottomUse（`-->`）追下游使用链。why：交叉端点场景下用一次正向追覆盖多个 sink 比逐 sink 反向追高效
- **双向同时追**（同时回答"从哪来 / 到哪去"）：在已定位 sink 上同时跑 TopDef + BottomUse，参 §8 / cookbook 片段 E

### 前置探索 SSA IR 结构

直接写 SyntaxFlow 容易漏对项目实际 IR 形态，**why**：项目自定义 wrapper / 注解处理器 / Lombok 生成代码会让 IR 与源码长得不一样。用 `ssa_query` 先探索：

- 锚定一个已知的 sink 调用，跑 `<ANCHOR>(* as $arg) as $call; alert $call`，看实际匹配到的调用点是否符合预期
- 若匹配为 0 / 数量异常 → 检查 sink 表达式是否覆盖了项目自有 wrapper、注解形态、Lombok 生成方法名

---

## 6. 跨框架代码变体

本能力跨语言可用，**不同语言项目的 SyntaxFlow 查询差异**主要在 source / sink anchor 表达式以及 IR 形态——业务调用语义相同，但 anchor 写法不同。

> 下列框架 / 语言示例仅作类似项目示例，不限于此；以目标实际栈为准。

| 语言 / 框架 | source anchor 形态 | sink anchor 形态 | 项目 IR 差异要点 |
|---|---|---|---|
| **Java / Spring** | `@RequestParam` / `request.getParameter` / `@RequestBody` 注解参数 / `request.getAttribute` | `Runtime.getRuntime().exec` / `JdbcTemplate.queryForObject` / `EntityManager.createNativeQuery` | Bean 注入 / AOP 拦截 / Lombok 生成 getter — anchor 要覆盖代理类与原始类两套方法名 |
| **Java / MyBatis** | 同上 + service 方法参数 | Mapper 接口方法 + `@Select` / XML 模板 `${}` | XML 不在 SSA IR 内 — 需 grep XML + SSA 联合，参 cookbook 模板 F |
| **Go** | `gin.Context.Query` / `Context.PostForm` / `Context.GetHeader` / struct binding | `db.Raw` / `db.Exec(fmt.Sprintf(...))` / `os/exec.Command` / `Runtime` | 接口实现 / 方法集 — anchor 写接口方法时要兼容所有实现；channel 传值跨 goroutine 在 SSA 上是边断点 |
| **Python / Django** | `request.GET.get` / `request.POST` / `request.META` / form `cleaned_data` | `cursor.execute` / `Model.objects.raw` / `os.system` / `subprocess.Popen` | 动态属性访问（`getattr` / `**kwargs`）— SyntaxFlow 静态追踪到此为边断点（参 §11） |
| **JS / Node.js / Express** | `req.query.x` / `req.body.x` / `req.cookies` / `req.params` | `sequelize.query` / `child_process.exec` / template literal 拼接 | CommonJS vs ESM 导入；解构赋值（`const {x} = req.body`）— anchor 要兼顾解构 alias |
| **PHP / Laravel** | `$request->input` / `$_GET` / `Request::input` | `DB::raw` / `whereRaw` / `eval` / `system` | Facade 静态代理（`DB::` → `\Illuminate\Support\Facades\DB`）— anchor 写实际实现类 |

**跨语言通用要点**：

- 项目自有 wrapper / 工具类 anchor 必须显式纳入（如 `CmdUtil.run` / `BaseRepository.rawQuery`）—— 漏掉就误以为"项目无 sink"
- 框架自动转义层（Spring HTML 转义 / Django 模板自动 escape）在 SSA 上看不到的位置——必须按框架文档判断是否引入过滤，参 §10 反例 3
- Lombok / 注解处理器 / 代码生成器（mybatis-generator）产出的代码——SSA IR 里方法名可能是生成后名字，要核对编译产物

---

## 7. 思考检查点

加载本 skill 时按这些问题思考（按 sink 语义、不按业务命名）：

- source 表达式是否覆盖了所有可能的入口位置（路径参数 / body / header / cookie / 已入库回读字段）？漏掉任一类源就可能把"真实可达"误判为"不可达"
- sink 表达式是否覆盖了**项目自有 wrapper / 封装层**？规则只查标准库 API 时，项目把 `Runtime.exec` 封装成 `CmdUtil.run` 会让命中数异常少
- 跨函数追踪到的"终点 sink"是真实危险操作还是中间 helper？helper 内部如果再调用真正的危险 API，需继续追下一跳
- 中间过滤层 / 白名单 / 类型转换是否被 SyntaxFlow 识别到？SSA 图能看到调用边但**判断不了语义**（"过滤是否完整"是业务判断，本能力只给"路径上有无过滤调用"事实）
- 同一 sink 是否被多个入口点到达？每个入口点单独成行（参 §9 产物契约），禁止合并折叠
- 闭源依赖 / 反射调用 / 动态字符串构造是否标了 `static-unknown`？默认 not_vulnerable 是白盒最常见的诚信底线失守（参 §11）

---

## 8. 检测方法论 / 数据流追踪

> 本能力**只到链路证明**——动态利用证据由黑盒 / graybox 流程取证；单漏洞 confirmed 结论由对应单漏洞 skill 给出，本能力只提供"静态可达"素材。

### 总览

跨函数数据流追踪的核心动作：**锚定（Anchor） → 捕获（Capture） → 追踪（Trace） → 输出（Alert）**——这不是固定步骤编号，而是写每条 SyntaxFlow 规则的四个递进维度。完整 DSL 语法、各类 sink 查询模板、Alert 规范、限深策略见 [references/syntaxflow-cookbook.md](references/syntaxflow-cookbook.md)。

### 查询构造的递进维度（非编号步骤）

**前置：SSA 编译 + IR 探索**
- `ssa_compile(target, language, program_name)`——若失败 / 语言不支持 → 走 cookbook 的 fallback 协议
- 前置探索 SSA IR 结构（参 §5 末段）——直接写 SyntaxFlow 容易漏对项目实际 IR 形态。why：项目 wrapper / Lombok 生成代码 / 注解处理器会让 IR 与源码长得不一样，先用一两条粗查询核验 anchor 真能命中

**锚定（Anchor）**：选具体调用点 / 方法链作起点（如 `Runtime.getRuntime().exec` / `Files.write` / `.parse` / `.evaluate`）
- 查询泛化的取舍：anchor 太宽（如 `*.* #-> ...` 全局 wildcard）召回大量噪声 SSA 图爆炸；太窄会漏掉项目自有 wrapper。why：先按已知标准 API 锚定，命中后看是否需要把 wrapper API 加入 anchor 集合
- 项目自有 wrapper anchor 是否纳入——这是本能力区分于"只查标准库"的关键

**捕获（Capture）**：用 `* as $arg` 把关心的参数 / 值抓出来
- 多参数 sink 要分清哪个参数是数据流终点（如 `JdbcTemplate.queryForObject(sql, args)`——`sql` 是 sink 上下文、`args` 是参数化绑定通道，追错参数等于追错语义）

**追踪（Trace）**：默认 `#->`（必要时先 `#>` 粗定位）；首次调试加 `depth: 3~8` 限深
- 反向（sink → source）vs 正向（source → sink）的取舍参 §5
- depth 限制太小会漏深链路，太大会图爆炸；从小开始按需放宽

**输出（Alert）**：每条规则至少一个 `alert $var for { message: "...", level: info }`
- 无 alert 视为无可见输出（即使内部追到结果也读不到）

### 跨函数追踪示例

**示例 1：从 SQL sink 反向追到 HTTP source**

锚定 `JdbcTemplate.queryForObject` 第一个参数为 sink，TopDef 递归到 source；若 source 命中 `@RequestParam` 注解或 `request.getParameter` 调用 → 静态可达；若链路中出现 `WhitelistUtil.check` / `Integer.parseInt` 等过滤调用 → 标 `static-unknown`，需人工判定过滤是否完整。

**示例 2：交叉端点 source 一次正向追**

source 是 filter 派生的 `request.setAttribute("userId", decoded)`；锚定 `setAttribute("userId", ...)`，BottomUse 一次追到所有读取 `getAttribute("userId")` 的 handler，再对每个 handler 看其下游 sink。

**示例 3：二阶链路追**

source 是 INSERT 写入字段（`INSERT INTO user(email) VALUES(?)`）；BottomUse 追该字段被哪些 SELECT 引用，每个回读点单独追是否参数化。

### 与 sast-scan 协作模式

- sast-scan `needs_dataflow_confirmation` 桶 → 本能力按 `file_location` 锚定 sink，反向追 source 可达性
- 同 `(source, sink)` 对在多端点出现 → 每个 `entry_point` 单独成行（参 §9）
- 链路证明完成 → 把 `confidence` 从 `needs_dataflow_confirmation` 升为 `dataflow-confirmed`（即本能力的 `static-confirmed`），写入 `shared/coverage-ledger/findings/dataflow-analysis.jsonl`
- 单漏洞 skill 拿本能力的 `dataflow-confirmed` 条目 + 黑盒可观测效果证据 → 最终判 `confirmed`

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] SSA 编译成功；失败或语言不支持时已走 cookbook fallback
- [ ] anchor 表达式已纳入项目自有 wrapper / 封装层
- [ ] 每条规则至少含一个 alert
- [ ] 跨函数追踪到真实终点 sink 而非中间 helper
- [ ] source 覆盖所有入口位置（query / body / header / cookie / path / attribute / 二阶回读）
- [ ] 中间过滤层 / 白名单调用点已列出（不替业务做"过滤是否完整"判断）
- [ ] 反射 / 闭源依赖 / 动态构造已标 `static-unknown`，未默认 not_vulnerable
- [ ] 每个 `(source, sink, entry_point)` 三元组独立成行落 jsonl

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——本能力的产物结构是下游单漏洞 skill / `result-with-file` 机器消费的接口；产物聚合或省略会让单漏洞 skill 无法回溯具体 file:line 与跨函数链路，整条链路失效，因此是刚性要求。

### 白盒判定上限

本能力作为**跨漏洞数据流工具**，判定上限为 `static-confirmed`（source-sink 链路静态可达，跨函数链路完整无 unknown 断点），**不等于动态 confirmed**，且**不独立给出单漏洞 confirmed 结论**——本能力提供"链路证明"作为单漏洞 skill 升级 confirmed 的素材。

| 链路状态 | 上限状态 | 升级路径 |
|---|---|---|
| 跨函数链路完整可达、无过滤、终点 sink 危险形态 | `static-confirmed (dataflow)` | 交单漏洞 skill 收黑盒可观测效果证据 → 最终 `confirmed` |
| 链路可达但中间有过滤 / 白名单调用、过滤完整性需业务判定 | `needs_review` | 业务复核过滤逻辑；过滤不完整则降回 `static-confirmed` |
| 链路追到反射 / 闭源依赖 / 动态构造边界 | `static-unknown` | 推 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译续追，或推动态分析 |
| 链路证明经参数化绑定 / 白名单 enum / 无 raw 通道 | `not_vulnerable` | — |

**禁止**仅凭本能力链路证明直接判 `confirmed`——无可观测效果证据，仅静态可达不构成动态利用。

### 产物契约（必须遵守）

**为什么这里是「必须」**：产物结构是下游机器消费的接口；聚合 / 区间 / 抽样会让 `result-with-file` 计数闸门失效，并让单漏洞 skill 无法回溯到具体 file:line 与跨函数链路上的中间过滤点。

每确认一条数据流发现 / 需复核项**立即** append 一行规范化 jsonl 到 `shared/coverage-ledger/findings/dataflow-analysis.jsonl`，不等汇总阶段回头整理（why："事后总结"是聚合 / 区间 / "等"省略的根源）：

```json
{
  "id": "df-001",
  "title": "request.getParameter -> JdbcTemplate.queryForObject 跨函数可达",
  "severity": "high",
  "cwe": "CWE-89",
  "source": "request.getParameter(\"name\")",
  "sink": "JdbcTemplate.queryForObject(sql, ...)",
  "entry_point": "GET /user/search",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "file_location": "UserController.java:42 -> UserService.java:88 -> UserDao.java:120",
  "source_report": "dataflow-analysis",
  "description": "..."
}
```

字段约束：

- `id` 带 `df-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（本能力默认 `needs_review`，不独立判 `confirmed`）
- `confidence ∈ static-confirmed | needs_review | static-unknown | not_vulnerable`
- `(source, sink, entry_point)` 三元组任一不同即各自独立成行——同一 sink 被多个入口点到达时每个入口点各一行，**禁止合并折叠**
- `file_location` 用 `->` 连接跨函数链路（source 位置 → 中间函数 → sink 位置）；单点 sink 也填具体 `file:line`，不留空
- `entry_point` 填该流可达的 HTTP 入口点（method+URL）；无明确入口点的系统性命中填 `systemic`

**禁止**：

- 聚合计数（"50 处可达 SQL sink + 13 处可达命令注入"）—— 丢失了具体链路，下游无法消费
- "等" / "..." / "（略）" / "（其余 N 条同理）" 省略 finding —— 看似覆盖完整实则漏检
- 同一 sink 多入口点合并成一行 —— 入口点对应不同的鉴权 / 业务上下文，合并掉等于丢失语义

### 反例义务（必须遵守）

> **why**：本能力"链路不可达 / 已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游单漏洞 skill 误信"该子系统该 sink 安全"。

写"未发现可达链路"或"已防护"前，产物必须包含：

- 所有 source 候选位置完整清单（每个 handler / Controller / filter 派生值都有追踪结论）
- 所有 sink 候选位置完整清单（含项目自有 wrapper anchor 覆盖证据）
- 每个 (source, sink, entry_point) 三元组的判定结果（链路状态 + 中间过滤点）
- `static-unknown` 单元格的具体原因（反射 / 闭源 / 动态构造 / 跨服务边界）

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：SyntaxFlow 查询过宽召回大量 helper / wrapper 误报**

- 抽象规则：anchor 写得太泛（如直接锚 `*.exec` 而不限定具体类）会把 helper、test、不可达分支都召回
- 具体场景：`Runtime.*.exec` 锚定后召回数百条，其中大量是测试代码 / 反射工具类调用
- 关键识别特征：召回数远超项目规模合理上限；多数命中在 `test/` / `*Test.java` / `MockExec` 类
- 排除方法：anchor 收敛到具体调用链（`Runtime.getRuntime().exec`），或加 depth 限制，或在追踪后用文件路径过滤排除 test

**反例 2：source 表达匹配到了非用户可控的内部入口**

- 抽象规则：`request.getAttribute("x")` 命中但实际值由 filter 内部设定而非用户可控
- 具体场景：admin-only handler 接收 `request.getAttribute("internalUserId")`，值由可信 filter 写入，攻击者无法直接控制
- 关键识别特征：source 追到上游是某个 filter 内部 `setAttribute` 而非 `request.getParameter`
- 排除方法：本能力客观记录链路（filter → handler → sink），把"filter 是否可绕"判断留给单漏洞 skill 业务复核；不在数据流阶段判 FP

**反例 3：框架自动转义层在 SSA 图上看不到**

- 抽象规则：Spring HTML 转义 / Django template autoescape / Express EJS 默认转义在 SSA 上没有显式调用边
- 具体场景：链路从 `request.getParameter` 直达 `model.addAttribute` 再到 Thymeleaf 模板渲染，SSA 看似无过滤；但 Thymeleaf 默认 HTML escape 让 XSS 不成立
- 关键识别特征：sink 是模板渲染 / response 输出，且项目用了带默认转义的模板引擎
- 排除方法：本能力链路证明仍记为 `static-confirmed`，但补一行 `description` 注明"sink 是 Thymeleaf 模板渲染、默认 HTML escape"；最终判定由单漏洞 skill（如 stored-xss-detection）结合框架默认行为给出

### FN（看似不命中实际是真洞）

**反例 4：查询过窄漏掉等价 sink**

- 抽象规则：`Runtime.exec` 写了但 `ProcessBuilder.start` 没写，漏掉等价 sink
- 具体场景：项目主要用 `ProcessBuilder().command(...).start()` 启动子进程，规则只锚 `Runtime.exec`，命中为零
- 关键识别特征：命中数异常少 / 项目代码里能 grep 到 `ProcessBuilder` 但 SyntaxFlow 报告无命中
- 确认方法：sink 类别按"语义等价集合"扩展（执行：`Runtime.exec` + `ProcessBuilder.start` + `Kernel32#CreateProcess`；SQL：`Statement.execute*` + `PreparedStatement.execute*` + ORM raw）

**反例 5：项目自定义包装层未识别**

- 抽象规则：项目封装 `CmdUtil.run(String)` 内部调 `Runtime.exec`，规则只查 JDK API 漏报
- 具体场景：业务调用点全是 `CmdUtil.run(userInput)`，SyntaxFlow 报告"无命令执行 sink"
- 关键识别特征：[project-framework-analysis](../project-framework-analysis/SKILL.md) 输出的"自有类与方法"清单提示项目有自定义封装但 SyntaxFlow 命中数异常少
- 确认方法：把 `CmdUtil.run` 加入 anchor 集合重跑；或对 `CmdUtil` 类先做一遍"内部是否调危险 API"扫描确认其为 sink wrapper

### 易混淆案例

**反例 6：闭源依赖里的 sink 默默被忽略**

- 抽象规则：依赖库内部有 sink 但 SyntaxFlow 没有源码可追
- 具体场景：项目调用 `external-lib.queryUser(name)` 数据流到此为止，但 external-lib 内部用拼接
- 关键识别特征：链路追到依赖包边界即停；依赖图谱里该包是 `unknown`
- 排除方法：标 `static-unknown` 推 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译后重新追内部数据流；**禁止**默认为 not_vulnerable

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到 SSA 图上的可见调用边为止；下列情形必须标 `static-unknown`，**不允许**默认为 not_vulnerable。

### SyntaxFlow 追踪到边界的情形

**反射 / 动态分派**：

- Java：`Method.invoke()` / 通过字符串决定调用哪个 DAO 方法
- Python：`getattr(obj, method_name)(args)` / `eval` / `exec`
- JS：`obj[methodName]()` / `new Function(...)`
- 处置：SyntaxFlow 标 `static-unknown`，记录反射点 `file:line`；不替业务做"反射目标是否危险"判断

**闭源 / 无源码依赖**：

- 三方依赖（jar / dll / so / 闭源 SaaS SDK）
- 链路追到依赖边界即停
- 处置：依赖图谱标 `unknown` → 走 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译恢复源码 → 在反编译产物上重新跑本能力续追内部数据流；**反编译恢复后续追时，若污点跨入产物内部新引用的无源码依赖，同样回 dependency-decompile triage**——别停在第一层

**动态字符串构造**：

- 配置文件驱动的 SQL（`config.get("query.user")` 然后执行）
- 代码生成器（mybatis-generator / sequelize-cli）生成的 SQL
- 模板引擎动态加载（`Template.evaluate(string)`）
- 处置：标 `static-unknown`，记录配置 / 模板位置；如有必要读实际配置文件验证；推 dynamic analysis

**跨服务 / 跨进程边界**：

- RPC 调用（gRPC / Thrift / REST）跨微服务
- 消息队列（Kafka / RabbitMQ）异步消费
- 跨进程 IPC
- 处置：本服务范围内的 source 追踪到出站调用即停；下游服务由对应 skill 独立审计

**AOP / 注解处理器 / 框架自动注入**：

- Spring `@Aspect` 拦截器在 sink 调用前后插入逻辑
- Django middleware 修改 request 上下文
- 注解处理器（Lombok / mapstruct）生成的方法体
- 处置：列出所有拦截器 / middleware / 注解处理器的实现位置；本能力客观记录链路，业务复核判过滤完整性

**运行时配置切换 / feature flag**：

- dev / prod 不同分支用不同 SQL 模板
- feature flag 控制查询路径
- 处置：每个分支独立审计；不能只看 dev 分支下结论

**SSA 编译失败 / 语言不支持**：

- SyntaxFlow MCP 未连接 / yak 未安装 / `ssa_compile` 失败 / 语言暂不支持
- 处置：走 [references/syntaxflow-cookbook.md](references/syntaxflow-cookbook.md) 的 fallback 协议（`rg` 入口盘点 + 参数角色盘点 + 固定 checklist），**禁止**直接判"无可达链路"

**底线**：本能力写"该 (source, sink) 对无可达链路"前，所有 `static-unknown` 单元格必须显式列出原因（反射点 file:line / 闭源依赖名 / 动态构造位置 / 跨服务出站调用点）。否则结论降级 `partial-coverage`。

---

## 12. 修复建议

**本能力 n/a**（原因：本能力是跨漏洞数据流工具，不对单类漏洞修复负责。修复路径见对应单漏洞 skill 的 §12：SQL 注入修复 → 参 sast-scan 协作的 SQL 注入审计能力下游 skill；命令注入修复 → 对应能力；XSS 修复 → [stored-xss-detection](../stored-xss-detection/SKILL.md) §12；反序列化修复 → 对应能力。本能力只提供"链路证明"素材，让单漏洞 skill 在制定修复时知道**该修哪一段链路**——上游 source 加白名单还是 sink 上加参数化绑定）。
