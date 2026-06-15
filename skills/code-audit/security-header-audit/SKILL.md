---
name: security-header-audit
description: >-
  HTTP 安全响应头白盒审计——对照基线检查 HSTS / X-Frame-Options / X-Content-Type-Options /
  Referrer-Policy / Permissions-Policy / COOP / COEP / CORP / CORS / CSP 声明存在性是否齐全、
  值是否符合最小权限基线、是否覆盖所有响应路径（含 4xx / 5xx）。
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
---

# HTTP 安全响应头白盒审计

## 1. 触发线索 / 适用信号

按 **代码 pattern + 配置文件 + 中间件依赖** 分类（不按业务命名）。

**代码 pattern 维度**（grep 命中模式）：
- `response.setHeader(` / `res.setHeader(` / `ctx.set(` / `resp.Header().Set(`
- Spring Security `.headers().` / `HttpHeaders.add(` / `HttpServletResponse.setHeader(`
- Express `app.use(helmet(` / `helmet.contentSecurityPolicy(` / `helmet.hsts(`
- Django settings `SECURE_HSTS_SECONDS` / `SECURE_BROWSER_XSS_FILTER` / `SECURE_CONTENT_TYPE_NOSNIFF` / `SECURE_PROXY_SSL_HEADER`
- Laravel `app/Http/Middleware/` 自定义 header 中间件 / `header()` helper
- Rails `config.action_dispatch.default_headers` / `config.force_ssl`
- ASP.NET `Response.Headers.Add(` / `app.UseHsts()`

**配置文件 / 服务器维度**：
- `nginx.conf` / `*.conf` 含 `add_header`
- Apache `.htaccess` / `httpd.conf` 含 `Header set` / `Header always set`
- Caddy `Caddyfile` 含 `header` block
- HAProxy `http-response set-header`
- CDN / 边缘节点配置：CloudFront Functions / Cloudflare Workers / Cloudflare Rules / Fastly VCL
- API Gateway：AWS API Gateway `gatewayResponses` / Kong response-transformer / Traefik middleware

**依赖 / 中间件维度**：
- `package.json` 含 `helmet` / `koa-helmet` / `fastify-helmet`
- `pom.xml` 含 `spring-security` / `spring-boot-starter-security`
- `requirements.txt` 含 `django-csp` / `secure` / `django-security`
- `go.mod` 含 `unrolled/secure` / 自定义 header middleware
- `composer.json` 含 `bepsvpt/secure-headers`

**反向信号**（不命中本能力）：
- CSP 策略细节（directive 分解 / `unsafe-inline` / source-list 收敛）→ 转 [csp-audit](../csp-audit/SKILL.md)；本能力只判 CSP 声明存在性 + 与其他 header 联动
- Set-Cookie 的 `Secure` / `HttpOnly` / `SameSite` → 转 session-security 等 cookie 维度 skill
- CORS 过宽配置（反射 Origin / null origin 接受 / 通配符 + Credentials）→ 转 dangerous-config；本能力只看 `Access-Control-Allow-*` 是否声明 + 联动其他 header

---

## 2. 造成原因

source 是 HTTP 响应构造代码——中间件链 / 拦截器栈 / 框架 header 配置 / 静态资源服务器配置 / CDN 边缘规则。sink 是 HTTP 响应输出到浏览器。

**任何防御性安全 header 缺失或值弱化，浏览器就无法兜底拦截对应的客户端攻击**——每个 header 各防御一类具体攻击面：

- **HSTS 缺失** → 中间人可降级到 HTTP（SSL Strip）/ 首次访问无强制 HTTPS
- **X-Frame-Options / `frame-ancestors` 缺失** → 页面被恶意站点 iframe 嵌入（点击劫持）
- **X-Content-Type-Options 缺失** → 浏览器 MIME 嗅探，把上传的 `.txt` 当 HTML/JS 执行
- **Referrer-Policy 缺失** → 跳转外链时 Referer 头泄露完整 URL（含敏感 query / token）
- **Permissions-Policy 缺失** → 嵌入的第三方脚本可调用摄像头 / 麦克风 / 地理位置 / 支付 API
- **COOP / COEP / CORP 缺失** → 跨源资源被恶意 origin embed 实现旁路（如 Spectre 攻击面 / 跨源资源读取）
- **CORS header 过宽** → 跨域凭据被窃取（与 `Access-Control-Allow-Credentials` 组合时尤其危险）
- **X-XSS-Protection 错误开启** → 该 header 已废弃，开启 `1; mode=block` 在旧浏览器会引入反射 XSS 通道

本能力是**静态值审计**——不依赖动态触发即可对照基线判断 header 是否声明、值是否符合最小权限。

---

## 3. 领域 source-sink 数据流模型

**代码层 source 集合**（按"谁可能写响应 header"分类）：
- 应用框架 header 中间件：Spring Security `HeadersConfigurer` / helmet 各子模块 / Django `SecurityMiddleware` / Laravel middleware / Rails `default_headers`
- 自定义拦截器 / 中间件：`OncePerRequestFilter` / Express `app.use((req,res,next)=>{...})` / Gin `gin.HandlerFunc` 写 header / Koa `ctx.set(...)`
- 框架配置项：Django `settings.SECURE_*` / Rails `config.force_ssl` / Spring Boot `server.servlet.session.cookie.*`
- 静态资源服务器：Nginx `add_header` / Apache `Header set` / Caddy `header` block
- 反向代理 / 边缘节点：HAProxy / CloudFront / Cloudflare Workers / API Gateway

**代码层 sink 集合**（响应输出位置）：
- HTTP 响应主体输出：`return Response(...)` / `res.send(...)` / `res.json(...)` / `c.JSON(...)`
- 错误响应路径：异常处理器 / `@ExceptionHandler` / Express `app.use(errorHandler)` / `try/catch` 内的错误 JSON 输出——容易绕过 header 中间件
- 静态资源直出：Nginx / Apache 直接服务静态文件、CDN 直出对象存储

**数据流追踪规则**：
- **中间件链路覆盖**：source 是否在 chain 中所有路径都生效——尤其是 4xx / 5xx 错误响应是否走同一条中间件链
- **多层叠加**：应用层 + Nginx + CDN 至少三层都可能设 header；后层可覆盖（add_header `always` 标志 / CDN 边缘规则）或弱化（前层设了 `max-age=31536000`，后层覆盖成 `max-age=0`）
- **条件分支**：feature flag / A/B 测试 / `if (env == "dev")` 是否会动态关闭 header
- **静态资源 vs 动态路由**：动态路由有 middleware 但静态资源（图片 / JS / CSS）由 Nginx 直出可能漏 header

**闭源依赖 / 边缘节点**不可见时落 `static-unknown`（参 §11）。

---

## 4. 常见类型

按"安全 header 维度 × 缺失 vs 弱化"组织：

| Header | 缺失场景 | 危险配置场景 |
|---|---|---|
| **Strict-Transport-Security** (HSTS) | 未声明；仅声明在 HTTPS 端点但未声明在跳转 endpoint | `max-age=0`（实际禁用）；缺 `includeSubDomains`；缺 `preload` 但已提交 preload list |
| **X-Frame-Options** / CSP `frame-ancestors` | 未声明任何 frame 保护 | `ALLOWALL`（非标准值）；`ALLOW-FROM` 已废弃但仍用；`frame-ancestors *` |
| **X-Content-Type-Options** | 未声明 | 声明非 `nosniff` 值 |
| **Referrer-Policy** | 未声明 | `unsafe-url` / `no-referrer-when-downgrade`（默认值，可能泄露 query） |
| **Permissions-Policy** / Feature-Policy | 未声明 | 未限制摄像头 / 麦克风 / geolocation / payment 等敏感能力 |
| **Cross-Origin-Opener-Policy** (COOP) | 未声明 | 缺 `same-origin`，允许跨源 window.opener 持有引用 |
| **Cross-Origin-Embedder-Policy** (COEP) | 未声明 | 跨源资源加载未受限，无法启用跨源隔离环境 |
| **Cross-Origin-Resource-Policy** (CORP) | 未声明 | 资源可被任意 origin embed（CORS 旁路） |
| **Access-Control-Allow-Origin** (CORS) | 业务确需 CORS 但未声明（功能性问题） | 反射 Origin + `Access-Control-Allow-Credentials: true`（详见 dangerous-config） |
| **Content-Security-Policy** (CSP) | 未声明（本能力只判存在性） | 详细 directive 缺陷 → [csp-audit](../csp-audit/SKILL.md) |
| **X-XSS-Protection** | n/a（已废弃，不声明是正确做法） | 错误开启 `1; mode=block`（在旧 Chrome 引入反射 XSS） |

**联动场景**：
- HSTS + COOP/COEP 组合缺失：跨源隔离失效
- 4xx/5xx 错误响应 header 缺失：错误页可被 iframe 嵌入做钓鱼

---

## 5. 入口点定位

按项目结构定位"哪里在设 response header"。

> 下列框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准。

### Java / Spring 项目

- Spring Security：`WebSecurityConfigurerAdapter.configure(HttpSecurity).headers().*`；新版 `SecurityFilterChain` bean → `.headers(headers -> ...)`
- 自定义拦截器：`@Component implements HandlerInterceptor` / `OncePerRequestFilter`
- `application.yml` 含 `server.servlet.headers.*`
- 错误响应：`@ControllerAdvice` + `@ExceptionHandler` 是否同样走 header filter

### Python / Django 项目

- `settings.py`：`MIDDLEWARE` 含 `SecurityMiddleware`；`SECURE_HSTS_SECONDS` / `SECURE_HSTS_INCLUDE_SUBDOMAINS` / `SECURE_HSTS_PRELOAD` / `SECURE_CONTENT_TYPE_NOSNIFF` / `SECURE_REFERRER_POLICY` / `SECURE_CROSS_ORIGIN_OPENER_POLICY`
- `django-csp`（如装）：`CSP_*` settings
- 自定义 middleware：`response["X-Frame-Options"] = ...`

### Node.js / Express 项目

- `app.use(helmet())` 整体启用（默认开多项）；或细粒度 `app.use(helmet.hsts({...}))`
- 错误处理：`app.use((err,req,res,next)=>{...})` 是否在 helmet 之前注册（顺序错则错误响应漏 header）

### Go / Gin / Echo / Chi 项目

- `unrolled/secure` middleware：`secure.New(secure.Options{...})`
- 自定义 `gin.HandlerFunc`：`c.Header(...)` / `c.Writer.Header().Set(...)`
- 注册顺序：在 recovery / error middleware 之前

### PHP / Laravel / Ruby / Rails

- Laravel：`app/Http/Middleware/` 自定义 SecurityHeaders；`bepsvpt/secure-headers` 包配置
- Rails：`config.action_dispatch.default_headers` / `config.force_ssl`；`secure_headers` gem `config/initializers/secure_headers.rb`

### 静态资源服务器 / 反向代理 / CDN

- **Nginx**：`add_header Strict-Transport-Security "..." always;`（缺 `always` 标志只在 200 响应生效——4xx / 5xx 漏）；`map` block 内的 header 设置
- **Apache**：`Header set` / `Header always set`（同 Nginx `always` 语义）
- **Caddy**：`header { ... }` block；`header_up` / `header_down` for reverse proxy
- **CloudFront**：Function 在 viewer-response 阶段加 header；Response Headers Policy
- **Cloudflare**：Workers 在 fetch event 加 header；Rules → Transform Rules → Modify Response Header
- **API Gateway**：AWS `gatewayResponses` 对默认错误响应单独配；Kong `response-transformer` plugin

### 通用建议

- 多层叠加时：找到**最外层**实际生效配置（一般是 CDN > 反向代理 > 应用），优先看它
- 错误响应路径独立追：4xx / 5xx 漏 header 是高发 FN

---

## 6. 跨框架代码变体

| 框架 | 安全形态 | 危险形态 |
|---|---|---|
| **Spring Security（旧式）** | `.headers().frameOptions().sameOrigin().and().contentTypeOptions().and().httpStrictTransportSecurity().maxAgeInSeconds(31536000).includeSubDomains(true)` | 默认 `.headers()` 未配置具体子项；显式 `.headers().disable()` |
| **Spring Security（Lambda DSL）** | `.headers(h -> h.frameOptions(f -> f.deny()).contentSecurityPolicy(c -> c.policyDirectives("...")))` | 漏调用 `headers(...)` 整段 |
| **Express + helmet（默认）** | `app.use(helmet())` 一次开多个 | `app.use(helmet({contentSecurityPolicy: false, hsts: false}))` 显式关 |
| **Express + helmet（细粒度）** | `app.use(helmet.hsts({maxAge: 31536000, includeSubDomains: true, preload: true}))` | 只 `app.use(helmet.contentSecurityPolicy())` 单项，其他默认全开但实际未启用 helmet 完整套件 |
| **Django settings（强）** | `SECURE_HSTS_SECONDS = 31536000` + `SECURE_HSTS_INCLUDE_SUBDOMAINS = True` + `SECURE_HSTS_PRELOAD = True` + `SECURE_CONTENT_TYPE_NOSNIFF = True` + `SECURE_REFERRER_POLICY = 'strict-origin-when-cross-origin'` | `SECURE_HSTS_SECONDS = 0` / `SECURE_PROXY_SSL_HEADER` 误配置导致 Django 误判 HTTPS |
| **Django middleware 顺序** | `SecurityMiddleware` 在 `MIDDLEWARE` 列表靠前 | `SecurityMiddleware` 缺失或被自定义错误处理 middleware 截断 |
| **Nginx** | `add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;` | 缺 `always`（仅 200 生效，4xx / 5xx 漏）；定义在 `server` 上层但子 `location` 重新 `add_header` 后**外层 add_header 全失效**（Nginx `add_header` 不继承） |
| **Apache** | `Header always set Strict-Transport-Security "max-age=31536000; includeSubDomains"` | `Header set` 不带 `always`（同上） |
| **Caddy** | `header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"` | 配置在错误 site block；reverse proxy 未传递 header |
| **Cloudflare Workers** | viewer-response 阶段统一加 header | 仅对部分路径添加；origin 已设但 worker 覆盖弱化 |
| **AWS API Gateway** | `gatewayResponses` 显式给所有默认错误响应加 header | 仅配置成功路径，DEFAULT_4XX / DEFAULT_5XX 漏 header |
| **Go + unrolled/secure** | `secure.New(secure.Options{STSSeconds: 31536000, STSIncludeSubdomains: true, ContentTypeNosniff: true, FrameDeny: true, ...})` | `IsDevelopment: true`（dev 模式跳过全部 header）误开到生产；中间件注册顺序在 recovery 之后 |
| **Rails** | `config.force_ssl = true` + `secure_headers` gem 完整配置 | `force_ssl = false`；`default_headers` 自定义但漏 HSTS |

**Nginx `add_header` 不继承陷阱**（高发 FN）：
- 外层 `server { add_header X-Frame-Options DENY; }` + 内层 `location /api { add_header Cache-Control no-store; }` → API 路径**丢失** X-Frame-Options。修复：每个 location 重复加，或用 `include` 文件统一。

**helmet 默认配置陷阱**：
- helmet 7.x 默认开 `Cross-Origin-Opener-Policy: same-origin`，可能影响 OAuth 跨源 window 流程——业务侧若关 COOP 需评估
- helmet 历史版本默认 `X-XSS-Protection: 0`（关闭已废弃 header），符合 OWASP 建议；老项目若手动开成 `1; mode=block` 反而引入反射 XSS

---

## 7. 思考检查点

加载本 skill 时按这些问题思考（按 sink 语义而非业务命名）：

- 该 header 是否在**所有响应路径**都生效——不只 200，4xx / 5xx 错误响应是否同样带 header？错误处理中间件是否在 security header middleware 之前注册？
- header 值是否符合**最小权限基线**——HSTS `max-age` 是否足够长（≥ 6 个月）+ `includeSubDomains`？XFO 是 `DENY` / `SAMEORIGIN` 而非自定义 `ALLOW-FROM`？Referrer-Policy 比 `no-referrer-when-downgrade` 严格？
- 是否被**多层叠加**覆盖——应用层设了但 Nginx / CDN 边缘覆盖？或反过来，应用层关了寄希望于 CDN 设但 CDN 未配？
- **静态资源服务器**（Nginx / S3+CloudFront）是否同步设置——动态路由有 header 但 `/static/*.js` 直出可能漏
- **条件分支**：feature flag / dev/prod env / A/B 测试是否会动态关闭 header？dev 环境关 HSTS 是合理设计但**不能误推到生产**

---

## 8. 检测方法论 / 数据流追踪

### Step 0：基线侦察

- 加载 `project-framework-analysis` 输出的项目结构 / 中间件链 / 反向代理配置
- 识别 web 框架（Spring / Django / Express / Gin 等）+ 是否引入 `helmet` / `secure_headers` / `unrolled/secure` / `django-csp` 等专门库
- 列出所有可能写 response header 的位置：中间件 / 拦截器 / 自定义过滤器 / 框架配置 / Nginx / CDN 配置

### Step 1：grep 出 header 设置代码 + 配置文件

```bash
# 应用层 header 写入
rg "setHeader\(|Header\(\)\.Set\(|res\.set\(|ctx\.set\(|response\.headers" --type-add 'web:*.{java,go,py,js,ts,php,rb}'
# Spring Security headers config
rg "\.headers\(\)|HeadersConfigurer|httpStrictTransportSecurity|frameOptions|contentTypeOptions"
# Django SECURE_* 设置
rg "SECURE_HSTS_|SECURE_CONTENT_TYPE_NOSNIFF|SECURE_REFERRER_POLICY|SECURE_BROWSER_XSS|SECURE_PROXY_SSL_HEADER" --type py
# helmet
rg "helmet\(|helmet\." --type js --type ts
# Nginx / Apache / Caddy
rg "add_header|Header (always )?set|^\s*header\s+\w+-\w+" --type-add 'cfg:*.{conf,vcl}' -g 'Caddyfile'
# CDN / edge
rg "Response Headers Policy|gatewayResponses|response-transformer" --type yaml --type json
```

### Step 2：按 header 清单对账

按 §4 表格逐 header 走：
1. 该 header 是否在任一 source 位置被设置？
2. 设置的值是否符合最小权限基线（见 §12）？
3. 设置是否覆盖所有响应路径（含 4xx / 5xx）？
4. 是否有后置中间件 / CDN 规则会覆盖弱化？

### Step 3：静态资源 / 反向代理同步审

- 找 Nginx `server` / `location` block 是否覆盖动态路由与静态资源
- 检查 Nginx `add_header` 继承陷阱：是否每个 `location` 都重复设
- CDN / 边缘节点配置：是否对所有路径生效，含错误页

### Step 4：错误响应路径独立追

- `@ControllerAdvice` / `@ExceptionHandler` 是否走同一 header filter
- Express `app.use(errorHandler)` 是否在 helmet 之后注册
- API Gateway `gatewayResponses` 是否覆盖 `DEFAULT_4XX` / `DEFAULT_5XX`

### Step 5：工具加速

调用 `dataflow-analysis` 跨函数追"哪些 handler 走了哪条 middleware 链"；调用 `sast-scan` 找未覆盖的 header 设置位置。

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

**关键安全 header 存在性 + 值基线**（详细攻击场景参见 [missing-critical-headers.md](references/missing-critical-headers.md)）：

- [ ] `Strict-Transport-Security` 已声明，`max-age ≥ 15552000`（6 个月）且含 `includeSubDomains`
- [ ] `X-Content-Type-Options: nosniff` 已声明
- [ ] `X-Frame-Options: DENY` 或 `SAMEORIGIN`（或 CSP `frame-ancestors` 已覆盖）
- [ ] `Content-Security-Policy` 已声明（细节走 [csp-audit](../csp-audit/SKILL.md)）
- [ ] `Referrer-Policy` 已声明且严于 `no-referrer-when-downgrade`（建议 `strict-origin-when-cross-origin`）
- [ ] `Permissions-Policy` 已声明，限制摄像头 / 麦克风 / geolocation / payment 等敏感能力
- [ ] `Cross-Origin-Opener-Policy: same-origin` 已声明（除非业务需要跨源 window）
- [ ] `Cross-Origin-Embedder-Policy` 已声明（如需启用跨源隔离）
- [ ] `Cross-Origin-Resource-Policy` 已声明（建议 `same-origin` 或 `same-site`）
- [ ] `X-XSS-Protection` 未声明或为 `0`（已废弃，开启反引入反射 XSS）

**CORS header 存在性 + 联动**（CORS 配置风险细节参见 [cors-misconfiguration.md](references/cors-misconfiguration.md)；过宽配置归 dangerous-config）：

- [ ] `Access-Control-Allow-Origin` 不为 `*`（或不与 `Allow-Credentials: true` 组合）
- [ ] 非业务必要不声明 CORS header

**覆盖完整性**：

- [ ] 所有 header 在 4xx / 5xx 错误响应路径同样生效
- [ ] 静态资源服务器（Nginx / CDN）与动态路由的 header 配置一致
- [ ] Nginx `add_header` 继承陷阱已检查（每个 `location` 单独验证）
- [ ] 多层叠加未弱化（应用层 → 反向代理 → CDN）

---

## 9. 闭环要求（必须遵守）

> 闭环判定（confirmed / suspected / not_vulnerable）以 [common/closure-verification.md](../../common/closure-verification.md) 为准。本能力作为白盒原子能力，判定上限为 `static-confirmed`，不等于动态 confirmed。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 `result-with-file` 与 coverage-ledger 的机器消费，按 header 维度聚合或区间表达会让单条 header 缺陷无法回溯到具体响应路径。

### 白盒判定上限

**static-confirmed（白盒上限，落 `status=needs_review`）**：
- 配置层证据证明：(a) header 在某响应路径未声明，或 (b) 声明值弱化（如 `max-age=0` / 缺 `includeSubDomains`）
- 中间件链路 / 配置叠加已追完，未发现后置补救

**static-unknown（落 `status=needs_review` + 标注 unknown）**：
- 动态 header 决策（feature flag / 运行时配置切换）
- A/B 测试两套 header 配置
- 边缘节点 / 闭源 CDN 规则不可见
- 不能默认为 not_vulnerable

**not_vulnerable（落 `status=not_vulnerable`）**：
- 配置层证明该 header 在所有响应路径都声明且值符合基线
- 端点不返回 HTML / 文档资源（如纯 JSON API 部分 header 不适用，需标 `[-] n/a (原因)`）

**升级路径**（白盒不能独立判 confirmed）：
- 走 graybox：用白盒候选指导黑盒端发请求实测响应 header
- 黑盒端按对应 pentest skill 的闭环要求收响应头实测证据
- 实测响应缺失 / 值弱化与白盒结论一致后，升为 `confirmed`

**禁止**白盒独立判 `confirmed`——配置层证据不等于浏览器实际收到的 header（CDN 边缘可能覆盖）。

### 产物契约（必须遵守）

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/security-header-audit.jsonl`：

```json
{
  "id": "secheader-001",
  "title": "HSTS 缺失 includeSubDomains",
  "severity": "medium",
  "cwe": "CWE-693",
  "endpoint_pattern": "/*",
  "header_name": "Strict-Transport-Security",
  "value": "max-age=31536000",
  "deficiency_type": "weak-value-missing-includeSubDomains",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "file_location": "config/nginx.conf:42",
  "source_report": "security-header-audit",
  "description": "..."
}
```

字段约束：
- `(endpoint_pattern, header_name, value, deficiency_type)` **四元组任一不同即各自独立成行**——禁止按 header 聚合或按端点折叠
- `endpoint_pattern` 填该响应路径模式（如 `/api/*` / `/static/*` / `/error/4xx`）；全站统一配置填 `/*`
- `header_name` 填具体 header（如 `Strict-Transport-Security` / `X-Frame-Options`）
- `value` 填当前实际声明值；未声明填 `null` 或 `<missing>`
- `deficiency_type ∈ missing | weak-value-* | wrong-value-* | inconsistent-coverage`，名称带 header 语义后缀
- `file_location` 填 `file:line`，多层叠加时填**实际生效那一层**

**禁止**：
- 按 header 聚合（"HSTS 全站缺失"——未给出每条响应路径覆盖证据）
- 按端点折叠（"该 endpoint 缺多个 header"——拆成每个 header 独立成行）
- 漏掉错误响应路径（4xx / 5xx）的独立覆盖审计

### 反例义务（必须遵守）

> **why**：header 审计的"已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统 header 已合规"。

写"所有安全 header 已合规"前，产物必须包含：
- 每个基线 header 在每条响应路径的覆盖结论（含 4xx / 5xx）
- 多层叠加每层的实际配置文件位置
- 静态资源 vs 动态路由的独立审计结论
- `static-unknown` 单元格（动态决策 / 闭源边缘）的具体原因

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：应用层未设但前置 CDN/反向代理统一注入**

- 抽象规则：应用代码看不到 header 设置不等于响应缺失
- 具体场景：应用是 Spring Boot 裸服务，header 由 Cloudflare Response Headers Policy 统一加；项目仓库内看不到 helmet / Spring `.headers()`
- 关键识别特征：CDN / 反向代理配置仓库独立（infra repo）；应用层 `pom.xml` 无 security 相关依赖但生产实测响应有完整 header
- 排除方法：先看部署架构与 CDN 配置仓库；标 `static-unknown` 推 graybox 端实测，确认后归 not_vulnerable

**反例 2：helmet 配置项已设但项目接 Nginx，Nginx 同步设置**

- 抽象规则：多层叠加最终响应取决于最外层
- 具体场景：Express helmet 设了 HSTS max-age=31536000；Nginx 也设了同样值；不构成弱化
- 关键识别特征：两层值一致
- 排除方法：核对每层实际配置，归 not_vulnerable

**反例 3：DEV 环境关 HSTS**

- 抽象规则：dev 环境关 HSTS 防开发证书污染浏览器是合理设计
- 具体场景：`if (env === 'production') app.use(helmet.hsts({...}))`；dev 不开
- 关键识别特征：条件分支按环境区分；prod 分支有完整配置
- 排除方法：确认 prod 分支配置完整 + 部署管道只走 prod；归 not_vulnerable（dev 缺失合理）

### FN（看似不命中实际是真洞）

**反例 4：helmet 老版本默认 `X-XSS-Protection: 1; mode=block`**

- 抽象规则：helmet 4.x 及更早默认开 X-XSS-Protection；该 header 已废弃且在旧 Chrome 引入反射 XSS 通道
- 具体场景：项目用 helmet 4.x 默认配置，未显式 `helmet.xssFilter(false)`
- 关键识别特征：`package.json` helmet 版本 < 5.0；未显式关闭 xssFilter
- 确认方法：核对 helmet 版本与默认配置文档；升级到 helmet 7.x（默认 `X-XSS-Protection: 0`）

**反例 5：Spring Security 默认 frameOptions DENY 但项目需要合法子域 iframe**

- 抽象规则：默认 DENY 与业务需求冲突，开发者改成 `sameOrigin` 但未配真正需要的跨子域允许
- 具体场景：业务需要 `app.example.com` 嵌 `widget.example.com` 的 iframe；Spring `.frameOptions().sameOrigin()` 不允许跨子域
- 关键识别特征：sameOrigin 设了但前端实测发现 iframe 加载失败 → 开发者后续关闭 frameOptions 或在 Nginx 覆盖成 ALLOWALL
- 确认方法：核对 Spring 配置与 Nginx 是否有 X-Frame-Options 覆盖；推荐改用 CSP `frame-ancestors 'self' https://widget.example.com`

**反例 6：HSTS preload list 已提交但 header 未标 `preload`**

- 抽象规则：preload 提交流程需要 header 同时含 `preload` 关键字
- 具体场景：项目向 hstspreload.org 提交了域名，但 `Strict-Transport-Security: max-age=31536000; includeSubDomains`（缺 `preload`）
- 关键识别特征：preload 实际未生效；后续 preload list 维护方会移除
- 确认方法：核对 preload list 状态 + header 值；补 `preload` 关键字

**反例 7：Cross-Origin-Resource-Policy 缺失导致 CORS 旁路**

- 抽象规则：CORP 缺失时资源可被任意 origin embed，构成 CORS 控制的旁路
- 具体场景：API 返回 JSON 配 CORS `Allow-Origin: https://app.example.com`，但未配 CORP；恶意 origin 可用 `<script src="...">` 触发资源加载侧信道
- 关键识别特征：CORS 已配但 CORP 未配；资源含敏感数据
- 确认方法：核对 CORP header；建议 `same-origin` 或 `same-site`

### 易混淆案例

**反例 8：Nginx `add_header` 不继承导致部分 location 漏 header**

- 抽象规则：Nginx `add_header` 不向子 location 继承，子 location 重新 `add_header` 后外层 add_header 全失效
- 具体场景：`server { add_header X-Frame-Options DENY always; } location /api { add_header Cache-Control no-store; }` → `/api` 路径下**所有外层 add_header 失效**
- 关键识别特征：每个 location block 独立看 add_header；只看 server 层会漏报
- 排除方法：要求每个 location 重复加 header，或用 `include /etc/nginx/conf.d/security-headers.conf` 在每个 location 引入

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。

下面这些情形数据流分析无法继续追踪，**必须标 `static-unknown`**：

1. **CDN / 反向代理边缘规则**——Cloudflare Workers / CloudFront Functions / Fastly VCL / 边缘节点 Transform Rules。代码仓库内看不到边缘配置时必须独立读 infra 仓库 / 控制台导出；不能默认应用层观察等于浏览器实际收到的 header。处置：标 `static-unknown` 记录"边缘配置未审"。

2. **动态 header 决策**——基于 `User-Agent` / 地域 / feature flag / A/B 实验切换 header 值。处置：每个分支独立审计；不能只看一个分支结论。

3. **WebSocket 握手响应 header（HTTP upgrade）**——WebSocket 升级响应不在本能力评估范围（浏览器对 WS 不应用大多数安全 header）。处置：明确标 `本能力 n/a (WebSocket upgrade)`。

4. **CSP 细节策略分析**——directive 分解 / `unsafe-inline` 风险 / source-list 收敛 / nonce 配置 → 全部转 [csp-audit](../csp-audit/SKILL.md)。处置：本 skill 只判 CSP 声明存在性；细节不在本能力范围。

5. **闭源中间件 / 三方 SDK 内部 header 操作**——第三方 SDK 可能在内部修改响应 header。处置：标 `static-unknown` 推 dependency-decompile 反编译验证。

6. **运行时配置中心（Apollo / Nacos / 配置文件热加载）**——header 值由远程配置中心动态下发。处置：读取实际生效配置；标 `static-unknown` 直到配置来源确认。

7. **反向代理保留头（preserve_host / proxy_pass_header）**——代理是否透传 upstream 已设的 header，取决于代理配置。处置：核对代理配置 `proxy_pass_header` / `proxy_hide_header`。

**底线**：写"所有安全 header 已合规"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

---

## 12. 修复建议

### 源头治理（首选）

- **使用框架推荐安全 header 套件**：Express → `helmet`；Spring Security → `.headers().*` 完整配置；Django → `SecurityMiddleware` + `django-csp`；Rails → `secure_headers` gem；Go → `unrolled/secure`
- **最小权限基线**对照 [OWASP Secure Headers Project](https://owasp.org/www-project-secure-headers/) 推荐值
- **统一在最外层（反向代理 / CDN）配置**，避免多层叠加歧义

### 推荐基线值

| Header | 推荐值 |
|---|---|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains; preload`（提交 preload list） |
| `X-Frame-Options` | `DENY`（或用 CSP `frame-ancestors 'self'`） |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin`（或更严 `no-referrer`） |
| `Permissions-Policy` | 显式禁用不需要的能力，如 `camera=(), microphone=(), geolocation=(), payment=()` |
| `Cross-Origin-Opener-Policy` | `same-origin` |
| `Cross-Origin-Embedder-Policy` | `require-corp`（若需启用跨源隔离） |
| `Cross-Origin-Resource-Policy` | `same-origin` 或 `same-site` |
| `Content-Security-Policy` | 见 [csp-audit](../csp-audit/SKILL.md) |
| `X-XSS-Protection` | **不声明**（或 `0`），已废弃 |

### 边界过滤（次选，深度防御）

- WAF 规则补加 header（边缘统一注入）——仅作辅助，不替代应用层配置
- 静态资源由 CDN 服务时，CDN 端配置 Response Headers Policy 统一注入

### 兜底拒绝

- 错误响应路径同步覆盖：API Gateway `gatewayResponses` 对 `DEFAULT_4XX` / `DEFAULT_5XX` 单独配
- Nginx 用 `include /etc/nginx/conf.d/security-headers.conf` 在每个 location 引入，规避 `add_header` 不继承陷阱

### 参考

- [OWASP Secure Headers Project](https://owasp.org/www-project-secure-headers/) — header 基线与推荐值
- [MDN HTTP Headers Security](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers) — 各 header 规范
- [HSTS Preload List](https://hstspreload.org/) — HSTS preload 提交
- 案例参考：[missing-critical-headers.md](references/missing-critical-headers.md) / [cors-misconfiguration.md](references/cors-misconfiguration.md)
