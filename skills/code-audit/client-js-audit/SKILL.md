---
name: client-js-audit
description: 客户端 JS 安全专项审计 — 覆盖 DOM XSS、客户端 token/凭据安全、postMessage 跨源通信、客户端安全决策逻辑等前端安全检查。
tags: code-audit,dom-xss,javascript,postmessage,client-side,token
when-to-use: 当项目前端存在安全敏感 JS 逻辑（token 存储/DOM 操作/postMessage/eval/innerHTML）时
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# 客户端 JS 安全专项审计

## 目标

审计前端 JavaScript 中的安全问题。SAST 规则能覆盖部分 DOM XSS 的 source→sink 模式，但以下场景需要 AI 的上下文推理：

- DOM XSS 变体（非标准 source/sink 组合）
- 客户端 token 存储和泄露路径
- postMessage 跨源通信的安全性
- 客户端安全决策是否有服务端兜底

## 与 stored-xss-detection 的边界

本 skill 关注 **source 在客户端** 的 XSS 链：`location.hash` / `document.referrer` / `postMessage` / `window.name` 等浏览器 API 作为输入源，数据不经过服务端直接进入 DOM sink。

`v-html` / `dangerouslySetInnerHTML` / `innerHTML` 等 sink 同时出现在本 skill 和 `stored-xss-detection` 中——**sink 相同但 source 不同**：
- 本 skill：source 是客户端可控输入（URL/hash/postMessage）→ **DOM XSS**
- `stored-xss-detection`：source 是服务端持久化数据（DB→API→渲染）→ **存储型 XSS**

审计时按 source 类型分流：客户端 source → 本 skill，持久化 source → `stored-xss-detection`。

## 参考案例

执行本 skill 前，应先阅读 `references/` 下的案例文件以建立攻击模式认知：

- [dom-xss-source-sink-chains.md](references/dom-xss-source-sink-chains.md) — DOM XSS 典型 source→sink 链（location.hash→innerHTML / URL param→jQuery.html() / referrer→DOM）
- [postmessage-origin-bypass.md](references/postmessage-origin-bypass.md) — postMessage 未校验 origin / 发送端 `'*'` / 消息进入 eval/innerHTML
- [client-token-storage-leakage.md](references/client-token-storage-leakage.md) — token 存 localStorage（XSS 可读）/ URL 传递（Referer 泄露）/ 全局变量暴露

## 检查项

### 1. DOM XSS 变体

参见 [dom-xss-source-sink-chains.md](references/dom-xss-source-sink-chains.md) 中的完整 source/sink 清单和攻击示例。

除了 SAST 已覆盖的标准 source→sink（`location.hash` → `innerHTML`），关注以下变体：

- `document.URL` / `document.referrer` → 任何 DOM 写入
- URL fragment / query parameter → `jQuery.html()` / `.append()` / `.after()`
- `postMessage` data → DOM 写入（跨 frame/window 注入）
- JSON.parse(untrusted) → 模板渲染
- 第三方库的 XSS sink（如 Handlebars 的 `{{{triple}}}` / Vue 的 `v-html`）

### 2. 客户端 Token/凭据安全

参见 [client-token-storage-leakage.md](references/client-token-storage-leakage.md) 中的风险矩阵和存储位置对比。

- Token 存储位置：`localStorage`（XSS 可读）vs `httpOnly cookie`（推荐）
- Token 是否在 URL 中传递（Referer 泄露）
- Token 过期机制（客户端是否可续期/伪造）
- 敏感数据是否明文存在于 JS 变量/全局对象

### 3. postMessage 安全

参见 [postmessage-origin-bypass.md](references/postmessage-origin-bypass.md) 中的 origin 校验绕过模式和攻击场景。

- `addEventListener('message', ...)` 是否验证 `event.origin`
- 发送端是否指定目标 origin（而非 `'*'`）
- 消息内容是否被不安全地使用（eval / innerHTML / location 赋值）

### 4. 客户端安全决策

- 权限检查是否仅在前端执行（无服务端校验）
- feature flag / 功能开关是否可通过 DevTools 修改
- 客户端加解密是否可被绕过（密钥硬编码在 JS 中）

## 结论口径

按 JS 文件或功能模块组织。每条发现标注 source、sink（如适用）、攻击场景和 severity。
