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

4. **为每条 confirmed 发现编写 POC**：POC 仅两种形式——原始 HTTP 数据包或 Python 脚本。needs_review 的发现 POC 可选。

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

- 仅两种形式：原始 HTTP 数据包（```http）或 Python 脚本（```python）
- 每条 confirmed 发现必须附 POC
- needs_review 的发现 POC 可选
- 渗透测试场景中证据链本身可充当 POC

### 文件格式

- 格式为 `.md`，不使用 JSON
- 文件名包含项目标识：`{project}-security-report.md`
