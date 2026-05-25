---
name: config-sec
description: 配置安全子清单 — 逐项排查敏感信息、安全头、危险配置。
tags: code-audit,config,secret,header,security-config
when-to-use: 当需要聚焦审计配置安全维度，或项目存在配置文件、Web 响应头设置、数据库连接配置时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 配置安全任务清单

| # | 任务项 | skill | 适用条件 |
|---|--------|-------|---------|
| 1 | 敏感信息检测 | `secret-detection` | **MUST** — 硬编码凭据、密钥、配置中的敏感信息 |
| 2 | 安全头审计 | `security-header-audit` | Web 应用且存在 HTTP 响应头设置 |
| 3 | 危险配置审计 | `dangerous-config` | 存在框架配置文件（php.ini/web.xml/application.yml 等） |

对每项：评估适用条件 → 适用则 `load_skills` 加载并执行 → 标注结果。
