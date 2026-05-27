---
name: result-with-file
description: 将安全分析/测试结果持久化为按严重度分级的 Markdown 报告，内嵌 POC
version: "2.0"
agent: all
when-to-use: 所有分析完成后，需要将结果持久化输出时
context: inline
---

## 执行步骤

1. **收集已完成步骤的分析结果**：读取所有已完成分析步骤的 `result_file` 和 `summary_file`。这些路径可从前序步骤的 step outcome 中获取。使用 `read_file` 逐一加载每个结果文件。

2. **选择报告模板**：根据当前 agent 类型，从 `${SKILL_DIR}/reference/` 读取对应模板文件：

   | Agent 类型匹配 | 模板文件 |
   |---------------|---------|
   | `code-audit` | `${SKILL_DIR}/reference/code-audit-template.md` |
   | `pentest-*` | `${SKILL_DIR}/reference/pentest-template.md` |
   | `host-defense-*` | `${SKILL_DIR}/reference/host-security-template.md` |
   | 其他 / 不确定 | `${SKILL_DIR}/reference/general-template.md` |

   使用 `read_file` 加载模板文件，按模板结构组织报告内容。

3. **提取全部发现并按严重度分级**：从每个步骤结果中提取所有漏洞/配置/架构类发现。按 CRITICAL → HIGH → MEDIUM → LOW 分级组织。每个漏洞实例独立成节，禁止合并或折叠。

4. **为每条 confirmed 发现编写 POC**：按下方"POC 规则"选择格式、构造内容、执行自检。needs_review 的发现 POC 可选。

5. **填充评估完整性章节**：从步骤结果中提取 needs_review 条目（附无法确认的原因和排查建议）、已排除的误报项（附排除依据）、已检测但未发现漏洞的维度、因前置条件不足或环境限制未能覆盖的维度，分别填入模板对应章节。最后编写结论章节，综合所有发现给出整体风险评级和后续建议。

6. **写入文件**：将完整 Markdown 报告写入共享工作区目录（workspace 根目录下的 `shared/` 子目录，使用绝对路径）。文件名格式：`{project}-security-report.md`。

7. **报告文件路径**：写入完成后，在调用 `update_current_step` 时将文件的绝对路径填入 `references` 字段，禁止使用相对路径。

## 通用规范（所有模板共享）

### 完整性约束

- 所有有危害的发现（CRITICAL 到 LOW）必须逐条出现，禁止折叠或省略
- 同一 sink 被多个入口点到达时，每个入口点独立成节
- 同一根因导致多个独立攻击面时，每个攻击面独立成节
- 配置类、架构类发现（RBAC 缺失、安全头缺失等）用表格汇总，不需逐条 POC
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
