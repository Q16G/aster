---
name: result-with-file
description: 将分析结果（source/sink/dataflow/verify）持久化为 Markdown 表格文件
version: "1.1"
agent: all
when-to-use: 所有分析完成后，需要将结果持久化输出时
context: inline
---

## 执行步骤

1. **收集已完成步骤的分析结果**：读取所有已完成分析步骤的 `result_file` 和 `summary_file`。这些路径可从前序步骤的 step outcome 中获取。使用 `read_file` 逐一加载每个结果文件。

2. **提取全部发现**：从每个步骤结果中提取所有漏洞/配置/架构类发现。每个 (source, sink) 对必须独立成行，禁止合并或折叠。配置类发现（RBAC 缺失、安全约束缺失等）同样提取。

3. **格式化 Markdown 表格**：按下方「输出规范」中定义的列和约束，将所有发现组织为 Markdown 表格。在表格前添加严重度统计摘要（severity_counts），统计数必须与实际行数一致。

4. **写入文件**：将完整 Markdown 内容写入共享工作区目录（即 workspace 根目录下的 `shared/` 子目录，使用绝对路径）。文件名格式：`{project}-security-findings.md`。格式为 `.md`，不使用 JSON。

5. **报告文件路径**：写入完成后，在调用 `update_current_step` 时将文件的绝对路径填入 `references` 字段，禁止使用 `shared/...` 等相对路径。

## 输出规范（硬要求）

将分析结果写入 Markdown 文件，使用表格格式组织。这是持久化输出的唯一合法格式——叙述式报告不符合本 skill 规范。

### 表格格式

每个 (source, sink) 对为独立行，不折叠。必须包含以下列：

| 列名 | 说明 | 示例 |
|------|------|------|
| 漏洞类型 | CWE 分类或具体类型名 | CWE-78 命令注入 |
| 严重度 | CRITICAL / HIGH / MEDIUM / LOW | HIGH |
| source | 用户输入进入点 | request→decryptedData.get("para") |
| sink | 危险 API / 判断点 | Runtime.exec(cmd) |
| 数据流路径 | source 到 sink 的完整路径 | request→para→cmd[2]拼接→Runtime.exec() |
| 入口点 | HTTP method + URL | POST /report/arch |
| 验证状态 | confirmed / needs_review / not_vulnerable | confirmed |
| 置信度 | high / medium / low | high |
| 文件位置 | 文件名:行号 | ReportServiceImpl.java:62-69 |

### 完整性约束

- 所有有危害的发现（CRITICAL 到 LOW）必须逐条出现，禁止折叠或省略
- 同一 sink 被多个入口点到达时，每个入口点独立成行
- 同一根因导致多个独立攻击面时，每个攻击面独立成行
- 配置类、架构类发现（如 RBAC 缺失、web.xml 安全约束缺失、密码存储方案缺陷）也需列入表格
- severity_counts 必须与实际行数一致

### 文件格式

- 格式为 `.md`，不使用 JSON
- 文件名包含项目标识，如 `{project}-security-findings.md`
