---
name: client-side-sec
description: 客户端安全子清单 — 逐项排查 CSP 策略、客户端 JS 安全。
when-to-use: 当需要聚焦审计客户端安全维度，或项目有前端 JS 安全敏感逻辑、CSP 设置、DOM 操作、postMessage 时
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 客户端安全任务清单

> 客户端安全关注两个层面：CSP 策略的完备性与可绕过性，以及前端 JS 代码中的数据流安全（DOM XSS、postMessage、Token 处理）。

| # | 任务项 | skill | 适用条件 |
|---|--------|-------|---------|
| 1 | CSP 策略审计 | `csp-audit` | 项目设置了 CSP header 或 CSP meta 标签 |
| 2 | 客户端 JS 安全审计 | `client-js-audit` | 存在前端 JS 安全敏感逻辑（token 存储/DOM 操作/postMessage） |

对每项：评估适用条件 → 适用则用 `skill` 工具加载并执行 → 标注结果。

## 基线检查项

以下是已知的常见检查角度，作为**基线起点**而非必检硬清单。结合目标代码与上下文动态调整：

- 适用且已完成 → 标注 `[x] done`
- 明确不适用 → 标注 `[-] n/a (原因)`，原因要具体到代码事实（例如"项目无 CSP 配置"），不要笼统写"不适用"
- 基线未列出但实际发现的相关问题 → 新增条目并标注 `[+] added (来源)`，来源指代码位置或上下文线索

不要为了凑齐基线而硬套不适用的检查；也不要因基线没列就漏掉真实发现。基线只是规范已知项，不限定覆盖边界。

### 1. CSP 策略审计（→ `csp-audit`）

- 检查 script-src 是否包含 'unsafe-inline' 或 'unsafe-eval'
- 验证是否存在 `*` 或过宽域名配置
- 检查 script-src 是否允许 data: 或 blob: URI
- 验证是否设置了 default-src
- 检查 script-src 是否使用 nonce 或 hash
- 检查允许域上是否存在可利用的 JSONP 端点
- 审计 base-uri、object-src 和 frame-ancestors 是否受限
- 检查是否仅使用 Report-Only 模式（不实际阻断违规请求，仅报告）
- 检查是否配置了 report-uri / report-to 上报端点

### 2. 客户端 JS 安全审计（→ `client-js-audit`）

**DOM XSS 数据流**
- 检查 document.URL/document.referrer 是否流入 DOM 写入操作
- 审计 URL fragment/query parameter 到 jQuery.html()/append()/after() 的流向
- 检查 JSON.parse(untrusted) 结果是否用于模板渲染
- 审计第三方库的 XSS sink（Handlebars `{{{triple}}}`、Vue `v-html`）

**postMessage 安全**
- 检查 addEventListener('message') 是否验证 event.origin
- 验证发送端是否指定目标 origin（而非 `*`）
- 审计消息内容是否被不安全地使用（eval/innerHTML/location 赋值、DOM 写入）

**Token 与敏感数据**
- 检查 Token 存储位置（localStorage vs httpOnly cookie）
- 验证 Token 是否在 URL 中传递
- 审计敏感数据是否明文存在于 JS 变量/全局对象

**前端逻辑安全**
- 检查权限检查是否仅在前端执行（无服务端校验）
- 检查客户端加解密密钥是否硬编码在 JS 中
