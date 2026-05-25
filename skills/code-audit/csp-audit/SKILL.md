---
name: csp-audit
description: CSP 策略专项审计 — 分析 Content-Security-Policy 指令的语义正确性、绕过风险和覆盖完整性。
tags: code-audit,csp,content-security-policy,xss,client-side
when-to-use: 当项目设置了 CSP header 或 CSP meta 标签时
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# CSP 策略专项审计

## 目标

CSP（Content-Security-Policy）是防御 XSS 的重要层。本 skill 不是检测"有没有 CSP"，而是分析**已有 CSP 策略的有效性**——是否存在可绕过的宽松配置。

## 检查项

### 1. 指令语义分析

对每条 CSP 指令评估其安全性：

| 指令 | 安全问题 |
|------|---------|
| `'unsafe-inline'` | 允许内联脚本/样式，XSS payload 可直接执行 |
| `'unsafe-eval'` | 允许 eval()、Function()、setTimeout(string) 等动态执行 |
| `*` 或过宽的域名 | 攻击者可从允许的域注入恶意资源 |
| `data:` URI | `script-src` 中允许 data: 可执行任意 JS |
| `blob:` URI | 类似 data:，可构造可执行内容 |
| 缺少 `default-src` | 未显式设置的指令没有 fallback 保护 |
| `script-src` 缺少 nonce/hash | 无法区分合法脚本和注入脚本 |

### 2. 绕过路径分析

- 是否存在 JSONP 端点在允许的域上（可绕过 script-src 白名单）
- 是否存在 Angular/Vue 等框架的模板注入路径（绕过 unsafe-eval 限制）
- `base-uri` 是否受限（未限制可导致 base tag 劫持）
- `object-src` 是否受限（Flash/Java applet 注入）
- `frame-ancestors` 是否设置（替代 X-Frame-Options）

### 3. CSP 报告模式

- 是否仅使用 `Content-Security-Policy-Report-Only`（不实际阻断，仅报告）
- 是否配置了 `report-uri` / `report-to`

## 结论口径

每条 CSP 发现标注：

- CSP 指令原文
- 安全问题描述
- 可能的绕过路径
- 建议的修复指令
- severity（HIGH: unsafe-inline/unsafe-eval on script-src, MEDIUM: 过宽域名, LOW: 缺少可选指令）
