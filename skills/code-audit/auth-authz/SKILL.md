---
name: auth-authz
description: 认证授权 P1 路由 — 编排登录语义、Cookie/Session 鉴权、IDOR/ownership、CSRF 等认证授权维度的专项复核。
tags: code-audit,authn,authz,idor,csrf,session,p1-router
when-to-use: 当 P0 Router 识别到项目有登录、会话管理、权限判断、角色分级、资源归属检查等信号时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: false
---

# 认证授权路由（P1 Router）

## 角色

你是认证授权维度的 **P1 路由**，负责将认证授权相关的审计需求分发到具体的 Topic Skill。

## 快速分诊

| 观察到的模式 | 加载 | 理由 |
|-------------|------|------|
| 登录函数、密码比对、认证分支判断 | `business-logic-auth-review` | 登录语义反转、伪成功/伪失败等需要逐行推理 |
| Cookie/Session 用于身份判断或权限决策 | `business-logic-auth-review` | Cookie 伪鉴权、Session 污染、信任边界越界 |
| 同时含 operator 和 resource 参数的接口 | `business-logic-auth-review` | IDOR / ownership 丢失需要跨层追踪 |
| Session ID 生成、session 生命周期管理 | `session-security` | 弱 Session ID、Session 固定、session 过期策略 |
| 状态变更操作（密码修改、数据删除、转账等） | `business-logic-auth-review` | CSRF、缺少二次验证、GET 方法执行状态变更 |

## Sub-Skill Map

```
auth-authz (P1 Router, 本文件)
├── business-logic-auth-review   ← 业务逻辑认证授权复核（核心）
└── session-security             ← 会话安全（Session ID 生成、生命周期、固定攻击防护）
```

## 推荐流程

1. **信号收集**：从 sast-scan 结果、攻击面盘点中提取认证授权相关的入口点和信号
2. **加载子 skill**：根据快速分诊表加载对应的 Topic Skill
3. **执行检查**：子 skill 按各自 checklist 执行
4. **汇总发现**：按入口点组织所有认证授权发现，回传给 P0 Router

## 覆盖完整性

当本路由被加载时，应确保以下维度被覆盖（即使 SAST 未报出相关问题）：

- 所有登录接口的语义正确性
- 所有使用 Cookie/Session 做权限判断的代码
- 所有含 operator + resource 参数的接口的 ownership 约束
- 全局 RBAC 架构评估（角色分级、资源隔离、跨角色越权）
- 状态变更操作的 CSRF 防护

遗漏任何一个维度都需要说明原因。
