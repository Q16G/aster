---
name: client-side-sec
description: 客户端安全子清单 — 逐项排查 CSP 策略、客户端 JS 安全。
tags: code-audit,csp,xss,dom,javascript,client-side
when-to-use: 当需要聚焦审计客户端安全维度，或项目有前端 JS 安全敏感逻辑、CSP 设置、DOM 操作、postMessage 时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 客户端安全任务清单

| # | 任务项 | skill | 适用条件 |
|---|--------|-------|---------|
| 1 | CSP 策略审计 | `csp-audit` | 项目设置了 CSP header 或 CSP meta 标签 |
| 2 | 客户端 JS 安全审计 | `client-js-audit` | 存在前端 JS 安全敏感逻辑（token 存储/DOM 操作/postMessage） |

对每项：评估适用条件 → 适用则 `load_skills` 加载并执行 → 标注结果。
