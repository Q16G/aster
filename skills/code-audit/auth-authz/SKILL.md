---
name: auth-authz
description: 认证授权子清单 — 逐项排查登录语义、Cookie/Session 鉴权、IDOR/ownership、会话安全。
tags: code-audit,authn,authz,idor,session
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

## 固定检查项

适用并加载子 skill 后，按以下 checklist 逐项执行，确保覆盖完整。每项标注 `[x] done` 或 `[-] n/a (原因)`。

### 1. 业务逻辑认证授权复核（→ `business-logic-auth-review`，4 功能区）

**登录接口**
- 检查查询 key 正确性（用户名、邮箱、手机号等）
- 验证查询结果后是否走向正确分支（成功/失败），检测反转漏洞
- 检查密码/凭证比对的存在性和方向正确性
- 验证 session 在正确分支创建/重新生成（非复用旧 session ID）
- 检查登录响应码与实际认证结果的一致性
- 验证是否存在频率限制或锁定机制
- 检查登录成功后是否重建 session（防止 session 固定攻击）

**管理接口**
- 审计管理接口是否仅用 body/query/cookie 中的字段做权限判断
- 检查是否缺少 server-side auth context
- 验证敏感操作前是否仅做了客户端可控字段比对

**查询与 ownership**
- 检查 controller 是否拿到了 operator/owner 信息
- 验证 service 是否继续保留该约束
- 检查 mapper/SQL 是否在 WHERE 条件中落实了该约束
- 验证是否只剩 target/resource ID 而丢失 owner/operator 信息

**session/cookie 信任边界**
- 审计 request-derived value 是否进入 session
- 检查 Cookie value 是否直接进入分支判断
- 验证 session 中身份字段是否可被覆盖、污染或重新绑定

### 2. 会话安全审计（→ `session-security`）

- 验证 Session ID 是否使用密码学安全的随机数生成器（排除自增、时间戳、MD5(自增)）
- 若使用自定义 session ID 生成逻辑（非框架默认），验证熵值 ≥ 128 bit
- 检查登录成功后是否重新生成 Session ID（session.invalidate() + 新 session / changeSessionId()）
- 验证权限提升时是否重新生成 Session ID
- 检查是否接受客户端提供的 Session ID
- 审计并发 session 控制（同一账号是否允许多处登录）
- 审计 Session 超时设置与显式 logout 机制（销毁 session 而非仅清除 cookie）
- 验证 Session 数据存储位置的安全性（内存 / 文件 / 数据库 / Redis 是否加密存储）
- 检查 Cookie 安全属性（HttpOnly、Secure、SameSite、Path/Domain 不宜过宽）
