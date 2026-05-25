---
name: auth-authz
description: 认证授权子清单 — 逐项排查登录语义、Cookie/Session 鉴权、IDOR/ownership、会话安全。
tags: code-audit,authn,authz,idor,csrf,session
when-to-use: 当需要聚焦审计认证授权维度，或项目存在登录、会话管理、权限判断、角色分级、资源归属检查时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 认证与授权任务清单

> 即使 SAST 未报出认证授权问题，仍应完成本清单。本维度覆盖的问题（登录语义反转、Cookie 伪鉴权、ownership 丢失）是规则引擎盲区。

| # | 任务项 | skill | 适用条件 |
|---|--------|-------|---------|
| 1 | 业务逻辑认证授权复核 | `business-logic-auth-review` | 存在登录/鉴权/权限判断/角色分级/资源归属检查 |
| 2 | 会话安全审计 | `session-security` | 存在 Session ID 生成、session 生命周期管理 |

对每项：评估适用条件 → 适用则 `load_skills` 加载并执行 → 标注结果。
