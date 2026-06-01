---
name: result-with-file
description: 将安全分析/测试结果持久化为按入口点/主题组织的分级 Markdown 报告，内嵌 POC
version: "2.0"
agent: all
when-to-use: 所有分析完成后，需要将结果持久化输出时
context: inline
---

## 执行步骤

1. **收集已完成步骤的分析结果（并集收集，禁止只读摘要）**：
   - 先取前序步骤 step outcome 中登记的 `result_file` / `summary_file` 路径。
   - **再递归列出共享工作区下的报告文件**（用你当前可用的任意手段枚举，不限定具体工具），避免遗漏未登记为 reference 的报告：workspace 根目录下的 `shared/` 目录树，以及所有 `sub_agents/**/shared/` 目录树（**含其下任意层级子目录**，如 `shared/recon-3/analysis-summary.md`），匹配全部分析报告 md（如 `*-report.md`、`*analysis*.md`、`*-overview.md`、`fallback_*.md`、`comprehensive-*.md` 等）。`planner_skills_index.md`、`findings-index.md` 等纯流程/索引/中间产物文件不算分析报告，跳过。**还须跳过本 skill 自己上一轮的输出**——即 `shared/` 根下的最终聚合报告，用双判据识别：(1) 文件名为 `{project}-security-report.md`（step 7 产物，与子报告 `*-api-security-report.md` 等不同名，不误伤）；(2) 文件内含 `## 源报告覆盖表` 这一聚合报告专属章节（子报告不会有，名字漂移时的兜底）。否则重跑时上一轮最终报告会被当源报告读入，发现翻倍、统计虚高。
   - **开始登记前先新建/清空 `shared/findings-index.md`（覆盖写，不在旧内容上追加）**，避免上一轮残留行污染本轮逐行对账。
   - 将两类来源**取并集**，**逐份**完整读取详细报告文件本身：每读完一份即把其每条发现/结构化交付物**增量追加一行写入 `shared/findings-index.md`**（每行登记：临时 ID / 标题 / 严重度 / source→sink / 入口点 / 来源文件绝对路径；**临时 ID 须全局唯一，建议带源序号前缀，如 `src1-001`**，避免多份报告各自从 F-01 起 append 后撞车），再读下一份。**不要求一次性把全部源报告 load 进上下文**——增量抽取并落盘即可，规避上下文过载。读完所有源报告后，这份 `findings-index.md` 即"应纳入最终报告的完整发现清单"。
   - **入口点清单也要落盘**：把攻击面盘点枚举出的入口点清单单独写入 `findings-index.md` 的一个区块（每入口点一行：HTTP method + URL + handler + 来源报告），作为 §4 覆盖面对账的依据——无发现的入口点不产生发现行，必须靠此清单才能核到。
   - **严禁用子 agent 的 `final_answer` / 摘要替代详细报告**：子 agent 的详细报告（写在各自 `sub_agents/<id>/shared/` 下）通常比冒泡上来的摘要包含更多发现、端点矩阵、攻击链等表格，必须读详细文件，否则最终报告会丢失这些内容。
   - **区分发现型与侦察型报告**：含漏洞/发现/矩阵的报告（如 `*-api-security-report.md`、`comprehensive-*.md`、`fallback_*.md`、`secret-detection-report.md`）其发现必须逐条带入；纯侦察/架构报告（如 `*-overview.md`、`*-technology-stack-report.md`）用于补全"配置/架构类发现"与覆盖声明的上下文，**正文可不照搬，但其结构化交付物（端点授权矩阵、技术栈表等）仍须在 `findings-index.md` 登记一行**，以便 step 6 对账，避免端点矩阵这类交付物被漏。
   - **去重但不折叠**：多份源报告报同一漏洞时，仅当 (source, sink, 入口点) 三元组完全相同才合并为一条；source、sink、入口点三者任一不同即各自独立成条，禁止跨三元组折叠。

2. **选择报告模板**：根据当前 agent 类型，从 `${SKILL_DIR}/reference/` 读取对应模板文件：

   | Agent 类型匹配 | 模板文件 |
   |---------------|---------|
   | `code-audit` | `${SKILL_DIR}/reference/code-audit-template.md` |
   | `pentest-*` | `${SKILL_DIR}/reference/pentest-template.md` |
   | `host-defense-*` | `${SKILL_DIR}/reference/host-security-template.md` |
   | 其他 / 不确定 | `${SKILL_DIR}/reference/general-template.md` |

   使用 `read_file` 加载模板文件，按模板结构组织报告内容。

3. **组织全部发现**：以 **step 1 收集到的全部发现为数据源**（即 `findings-index.md` 登记的每一条 + 已逐份读入的详细报告内容），**严禁回头从冒泡上来的步骤结果/摘要重新抽取**——正文与 index 必须同源。把每个漏洞实例独立成节，禁止合并或折叠。组织方式按模板区分：
   - **code-audit 模板：按入口点组织**（入口点 = handler/controller 方法 + HTTP method + URL pattern）。攻击面盘点枚举的每个入口点都必须出现；无发现的入口点标 `not_vulnerable` 以证明覆盖。入口点内多条发现按严重度降序（confirmed > needs_review > not_vulnerable）。同一 sink 被多个入口点到达时，每个入口点下独立列出。无单一 HTTP 入口点的系统性发现（硬编码密钥、弱加密、会话固定、CSRF 全局禁用、依赖漏洞等）归入"系统性发现"节。
   - **pentest / host / general 模板**：按各自模板结构组织。

4. **为每条 confirmed 发现编写 POC**：按下方"POC 规则"选择格式、构造内容、执行自检。needs_review 的发现 POC 可选。

5. **填充评估完整性章节**：同样以 step 1 收集结果（`findings-index.md` + 详细报告）为准，提取 needs_review 条目（附无法确认的原因和排查建议）、已排除的误报项（附排除依据）、已检测但未发现漏洞的维度、因前置条件不足或环境限制未能覆盖的维度，分别填入模板对应章节。最后编写结论章节，综合所有发现给出整体风险评级和后续建议。

6. **完整性交叉核对（写入前必须执行）**：拿 step 1 落盘的 `shared/findings-index.md` **逐行对账**最终报告：
   - `findings-index.md` 中**每一行**要么在最终报告正文找到对应发现，要么显式落入"误报与排除项"或"评估局限性"章节并注明原因；禁止 index 有、报告无却无任何说明（把"凭记忆自查"变成"照清单核对"）。
   - **入口点覆盖对账**：拿 `findings-index.md` 的入口点清单逐一核对 §4，每个入口点在报告中要么挂有发现，要么标 `not_vulnerable`，禁止枚举到却在 §4 缺席。
   - 结构化交付物（端点授权矩阵的每一行、每条攻击链、密码存储横向对比表、密钥泄露因果链等）同样逐项核对，缺失项须显式说明，禁止静默丢弃。
   - `severity_counts` / 风险统计表的数字必须等于最终报告实际列出的条目数。
   - 覆盖声明里标 `done` 的维度，其对应交付物（尤其端点授权矩阵）必须实际存在于报告正文，禁止"声明 done 但正文缺表"。
   - 最终报告末尾必须附**源报告覆盖表**（code-audit 模板见 §15；其他模板在末尾追加同等表格）：每份源报告 → 贡献的发现编号 → 是否全部纳入（是 / 部分+原因）。

7. **写入文件**：将完整 Markdown 报告写入共享工作区目录（workspace 根目录下的 `shared/` 子目录，使用绝对路径）。文件名格式：`{project}-security-report.md`。

8. **报告文件路径**：写入完成后，在调用 `update_current_step` 时将文件的绝对路径填入 `references` 字段，禁止使用相对路径。

## 通用规范（所有模板共享）

### 完整性约束

- 所有有危害的发现（CRITICAL 到 LOW）必须逐条出现，禁止折叠或省略；归属到对应入口点（code-audit）或系统性发现节，不得因组织方式丢条
- code-audit 模板按入口点组织：攻击面盘点枚举的每个入口点都必须出现，无发现的标 `not_vulnerable`
- 同一 sink 被多个入口点到达时，每个入口点独立成节
- 同一根因导致多个独立攻击面时，每个攻击面独立成节
- 代码审计模板的"端点授权矩阵（B3）"与"攻击链"为结构化交付物章节：源报告中已产出的端点矩阵整表、攻击链必须完整填入对应章节，禁止压扁成单条发现或省略整表
- 有 source→sink 或可构造 POC 的系统性漏洞（硬编码密钥、弱加密、可利用依赖漏洞等）归"系统性发现"节并逐条写卡片；仅配置/基线/架构缺陷（RBAC 缺失、安全头缺失等）用"配置/架构类发现"表格汇总，不需逐条 POC
- 风险统计表数字必须与实际 finding 数量一致
- 所有 needs_review 条目必须出现在"待人工复核项汇总"章节，逐条附注无法确认的具体原因和排查建议
- 初步检测后排除的误报项必须在"误报与排除项"章节列出，说明排除依据
- 已检测但未发现漏洞的维度必须在"已验证安全的维度"章节声明
- 因前置条件不足（如无测试账号、无源码权限、WAF 拦截等）导致无法覆盖的维度必须在"评估局限性"章节说明
- "结论"章节必须包含整体风险评级和后续建议

### POC 规则

#### 格式选择

| 场景 | 格式 | 原因 |
|------|------|------|
| 单一请求即可触发 | `http` | 可直接复制到 Burp/curl |
| 需多步骤、条件判断或自动化 | `python` | 单请求无法表达完整攻击链 |
| 默认凭证 / 配置类 | `http` | 一次请求验证即可 |

同一报告内尽量统一格式；对于单请求可触发的漏洞，优先选 `http`。

#### 构造规则（代码审计）

代码审计的 POC 从源码推导，未经运行时验证。必须遵循：

1. **URL 必须从代码注解推导**：@RequestMapping / @GetMapping / web.xml 映射 → 完整 URL path
2. **HTTP 方法必须从代码注解推导**：@GetMapping → GET, @PostMapping → POST
3. **参数名必须从 source 表达式提取**：request.getParameter("questionData") → 参数名 questionData
4. **payload 必须匹配漏洞类型**（见下方适配表）
5. **前置条件显式标注**：如需特定配置、认证、角色等，在 POC 上方注明
6. POC 末尾加注释行：`# 基于代码分析构造，未经运行时验证`

#### 构造规则（渗透测试）

渗透测试的 POC 是实际发送并得到响应的请求。必须遵循：

1. **请求必须是实际发送过的**（从 HAR/流量捕获中提取）
2. **响应要点附在 POC 后**：关键响应字段/状态码
3. 不编造未测试的请求

#### 漏洞类型 payload 适配

| CWE | 漏洞类型 | payload 要求 |
|-----|---------|-------------|
| CWE-78 | 命令注入 | payload 包含命令分隔符 + 可观测命令，如 `; id` 或 `| whoami` |
| CWE-89 | SQL 注入 | payload 包含 SQL 语法，如 `' OR 1=1--` 或 `1 UNION SELECT ...` |
| CWE-79 | XSS | payload 包含脚本标签，如 `<script>alert(1)</script>` |
| CWE-611 | XXE | payload 包含外部实体声明 |
| CWE-918 | SSRF | payload 包含内网地址，如 `http://127.0.0.1:port` |
| CWE-798 | 硬编码凭证 | payload 直接使用发现的凭证 |
| CWE-352 | CSRF | 提供完整 HTML PoC 表单 |
| CWE-22 | 路径穿越 | payload 包含 `../` 序列 |

#### 完整性要求

- 每条 confirmed 发现必须附 POC
- needs_review 的发现 POC 可选
- 渗透测试场景中证据链本身可充当 POC
- 配置/架构类发现（安全头缺失、RBAC 缺失等）用表格汇总，不需 POC

#### POC 最终自检（写入报告前必须执行）

所有 POC 写完后，逐条对照以下检查表。任一项不通过则修正后再写入报告。

| # | 检查项 | 通过标准 |
|---|--------|---------|
| 1 | URL 路径与源码一致 | 代码审计：URL 能在 @RequestMapping / web.xml / 路由配置中找到对应；渗透测试：URL 来自实际发送的请求 |
| 2 | HTTP 方法正确 | GET/POST/PUT/DELETE 与代码注解或实际请求一致 |
| 3 | 参数名正确 | 参数名可在 source 表达式（request.getParameter / @RequestParam / body 解析）中找到 |
| 4 | payload 匹配漏洞类型 | 对照"漏洞类型 payload 适配"表，payload 中包含该 CWE 对应的特征语法 |
| 5 | 前置条件已标注 | 若漏洞触发需要特定配置/认证/角色/数据状态，POC 上方已注明；无前置条件则标注"无" |
| 6 | 格式一致性 | 同一报告内同类单请求漏洞使用相同格式（http 或 python），不混用 |
| 7 | 代码审计标注 | 代码审计场景的 POC 末尾包含"基于代码分析构造，未经运行时验证"注释 |

### 文件格式

- 格式为 `.md`，不使用 JSON
- 文件名包含项目标识：`{project}-security-report.md`
