# {project} 代码安全审计报告

> 本报告是各子报告的**无损超集**。下方各汇总表仅作索引/概览，明细以详细发现节（§4-§7）与附录（§15）为准；不得用汇总表替代明细。子报告中的入口点清单、数据流路径、源码片段必须逐条原样保留。

## 0. 模板专属机制（发现记录与派生 + fid 锚点 + 入口点台账）

> 以下是 code-audit 模板在 SKILL 通用机制之上的专属绑定：定义本模板的发现记录 schema / status / 去重键、给出派生命令产出 SKILL 闸门消费的两个 id 清单，并规定 fid 打在哪里、补一份入口点覆盖台账。

### 0.1 发现记录 schema（jsonl，每行一条）

```json
{"id","title","severity","cwe","source","sink","entry_point","status","confidence","file_location","source_report","description"}
```

- **status 取值与桶映射**：
  - `confirmed` → 进正文（**已确认**档，必附 POC）
  - `needs_review` → 进正文（**待复核**档，POC 可选，归"待人工复核项汇总"）
  - `false_positive` → 进排除项（"误报与排除项"章节）
  - `not_vulnerable` → 仅作覆盖证明（入口点节标 `not_vulnerable`，不进发现集）
  - `superseded` → 忽略（被更全的记录取代，不进任何章节）
- **去重键** = `[source, sink, entry_point]` 三元组；三者任一不同即各自独立成行，**禁止跨三元组折叠**。
- `id` 带源前缀（如 `sast-001`、`bla-003`），全局唯一。

### 0.2 派生命令（SKILL step 1 执行，产出 3 件机器产物）

```bash
mkdir -p shared/coverage-ledger/findings
merged=$(find shared/coverage-ledger/findings -name '*.jsonl' -exec cat {} +)

# 去重键非空校验：三元组全空会让 unique_by 把多条不同发现误折成一条
bad=$(printf '%s' "$merged" | jq -s 'map(select((.source==null or .source=="") and (.sink==null or .sink=="") and (.entry_point==null or .entry_point==""))) | length')
[ "${bad:-0}" = "0" ] || echo "FAIL: 有 $bad 条 jsonl 三元组全空，去重键失效，必须补全 source/sink/entry_point 再跑"

# 进正文桶（confirmed+needs_review，按去重键去重）
incl=$(printf '%s' "$merged" | jq -s 'unique_by([.source,.sink,.entry_point]) | map(select(.status=="confirmed" or .status=="needs_review"))')
{
  echo "# Findings Index（jq 机械派生，禁止手写）"; echo
  echo "| id | title | severity | source→sink | entry_point | status | source_report |"
  echo "|----|-------|----------|-------------|-------------|--------|---------------|"
  printf '%s' "$incl" | jq -r '.[] | "| \(.id) | \(.title) | \(.severity) | \(.source)→\(.sink) | \(.entry_point) | \(.status) | \(.source_report) |"'
} > shared/findings-index.md
printf '%s' "$incl"   | jq -r '.[].id'                                  | sort -u > shared/coverage-ledger/index-ids.txt
printf '%s' "$merged" | jq -r 'select(.status=="false_positive") | .id' | sort -u > shared/coverage-ledger/exclude-ids.txt
```

### 0.3 fid 锚点位置

每条进入正文的发现，在其卡片标题行或表格行**行末**附 `<!-- fid:<该发现 jsonl 的 id> -->`：

- §4 每张 `#### F-xx` 发现卡片的标题行
- §5 每张 `### SYS-xx` 系统性发现卡片的标题行
- §8 配置/架构类发现表的**每一行**（行末追加，紧跟 `|` 之后）
- §11 误报与排除项表的每一行（核到 `exclude-ids.txt`）

`### EP-xx` 入口点节标题**不打 fid**——它是组织单元、不是发现；无发现的 `not_vulnerable` 入口点节同理不打 fid。

### 0.4 入口点台账与覆盖断言

把攻击面盘点枚举出的入口点写入 `shared/coverage-ledger/entry-points.jsonl`（每入口点一行：`{"method","url","handler","source_report"}`）。无发现的入口点不产生发现行，必须靠此台账核到覆盖面。在 SKILL step 6 fid 闸门之后追加执行：

```bash
REPORT=shared/<project>-security-report.md
ep_total=$(jq -s 'length' shared/coverage-ledger/entry-points.jsonl 2>/dev/null || echo 0)
rep_ep=$(grep -cE '^### EP-' "$REPORT")   # 入口点节数
[ "$rep_ep" -ge "${ep_total:-0}" ] || echo "FAIL: 入口点节数 $rep_ep < 枚举入口点 $ep_total —— 有入口点未在 §4 成节（含应标 not_vulnerable 的）"
```

每个枚举入口点都须在 §4 成节（含标 `not_vulnerable` 的）；枚举到却缺席即失败。

## 1. 审计概览

| 项目 | 内容 |
|------|------|
| 审计对象 | {project + version} |
| 审计类型 | 静态代码安全审计 |
| 审计日期 | {date} |
| 技术栈 | {tech stack} |
| 核心文件 | {主要审计文件/模块} |

## 2. 覆盖声明

| 维度 | 覆盖状态 | 说明 |
|------|---------|------|
| 结构化漏洞扫描（SAST） | ✅/⚠️/❌ | {说明} |
| 认证授权复核 | ✅/⚠️/❌ | {说明} |
| 数据流验证 | ✅/⚠️/❌ | {说明} |
| 依赖安全审计 | ✅/⚠️/❌ | {说明} |
| 配置安全审计 | ✅/⚠️/❌ | {说明} |

## 3. 风险统计

| 严重度 | Confirmed | Needs Review | 合计 |
|--------|-----------|-------------|------|
| CRITICAL | N | N | N |
| HIGH | N | N | N |
| MEDIUM | N | N | N |
| LOW | N | N | N |
| **合计** | **N** | **N** | **N** |

## 4. 入口点发现

> **按入口点组织**。每个入口点 = handler/controller 方法 + HTTP method + URL pattern，作为小节标题。
> 攻击面盘点枚举的**每个入口点都必须出现**；经审计无发现的入口点单独成节并标 `not_vulnerable`，以证明覆盖范围。
> 入口点内多条发现按严重度降序排列（confirmed > needs_review > not_vulnerable）。同一 sink 被多个入口点到达时，在每个入口点下各自独立列出。
> **禁止折叠话术**（"还有相同 N 个接口"、"其余同理"、"等"、"代表性几例"等）：一个 sink 命中 N 个入口点就写 N 个 EP 小节，相似不构成折叠理由。详见 SKILL "通用规范 → 禁止折叠话术"。
> 发现编号 `F-xx` 在所属入口点内部编号；被攻击链/交叉核对跨节引用时用复合 ID `EP-xx/F-xx`。系统性发现（§5）用全局 `SYS-xx`。

### EP-01: {HTTP method} {URL pattern} — {Controller.方法}

> 该入口点下发现按严重度降序。无发现时写：`not_vulnerable`（已审计，未发现漏洞）。

#### F-01: {CWE-xxx 漏洞类型} <!-- fid:{该发现 jsonl 的 id} -->

| 属性 | 值 |
|------|-----|
| 漏洞类型 | CWE-xxx {名称} |
| 严重度 | CRITICAL / HIGH / MEDIUM / LOW |
| Source | {用户输入进入点} |
| Sink | {危险 API / 判断点} |
| 入口点 | {HTTP method + URL} |
| 验证状态 | confirmed / needs_review |
| 置信度 | high / medium / low |
| 文件位置 | {file:line} |

**数据流路径**:

```
{source} → {step1} → {step2} → ... → {sink}
```

**描述**: {漏洞的技术描述，包含上下文和触发条件}

**前置条件**: {触发所需配置/认证/角色，如"需 admin 角色"或"无"}

**POC**:

```http
{METHOD} {从 @RequestMapping 推导的 URL path} HTTP/1.1
Host: {TARGET}
Content-Type: {从代码推导的类型}

{参数名}={漏洞类型对应的 payload}
# 基于代码分析构造，未经运行时验证
```

**影响**: {攻击者可以做什么，最大损失评估}

**修复建议**: {具体修复措施}

---

（同一入口点下的更多发现 F-02… 按严重度降序；之后是 EP-02、EP-03… 直至覆盖所有枚举入口点）

## 5. 系统性发现（无单一 HTTP 入口点）

> 不绑定单一入口点、但**有明确 source→sink 或可构造 POC** 的系统性/横切漏洞：硬编码密钥、弱加密、会话固定、CSRF 全局禁用、可利用的依赖漏洞（如反序列化）等。
> 沿用 §4 的发现卡片格式（漏洞类型/严重度/Source/Sink/文件位置/POC/影响/修复），按主题分组，每条独立成节，禁止折叠（见 SKILL "通用规范 → 禁止折叠话术"）。
> 仅有配置/基线缺陷、无 source/sink、不需 POC 的项归 §8，不要放这里。

### SYS-01: {主题，如"硬编码加密密钥"} — {具体发现} <!-- fid:{该发现 jsonl 的 id} -->

（卡片字段同 §4 的 F 卡片；入口点字段可填"系统性/多入口点复用"）

## 6. 端点授权矩阵（B3）

> 枚举所有 Controller / Servlet 端点，逐端点输出下列字段。未覆盖到的端点显式标注 `未覆盖（原因）`。
> 内容来自认证授权复核（B1/B3）产出，禁止省略整表——这是覆盖声明标 `done` 的对应交付物。
> **禁止折叠话术**：每个端点一行，禁止"其余端点同上/略"（见 SKILL "通用规范 → 禁止折叠话术"）。

| Controller | 方法 | HTTP Method | URL Pattern | 需要登录? | 需要角色? | 需要归属校验? | 实际检查方式 | 判定 |
|-----------|------|-------------|-------------|----------|----------|--------------|------------|------|
| {controller} | {method} | {GET/POST/...} | {url} | {是/否} | {角色或否} | {是/否} | {实际检查代码} | {安全/越权/缺失} |

## 7. 攻击链

> 当多条独立发现可组合成更高危害的攻击链时，每条链独立成节。单条发现无法组合时本节可标注"无可行组合攻击链"。

### CHAIN-01: {链路最终危害概述}

- **链路步骤**: {step1} → {step2} → {step3} → ...
- **每步依赖的发现编号**: step1={EP-01/F-01} / step2={SYS-02} / ...
- **最终危害**: {高于任何单独发现的综合危害}
- **前提条件与可行性**: {触发链路所需条件，及可行性评估}

## 8. 配置/架构类发现

> 仅配置/基线/架构缺陷（无 source/sink、不需 POC）：安全头缺失、CORS 过宽、Cookie 属性缺陷、RBAC 架构缺失等。表格汇总即可。
> 有可构造 POC 或明确 source→sink 的系统性漏洞应归 §5，不要放这里。
> **禁止折叠话术**：每条缺陷一行，禁止"其余同上/略"（见 SKILL "通用规范 → 禁止折叠话术"）。

| 漏洞类型 | 严重度 | 描述 | 验证状态 | 置信度 | 修复建议 |
|---------|-------|------|---------|-------|---------|
| {type} | {sev} | {desc} | {status} | {conf} | {fix} | <!-- fid:{该发现 jsonl 的 id} --> |

## 9. 修复建议汇总

| 优先级 | 数量 | 说明 |
|--------|------|------|
| **P0 — 立即修复** | N | {概述} |
| **P1 — 高优先级** | N | {概述} |
| **P2 — 中优先级** | N | {概述} |
| **P3 — 低优先级** | N | {概述} |

## 10. 待人工复核项汇总

> 以下发现已执行分析但因特定原因无法确认。每项附注具体原因和建议的排查方向。

| 编号 | 漏洞类型 | 严重度 | 位置 | 原因 | 排查建议 |
|------|---------|-------|------|------|---------|
| NR-01 | {type} | {sev} | {location} | {为什么无法确认} | {建议如何排查} |

## 11. 误报与排除项

> 以下项在初步检测中被标记为可疑，经分析确认为误报。

| 编号 | 初始判定 | 排除原因 |
|------|---------|---------|
| FP-01 | {初始判定} | {为什么排除} | <!-- fid:{该 false_positive 发现 jsonl 的 id} --> |

## 12. 已验证安全的维度

> 以下维度在本次审计范围内已检测，未发现漏洞。

| 维度 | 验证方法 | 结论 |
|------|---------|------|
| {dimension} | {method} | 未发现漏洞 |

## 13. 评估局限性

> 以下维度因前置条件不足、环境限制或时间约束未能完整覆盖。必须并入各子报告中的环境/前置条件限制说明，逐条保留。

| 维度 | 限制原因 | 影响评估 | 补充建议 |
|------|---------|---------|---------|
| {dimension} | {reason} | {impact} | {what to do next} |

## 14. 结论

### 整体风险评级

{总体安全水位评估：一段话描述}

### 核心发现

1. {最关键的 2-3 个发现，一句话概括}

### 后续建议

1. {优先级最高的后续行动}

## 15. 附录：详细枚举与原始证据

> 容纳子报告中的大块明细（入口点/路由清单、数据流路径、算法或关键逻辑源码等）。逐条原样保留，禁止抽样或概括。正文相关发现引用此处对应小节。

### 15.1 入口点 / 路由清单

| # | Method | URL / 映射 | source 参数 | sink / 文件位置 |
|---|--------|-----------|------------|-----------------|
| 1 | {GET/POST} | {@RequestMapping} | {param} | {file:line} |

### 15.2 数据流路径明细

```
{source} → {step1} → ... → {sink}
```

### 15.3 算法 / 关键逻辑源码

```
{原样保留子报告中的源码片段}
```

### 15.4 其他原始证据

{配置清单、依赖清单等}

## 16. 附录：源报告覆盖表

> 对账依据为 `shared/coverage-ledger/findings/*.jsonl`（jq 去重后的发现真值源）及其派生视图 `shared/findings-index.md`：逐份源报告登记其贡献的发现是否全部纳入本报告。
> "部分纳入"必须注明未纳入条目去向（落入 §11 误报与排除项 / §13 评估局限性）。

| 源报告文件 | 贡献的发现编号 | 是否全部纳入 | 未纳入项说明 |
|-----------|--------------|------------|------------|
| {file path} | {EP-xx/F-xx / SYS-xx / CHAIN-xx ...，跨节引用用复合 ID} | 是 / 部分 | {若部分，注明未纳入条目及去向；全部纳入填"—"} |
