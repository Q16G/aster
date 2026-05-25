---
name: session-security
description: 会话安全专项审计 — 覆盖 Session ID 生成强度、Session 固定攻击防护、会话生命周期管理、Cookie 安全属性等会话层面的安全检查。
tags: code-audit,session,cookie,session-fixation,weak-session-id
when-to-use: 当项目存在 Session ID 生成、session 生命周期管理、Cookie 安全属性设置时
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# 会话安全专项审计

## 目标

审计项目的会话管理机制，发现 Session ID 可预测、Session 固定攻击、会话过期策略不当等问题。这类问题介于结构化漏洞和语义漏洞之间——部分可用规则覆盖（弱随机数生成），部分需要上下文推理（session 生命周期设计）。

## 检查项

### 1. Session ID 生成强度

- Session ID 是否使用密码学安全的随机数生成器
- 是否存在自增整数、时间戳、MD5(自增) 等可预测模式
- 是否使用自定义 session ID 生成逻辑（而非框架默认）
- 自定义 session ID 的熵是否足够（至少 128 bit）

### 2. Session 固定攻击防护

- 登录成功后是否重新生成 Session ID
  - Java：`session.invalidate()` + 新 session 或 `changeSessionId()`
  - PHP：`session_regenerate_id(true)`
  - Python/Flask：`session.regenerate()`
- 权限提升时（如从匿名到已认证）是否重新生成
- 是否接受客户端提供的 Session ID

### 3. 会话生命周期

- Session 超时设置是否合理（不宜过长）
- 是否有显式的 logout 机制（销毁 session 而非仅清除 cookie）
- 并发 session 控制（同一账号是否允许多处登录）
- Session 数据存储位置（内存 / 文件 / 数据库 / Redis）的安全性

### 4. Cookie 安全属性

- `HttpOnly`：是否设置（防止 JS 读取 session cookie）
- `Secure`：是否设置（防止明文传输）
- `SameSite`：是否设置（防止 CSRF）
- `Path` / `Domain`：是否限制过宽

## 结论口径

按入口点组织，与 `business-logic-auth-review` 的发现合并到同一入口点下。每条发现标注 `confirmed` / `needs_review` / `not_vulnerable`。
