---
name: security-header-audit
description: HTTP 安全头专项审计 — 检查安全响应头的完整性、正确性和 Cookie 安全属性配置。
tags: code-audit,security-headers,hsts,x-frame-options,cookie,http
when-to-use: 当 config-sec P1 Router 识别到项目为 Web 应用且有 HTTP 响应头设置时
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# HTTP 安全头专项审计

## 目标

审计 Web 应用的 HTTP 安全响应头配置。安全头是纵深防御的重要层——单独缺失一个头通常是 LOW/INFO，但组合缺失会显著降低整体安全态势。

## 检查项

### 1. 关键安全头

| Header | 作用 | 缺失风险 |
|--------|------|---------|
| `Strict-Transport-Security` (HSTS) | 强制 HTTPS | 降级攻击 / SSL strip |
| `X-Content-Type-Options: nosniff` | 防止 MIME 类型嗅探 | 上传文件被浏览器重新解释执行 |
| `X-Frame-Options` / CSP `frame-ancestors` | 防止点击劫持 | 页面被嵌入恶意 iframe |
| `Content-Security-Policy` | 防止 XSS / 资源注入 | → 转交 `csp-audit` 深入分析 |
| `Referrer-Policy` | 控制 Referer 泄露 | 敏感 URL 参数泄露给第三方 |
| `Permissions-Policy` | 限制浏览器功能 | 摄像头/麦克风/定位被滥用 |

### 2. Cookie 安全属性

对所有 `Set-Cookie` 操作检查：

- `HttpOnly`：session cookie 必须设置
- `Secure`：生产环境必须设置
- `SameSite`：推荐 `Strict` 或 `Lax`（防 CSRF）
- `Path`：不宜设为 `/`（除非必要）
- `Domain`：不宜设置过宽

### 3. 危险头

- `X-Powered-By` / `Server`：是否泄露技术栈和版本
- `Access-Control-Allow-Origin: *`：CORS 过宽
- `Access-Control-Allow-Credentials: true` + 宽泛 origin：凭据泄露

## 结论口径

按安全头分类组织。每条发现标注：当前值（或"缺失"）、风险描述、建议值、severity。
