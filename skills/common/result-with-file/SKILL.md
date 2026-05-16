---
name: result-with-file
description: 将分析结果（source/sink/dataflow/verify）持久化为 Markdown 表格文件
version: "1.1"
agent: all
when-to-use: 所有分析完成后，需要将结果持久化输出时
context: inline
---

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
