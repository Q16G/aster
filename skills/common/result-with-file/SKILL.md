---
name: result-with-file
description: 将分析结果（source/sink/dataflow/verify）持久化为 Markdown 表格文件
version: "1.0"
agent: all
when-to-use: 所有分析完成后，需要将结果持久化输出时
context: inline
---

## 输出规范

将分析结果写入 Markdown 文件，使用表格格式组织：

- 每个 (source, sink) 对为独立行，不折叠
- 必须包含列：漏洞类型、source、sink、数据流路径、验证状态、置信度
- 文件格式为 `.md`，不使用 JSON
