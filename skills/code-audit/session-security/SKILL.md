---
name: session-security
description: >-
  会话 / Cookie / JWT 白盒安全审计——按代码层追踪 session ID 生成强度、Cookie 属性（HttpOnly /
  Secure / SameSite）、登录与注销时的 session 重建与失效、JWT 签名算法与 secret 强度 / 撤销机制、
  "记住我" 长期 token、多租户 / 多用户 session 隔离。
when-to-use: 当项目存在 Session ID 生成、session 生命周期管理、Cookie 安全属性设置时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
---

# 会话 / Cookie / JWT 安全（白盒）

## 1. 触发线索 / 适用信号

按 **代码 pattern + 文件结构 + 依赖**分类。本能力是白盒视角，看代码层 session / cookie / JWT 实现；构造 fixation 攻击 / 篡改 cookie / forge JWT 的动态验证走 `pentest/auth-comprehensive`，二者形成 graybox 互补。

**代码 pattern 维度**（grep 命中模式）：
- 登录端点：`/login` / `/signin` / `/auth/*` / `/oauth/*` / `/sso/*` handler
- 注销端点：`/logout` / `/signout` handler + `session.invalidate()` / `req.session.destroy()` / `session_destroy()` 调用
- Cookie 设置：`Set-Cookie` 响应头构造、`new Cookie(name, value)` / `res.cookie(...)` / `setcookie(...)` / `response.set_cookie(...)`
- Session ID 生成：自实现 `UUID.randomUUID()` 当 token、`Math.random()` / `currentTimeMillis()` 拼接、`SecureRandom.nextBytes(...)`、`crypto.randomBytes(n)`
- JWT 调用：`jwt.sign(...)` / `Jwts.builder()` / `pyjwt.encode(...)` / `golang-jwt` 的 `token.SignedString(...)`
- "记住我"：`remember_me` / `remember_token` / `persistent_login` 字段或表

**文件位置 / 命名约定维度**：
- Spring Security：`WebSecurityConfig*.java` / `SecurityConfig*.java` 的 `sessionManagement()` 链；`*JwtUtil.java` / `*TokenService.java`
- Express：`app.js` / `server.js` 含 `app.use(session(...))`；自定义 `middleware/auth*.js`
- Django：`settings.py` 的 `SESSION_*` / `CSRF_*` 项；`SESSION_ENGINE` 配置
- Laravel：`config/session.php`、`app/Http/Middleware/EncryptCookies.php`、`config/auth.php`
- 自实现：`*AuthFilter.java` / `*AuthInterceptor.java` / `tokenService.{js,ts}`

**依赖 / 注解维度**：
- `pom.xml` 含 `spring-security-web` / `spring-session-*` / `io.jsonwebtoken:jjwt-*` / `com.auth0:java-jwt`
- `package.json` 含 `express-session` / `cookie-session` / `jsonwebtoken` / `passport*`
- `requirements.txt` 含 `django` / `flask-session` / `pyjwt` / `authlib`
- `composer.json` 含 `laravel/framework`（自带 session）/ `firebase/php-jwt`
- `go.mod` 含 `github.com/golang-jwt/jwt` / `github.com/gorilla/sessions`

业务命名（如 `LoginController.doLogin`）只作粗筛——sink 语义相同（Cookie 写入 / Token 生成 / session 失效）就是审计候选。

---

## 2. 造成原因

source 是登录成功事件 / 服务端发起的 token 生成调用 / 会话标识写入流；sink 是浏览器侧持久化的 Cookie（含 session ID / JWT / remember token）、服务端 session store 中的记录、客户端可读的 token 载荷。

**任何 source 在 sink 落地时缺失关键属性 / 弱化关键算法 / 漏掉关键生命周期管理，就构成本类漏洞**——攻击者可利用这种缺失重放会话 / 接管账号 / 横向跨租户。具体成因谱系：

- **弱 session ID**：`Math.random()` / `currentTimeMillis()` / 自增 ID 可被枚举或预测，攻击者直接构造合法 session
- **Cookie 属性缺失**：缺 `HttpOnly` 则 XSS 可读 token；缺 `Secure` 则 HTTP 抓包；缺 `SameSite` 则跨站请求自动带 cookie 引发 CSRF
- **session 固定（fixation）**：登录前后未 regenerate session ID，攻击者提前预置一个 session ID 让受害者登录后继续使用该 ID
- **注销 / 改密未失效**：服务端 session 未真正销毁、JWT 无撤销表，注销后 token 仍可使用
- **弱 JWT**：`alg: none` 接受 / HS256 + 弱 secret / 无 `exp` 或过长、无 `jti` 黑名单——可伪造或泄露后无法吊销
- **"记住我" 永久 token**：长期凭证缺过期 / 缺撤销 / 单设备绑定，泄露代价无限放大
- **多租户 session 串号**：session store 共享但缺 `tenant_id` / `org_id` 维度校验，登录用户 A 可被串到用户 B 的上下文

预编译参数、算法白名单、Cookie 三属性、登录后 regenerate、注销时清服务端 session、JWT 撤销表、租户字段隔离——这些是白盒判定的默认基线。

---

## 3. 领域 source-sink 数据流模型

**代码层 source 集合**：
- 登录成功事件：`AuthenticationSuccessHandler.onAuthenticationSuccess(...)` / `passport.authenticate` 成功分支 / Django `login(request, user)` 调用 / Laravel `Auth::login(...)` 调用
- Token 生成调用：`UUID.randomUUID()` / `SecureRandom.nextBytes(...)` / `crypto.randomBytes(n)` / `secrets.token_urlsafe(n)` / `jwt.sign(payload, secret, opts)` 的返回值
- Cookie 写入构造：`new Cookie(name, value)` 后 `setHttpOnly` / `setSecure` / `setPath` 等链式调用、`res.cookie(name, value, opts)` 的 opts 字面量、`response.set_cookie(...)` 的 kwargs、`setcookie(...)` 的位置参数
- "记住我" 持久 token：写入 `remember_tokens` 表 / `users.remember_token` 字段 / Redis `persistent_login:*` key

**代码层 sink 集合**：
- HTTP 响应头：`Set-Cookie: ...` 经框架 helper（`addCookie` / `res.cookie`）或手写头部
- 服务端 session store：`HttpSession.setAttribute(...)` / `req.session.x = ...` / `request.session['x'] = ...` / `session(['x' => y])`、远程 store（Redis / Memcached）`SET session:<id> <data>`
- JWT 签发：`jwt.sign(...)` / `Jwts.builder().signWith(key, alg).compact()` / `golang-jwt` 的 `token.SignedString(key)` 返回的 token 串
- 后续读取：鉴权中间件从 Cookie / Header 取 token 后 `verify` / `parse` / store lookup

**数据流追踪规则**：
- **Cookie / Token 全生命周期**：生成（source）→ 设置到响应（sink-write）→ 浏览器回传 → 服务端解析与校验（sink-read）→ 失效（logout / 改密 / 过期），每个状态切换点都要单独审。
- **"登录" 与 "注销" 两个状态转换点单独追踪**：登录是否 regenerate session ID？注销是否真清服务端 session + 撤销表写入？
- **跨函数追踪**：source 流到 Cookie 设置函数 / token 工具类 / session store 包装层时，追到底层是否落齐属性 / 算法 / 过期。
- **多租户隔离追踪**：从登录得到 user_id → session 写入 → 后续鉴权读取 session → tenant 维度是否在每次读取时被校验。
- **闭源依赖与远端 store**：依赖库内部细节（如 SSO 客户端、Spring Session-Redis 实现）/ 远端 store（Redis / Memcached）/ Vault 注入的 secret 走 §11 静态分析边界处置。

---

## 4. 常见类型

| 类型 | 静态识别特征 | 白盒识别难点 |
|---|---|---|
| **session fixation** | 登录成功路径不含 `changeSessionId()` / `session.invalidate()` 后重新签发 | 框架默认有时已开（Spring Security 默认 `migrateSession`），自实现 SessionRegistry 易绕过默认 |
| **session hijacking 助攻** | Cookie 缺 `HttpOnly` / `Secure` / `SameSite` | Express / Django 默认部分属性是开的，混淆默认值与显式配置 |
| **弱 session ID** | `Math.random()` / `Random` 非 `SecureRandom` / 时间戳拼接 | 工具方法包装后看起来"像 UUID"但底层不是 CSPRNG |
| **Cookie 属性缺失** | `Set-Cookie` 无 `HttpOnly` / `Secure` / `SameSite=` 字段 | 反代 / CDN / 框架中间件可能改写属性 |
| **JWT `alg: none`** | 鉴权 verify 时未强制断言算法 / 用 `decode` 而非 `verify` | 老版本 PyJWT 默认接受 none；自实现 verify 漏校 alg |
| **JWT secret 弱** | secret 是字面量短字符串 / 默认值 / `"secret"` / `"changeme"` | 来自 `application.yml` 但占位符未替换；环境变量 fallback 是弱默认 |
| **JWT 无撤销** | 无 `jti` 黑名单 / 无短过期 + refresh 双 token | 看似有撤销但实际只清客户端 cookie，服务端未拉黑 |
| **"记住我" 永久 token** | `remember_token` 字段无 `expires_at` / 无清理任务 | 数据库设计层看似正常，实际逻辑层从不过期 |
| **注销不失效** | logout handler 只 `res.clearCookie` 不删服务端 session / 不加 jti 黑名单 | 多端注销 / 强制下线场景缺失 |
| **多租户 session 串号** | session store 共享但 key 不含 tenant 维度 / 鉴权读 session 时未校 tenant_id | 多角色同账号体系下默认 tenant 漂移 |

---

## 5. 入口点定位

按项目结构找 session / cookie / JWT 的 source 与 sink 位置。

> 下列框架 / 项目类型仅作类似项目示例，不限于此；以目标实际栈为准。

### Java / Spring 项目

- `WebSecurityConfig*.java` / `SecurityConfig*.java` 的 `http.sessionManagement(...)` / `sessionFixation(...)` / `maximumSessions(...)` 配置链
- Spring Session：`spring-session-data-redis` 依赖 + `@EnableRedisHttpSession` / `RedisIndexedSessionRepository` 配置
- 登录入口：`AuthenticationSuccessHandler` / `LoginController` / `UsernamePasswordAuthenticationFilter` 子类；注销：`LogoutSuccessHandler` / `LogoutHandler`
- JWT 工具类：`*JwtUtil.java` / `*JwtProvider.java` / `*TokenService.java`，看 `Jwts.builder()` / `Jwts.parserBuilder()`
- 自实现：`*AuthFilter.java` / `*AuthInterceptor.java` 里手写 `new Cookie(...)`、`response.addCookie(...)`

### Node.js / Express 项目

- `app.js` / `server.js` 含 `app.use(session({...}))` / `app.use(cookieParser(...))`
- `express-session` 配置对象的 `cookie: { httpOnly, secure, sameSite, maxAge }` 字面量
- 登录：`routes/auth/login.js` / passport strategy 回调；注销：`routes/auth/logout.js` 看 `req.session.destroy(...)`
- JWT 工具：`utils/jwt.js` / `services/tokenService.js`，看 `jsonwebtoken.sign` / `verify`
- `package.json` 看 `express-session` / `cookie-session` / `jsonwebtoken` 版本

### Python / Django 项目

- `settings.py` 看 `SESSION_ENGINE` / `SESSION_COOKIE_*` / `CSRF_COOKIE_*` / `SESSION_COOKIE_AGE` / `SESSION_EXPIRE_AT_BROWSER_CLOSE`
- `urls.py` 找 `LoginView` / `LogoutView` / 自定义 auth view
- 自定义 session backend：`django.contrib.sessions` vs Redis / DB / 文件
- Flask：`app.config['SESSION_*']`、`flask_session` 配置；登录注销在 view function 里 `session['x'] = ...` / `session.clear()`

### PHP / Laravel 项目

- `config/session.php`：`driver` / `lifetime` / `expire_on_close` / `encrypt` / `cookie` / `path` / `domain` / `secure` / `http_only` / `same_site`
- `config/auth.php`：guards / providers
- `app/Http/Middleware/EncryptCookies.php`：哪些 cookie 跳过加密
- 登录：`Auth::login(...)` / `Auth::attempt(...)`；注销：`Auth::logout()`
- JWT：`firebase/php-jwt` 或 `tymon/jwt-auth`

### Go 项目

- `gorilla/sessions` / `gin-contrib/sessions` 中间件配置：`store.Options.HttpOnly` / `Secure` / `SameSite`
- `golang-jwt/jwt` 调用点：`jwt.NewWithClaims(...)` + `token.SignedString(secret)`；解析：`jwt.Parse(...)` 看 keyfunc 是否校验 `token.Method`

### 通用建议

- 优先从登录 / 注销 handler 入手，倒推到 session ID 生成、Cookie 设置、JWT 签发三类 sink
- 用 `sast-scan` / `dataflow-analysis` 加速跨函数追踪
- 远程 session store / Vault 注入的 secret / SSO / OIDC 委托走 §11 静态分析边界

---

## 6. 跨框架代码变体

| 框架 / 方面 | 安全形态 | 危险形态 |
|---|---|---|
| **Spring Security session fixation** | `http.sessionManagement(s -> s.sessionFixation().migrateSession())`（默认） | `sessionFixation().none()` 或自实现 SessionRegistry 未触发重建 |
| **Spring Security Cookie 三属性** | `serverHttp.headers(...).contentSecurityPolicy(...)` + Spring Session `cookie.setSecure(true)` + `setHttpOnly(true)` + `setSameSite("Strict")` | 自实现 `new Cookie(...)` 不链 `setHttpOnly(true)` / `setSecure(true)` |
| **Spring 登录重建** | `request.changeSessionId()` 或 Spring Security 自动迁移 | 复用同一 session ID，无 invalidate 也无 changeSessionId |
| **Express express-session** | `session({ cookie: { httpOnly: true, secure: true, sameSite: 'strict', maxAge: 3600000 }, resave: false, saveUninitialized: false, rolling: true })` | 缺属性 / `secure: 'auto'` 在 prod 反代后失效 / `saveUninitialized: true` 给匿名也建 session |
| **Express 登录重建** | `req.session.regenerate(cb)` 后再写 `req.session.user = ...` | 直接 `req.session.user = ...`，复用同一 sid |
| **Django Cookie 三属性** | `SESSION_COOKIE_HTTPONLY = True` + `SESSION_COOKIE_SECURE = True` + `SESSION_COOKIE_SAMESITE = 'Strict'` + `CSRF_COOKIE_SECURE = True` | 缺项（HttpOnly 默认 True 但 Secure / SameSite 默认非严格） |
| **Django 登录重建** | `django.contrib.auth.login(request, user)` 自动 `request.session.cycle_key()` | 自实现 view 直接 `request.session['user_id'] = ...` 漏 cycle_key |
| **Laravel session 配置** | `config/session.php`：`'secure' => true`、`'http_only' => true`、`'same_site' => 'lax' \| 'strict'`、`'encrypt' => true` | `'secure' => false` 或 `'same_site' => null` |
| **Laravel 登录重建** | `Auth::login(...)` 默认 `regenerate()` session ID | 自实现 guard 漏 `session()->regenerate()` |
| **JWT 签名算法** | `RS256` / `ES256` 公私钥分离 + 强随机密钥；或 `HS512` + 长 secret + Vault 注入 | `HS256` + 短 / 默认 / 硬编码 secret；接受 `alg: none` |
| **JWT verify 强制算法** | `Jwts.parserBuilder().verifyWith(key).requireIssuer(...).build().parseSignedClaims(token)` / `jwt.verify(token, secret, { algorithms: ['RS256'] })` | `jwt.decode(token)` 不 verify / `jwt.verify(token, secret)` 不传 algorithms |
| **JWT 撤销** | 短 access token（≤15min）+ refresh token + `jti` 黑名单表 | 长 token + 无撤销表 + 注销只清客户端 cookie |
| **session ID 随机性** | `SecureRandom` / `crypto.randomBytes(32)` / `secrets.token_urlsafe(32)` | `Math.random()` / `new Random()` / `currentTimeMillis()` / 自增 ID / MD5(user_id+ts) |
| **"记住我" token** | 长 token 随机生成 + DB 存哈希 + `expires_at` + 单设备绑定 + 注销清除 + 旋转 | 字段写入但无 `expires_at`、无清理任务、无单设备绑定 |
| **多租户 session** | session key 或值含 `tenant_id`，鉴权中间件每次读 session 时校 `session.tenant_id == request.tenant_id` | session 共享 store 无 tenant 维度，跨子域 / 跨子系统串号 |

ORM / 框架特殊点：
- 反向代理（Nginx / CloudFront / Cloudflare）会改写 / 剥离 Cookie 属性，应用层 `secure: true` 在 HTTP 接入时可能被弱化——需独立确认链路
- Spring Session-Redis 把 session 写到 Redis，本地 `HttpSession` API 仍可用但 store 安全性依赖 Redis 配置（鉴权 / TLS）

---

## 7. 思考检查点

加载本能力时按下列问题按 sink 语义思考：

- session ID（或 token）是不是 CSPRNG 生成？看到的是 `SecureRandom` / `crypto.randomBytes` 还是 `Math.random` / `Random` / 时间戳？
- Cookie 三属性（HttpOnly + Secure + SameSite）是否齐全？默认值靠不靠得住（框架默认 vs 显式配置）？
- 登录成功后 session ID 是否 regenerate？fixation 防护是开还是关？
- 注销是否真清服务端 session、写撤销表？还是只 `clearCookie` 让客户端"忘记"？
- JWT 是否有撤销机制（短过期 + refresh + jti 黑名单）？verify 时是否强制算法白名单防 `alg: none`？
- 多租户 / 多角色场景，session 里 tenant / org 维度是否在每次读取时都被强校验？

---

## 8. 检测方法论 / 数据流追踪

> 本能力只到 `static-confirmed`——动态利用证据（实际重放 / 实际伪造 / 实际跨租户接管）走 `pentest/auth-comprehensive`。本节描述白盒方法论，不规定 plan / step 编排。

### Step 0：基线侦察

- 加载 [project-framework-analysis](../project-framework-analysis/SKILL.md) 的项目结构 / 依赖图谱
- 识别 web 框架与 session / auth 库版本，按 §5 在扫描面里列出登录 / 注销 / 鉴权三类入口位置
- 如已跑过 [sast-scan](../sast-scan/SKILL.md)，先看 `needs_dataflow_confirmation` / `high_confidence` 桶里跟 session / cookie / jwt 相关候选

### Step 1：session ID 生成代码与随机性来源

```bash
rg -n 'SecureRandom|crypto\.randomBytes|secrets\.token_|UUID\.randomUUID|Math\.random|new Random\(|currentTimeMillis' --type-add 'src:*.{java,kt,js,ts,py,php,go}' -t src
rg -n 'sessionId|session_id|generateToken|generate_token' --type-add 'src:*.{java,kt,js,ts,py,php,go}' -t src
```

对每个生成点追到底层调用，区分是 CSPRNG 还是弱随机。参见 [references/weak-session-id-generation.md](references/weak-session-id-generation.md)。

### Step 2：Cookie 设置代码与属性配置

```bash
# Cookie 设置点
rg -n 'new Cookie\(|res\.cookie\(|response\.set_cookie|setcookie\(|http\.SetCookie' --type-add 'src:*.{java,kt,js,ts,py,php,go}' -t src
# Spring / express-session / Django / Laravel 配置点
rg -n 'sessionManagement|sessionFixation|express-session|cookie-session|SESSION_COOKIE_|CSRF_COOKIE_|config/session\.php'
```

对每个 sink 看 HttpOnly / Secure / SameSite 三属性是否齐全 + Path / Domain 是否最小化。参见 [references/cookie-security-misconfiguration.md](references/cookie-security-misconfiguration.md)。

### Step 3：登录成功路径与 session 重建

```bash
rg -n 'changeSessionId|session\.invalidate|cycle_key|session_regenerate_id|req\.session\.regenerate|session\(\)->regenerate' --type-add 'src:*.{java,kt,js,ts,py,php,go}' -t src
```

从登录 handler / `AuthenticationSuccessHandler` / passport callback / `Auth::login` 调用点出发，确认登录成功后是否触发 session ID 重建。参见 [references/session-fixation-missing-regeneration.md](references/session-fixation-missing-regeneration.md)。

### Step 4：注销 / 改密路径与 token 失效

- 注销 handler：服务端是否真 `invalidate` / `destroy` / `cycle_key` + 是否写 JWT 撤销表（jti 黑名单）+ 远端 store 删 key
- 改密 / 权限变更：是否同步失效旧 session / 旧 token
- 多端登录场景：是否提供"踢掉其他设备" / 强制下线

### Step 5：JWT 配置审计

```bash
rg -n 'jwt\.sign|jwt\.verify|jwt\.decode|Jwts\.builder|Jwts\.parserBuilder|SignedString|jwt\.encode' --type-add 'src:*.{java,kt,js,ts,py,php,go}' -t src
```

每个签发点核：算法（`HS256` / `RS256` / `none`）+ secret 来源（字面量 / env / Vault）+ `exp` / `iat` / `jti` 字段；每个 verify 点核：是否传 `algorithms=` 白名单防 `alg: none`、是否查 jti 黑名单。

### Step 6：多租户 session 隔离 + "记住我" / 长期 token

- session key / value 是否含 `tenant_id` / `org_id`；鉴权中间件读 session 时是否拿 `request.tenant_id` 与 `session.tenant_id` 做强相等比较；store 是否按 tenant 隔离 namespace
- "记住我" 字段 / 表：`expires_at` 是否存在 + 是否有清理任务（cron / GC）+ 是否含 `device_id` / `user_agent` / `ip` 绑定 + 注销 / 改密时是否同步清 remember token

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

**session ID 强度**：
- [ ] session ID 用 CSPRNG 生成（`SecureRandom` / `crypto.randomBytes` / `secrets.token_urlsafe`）
- [ ] 不出现 `Math.random` / `new Random()` / `currentTimeMillis()` / 自增 ID 作为 token 源
- [ ] 自定义 session ID 熵 ≥ 128 bit
- [ ] 自定义生成逻辑未对底层 CSPRNG 做截断 / 哈希弱化

**Cookie 属性**：
- [ ] 会话 Cookie 设 `HttpOnly`
- [ ] 会话 Cookie 设 `Secure`（HTTPS 强制）
- [ ] 会话 Cookie 设 `SameSite=Strict` 或 `Lax`，跨站场景显式说明为何 `None`
- [ ] `Path` / `Domain` 最小化（不写 `Domain=.example.com` 泄露子域）
- [ ] 反代 / CDN 链路对属性的改写已独立确认

**session 生命周期**：
- [ ] 登录成功后 regenerate session ID（fixation 防护）
- [ ] 权限提升时（匿名 → 已认证、普通 → 管理员）同步 regenerate
- [ ] 不接受客户端预置的 Session ID
- [ ] session 超时（idle + absolute）配置合理（不宜过长）
- [ ] 注销服务端真清 session（`invalidate` / `destroy` / `cycle_key`），不仅清客户端 cookie
- [ ] 改密 / 关键操作同步失效旧 session
- [ ] 并发 session 控制策略明确（允许多端 / 单点登录）

**JWT 安全**：
- [ ] 签名算法是 `RS256` / `ES256` / `HS512`，不是 `HS256` + 弱 secret
- [ ] 不接受 `alg: none`，verify 时显式传 `algorithms=` 白名单
- [ ] secret 来自环境变量 / Vault，不在源码字面量、不是默认占位
- [ ] 含 `exp` 字段，过期时间合理（access token ≤ 15min + refresh token）
- [ ] 有撤销机制（`jti` 黑名单 / 服务端 session lookup）
- [ ] 注销 / 改密时把 token 加入撤销表

**多租户 / 长期 token**：
- [ ] 多租户 session 含 `tenant_id` 维度，鉴权每次读取强校
- [ ] session store key 按 tenant 隔离 namespace
- [ ] "记住我" token 有 `expires_at` + 清理任务 + 单设备绑定 + 注销清除

---

## 9. 闭环要求（必须遵守）

> 闭环判定（confirmed / suspected / not_vulnerable）/ 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：产物结构是下游机器消费的接口，聚合 / 省略会让 `result-with-file` 计数闸门失效，也让 `pentest/auth-comprehensive` 在 graybox 阶段无法回溯到具体 file:line。

### 白盒判定上限

本能力作为白盒原子能力，判定上限为 `static-confirmed`（属性 / 算法 / 流程缺漏在代码层证明），**不等于动态 confirmed**。

| 形态 | 上限状态 | 升级路径 |
|---|---|---|
| Cookie 属性缺失 / 弱 session ID / `alg: none` 接受 / 缺 regenerate | `static-confirmed` | `pentest/auth-comprehensive` 构造 fixation 攻击 / 篡改 cookie / forge JWT 取得可观测效果证据后升 `confirmed` |
| 注销不失效 / "记住我" 永久 token | `static-confirmed` | 黑盒端实测旧 token 仍可访问授权资源后升 `confirmed` |
| session 存储 / secret 来源 / SSO 委托追到能力边界 | `static-unknown` | 见 §11，标注原因，不能默认为 not_vulnerable |
| 已参数化 / 算法白名单 / 三属性齐全 / 登录后 regenerate | `not_vulnerable` | 直接落 `status=not_vulnerable` |

**禁止**白盒独立判 `confirmed`——无可观测效果证据，仅静态缺漏不构成动态利用。

### 产物契约（必须遵守）

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/session-security.jsonl`，不等汇总阶段回头整理（why："事后总结"是聚合 / 区间 / "等"省略的根源）。

字段约束：
- `id` 带 `session-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒默认 `needs_review`）
- `confidence` 取 `static-confirmed` / `static-unknown` / `not-vulnerable` 之一
- `(file, defect_type, scope)` **三元组任一不同即各自独立成行**——禁止把"5 处 cookie 缺 HttpOnly + 3 处缺 Secure"合并成一条
- `defect_type` 取本节涉及的具体语义（`weak-session-id` / `cookie-missing-httponly` / `cookie-missing-secure` / `cookie-missing-samesite` / `missing-session-regeneration` / `logout-not-invalidating` / `jwt-alg-none-accepted` / `jwt-weak-secret` / `jwt-no-revocation` / `remember-me-no-expiry` / `cross-tenant-session-leak` …）
- `scope` 表示作用域：`endpoint:/login` / `cookie:JSESSIONID` / `jwt-util:TokenService.java`
- `file_location` 填 `file:line`，不留空、不写区间

**禁止**：
- 聚合计数（"全项目 12 处 cookie 缺属性"）——丢失具体位置，下游无法消费
- "等" / "..." / "（略）" 省略 finding
- 用一条 finding 覆盖多个 defect_type / 多个 scope

### 反例义务（必须遵守）

> **why**：白盒"已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统会话安全已达标"。

写"未发现会话相关缺陷"或"已防护"前，产物必须包含：
- 所有登录 / 注销 / 鉴权入口完整清单（grep 覆盖证据）
- 所有 Cookie 设置点 + 三属性核验结论
- 所有 JWT 签发 / verify 点 + 算法白名单核验结论
- 远程 session store / Vault secret / SSO 委托位置的 `static-unknown` 原因
- 多租户场景下 tenant 维度核验结论（每个 session 读取点）

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：Spring Security 默认已开 session fixation 防护**

- 抽象规则：Spring Security 未显式配置 `sessionFixation()` 时默认是 `migrateSession`，登录时自动重建 session
- 具体场景：项目用 `@EnableWebSecurity` + 默认 `SecurityFilterChain`，扫描没看到 `changeSessionId()` 显式调用
- 关键识别特征：依赖含 `spring-security-web` 且未显式 `sessionFixation().none()` / `.newSession()` / 自实现 SessionRegistry
- 排除方法：核对 Spring Security 版本对应的默认策略文档，确认是 `migrateSession` 后归 `not_vulnerable`

**反例 2：express-session 默认 HttpOnly = true**

- 抽象规则：`express-session` cookie 选项的 `httpOnly` 默认就是 `true`
- 具体场景：配置只写 `app.use(session({ secret: ..., cookie: { maxAge: 3600000 } }))`，没显式 `httpOnly`
- 关键识别特征：未显式 `cookie.httpOnly = false`，依赖版本未魔改默认
- 排除方法：归 `not_vulnerable`，但其他属性（`secure` / `sameSite` 默认不开）仍要独立审

**反例 3：Django 默认 SESSION_COOKIE_HTTPONLY = True**

- 抽象规则：Django 设置 `SESSION_COOKIE_HTTPONLY` 默认 True、`SESSION_COOKIE_AGE` 默认两周
- 具体场景：`settings.py` 没写 `SESSION_COOKIE_HTTPONLY`
- 关键识别特征：默认值未被覆写
- 排除方法：HttpOnly 归 `not_vulnerable`；但 `SESSION_COOKIE_SECURE` / `SESSION_COOKIE_SAMESITE` 默认不严格，必须独立看

**反例 4：JWT 看似 HS256 但 secret 实际从 env + Vault 注入**

- 抽象规则：源码里写 `Jwts.builder().signWith(secretKey, HS256)` 看似弱，但 `secretKey` 是 64 字节高熵随机串从 Vault 注入
- 具体场景：`@Value("${jwt.secret}")` + `application.yml` 含占位符 `${JWT_SECRET}` + 部署时 Vault Sidecar 注入
- 关键识别特征：secret 不是字面量字符串，从配置链来；占位符未在源码 hardcode 默认
- 排除方法：标 `static-unknown` 推 §11 静态分析边界处置（追配置链直至 Vault 出口），不直接判 confirmed-static

### FN（看似不命中实际是真洞）

**反例 5：Spring 自实现 SessionRegistry 漏配 fixation 防护**

- 抽象规则：自实现 `SessionRegistry` / `AuthenticationSuccessHandler` 覆盖默认行为，但未触发 session 重建
- 具体场景：自定义 `SsoAuthenticationSuccessHandler` 直接 `request.getSession().setAttribute("user", user)` 复用旧 sid
- 关键识别特征：项目有自定义 success handler + 调用链不含 `changeSessionId()` / `invalidate()`
- 确认方法：跨函数追登录入口到 session 写入，确认 sid 是否被重建

**反例 6：express-session `secure: 'auto'` 在反代后失效**

- 抽象规则：`secure: 'auto'` 依赖 `req.secure`，反代未传 `X-Forwarded-Proto` 时 prod 也判 false → cookie 实际不带 Secure
- 具体场景：`app.set('trust proxy', 0)` + Nginx 反代 + `cookie: { secure: 'auto' }`
- 关键识别特征：`trust proxy` 未开 + `secure` 不是显式 true
- 确认方法：标 `static-confirmed` + 备注"需结合部署链路确认"，推黑盒抓 `Set-Cookie` 验证

**反例 7：老版本 PyJWT `decode` 默认接受 `alg: none`**

- 抽象规则：PyJWT < 2.0 `jwt.decode(token, key, verify=False)` 默认不强校算法
- 具体场景：`jwt.decode(token, key)` 没传 `algorithms=`
- 关键识别特征：未传 `algorithms=['RS256', ...]`；依赖版本 < 2.0
- 确认方法：标 `static-confirmed` + 备注版本

**反例 8："记住我" token 写库但缺过期清理**

- 抽象规则：DB 含 `remember_token` 字段但无 `expires_at` + 无清理 cron
- 具体场景：登录写入 token、设置 cookie `Max-Age=315360000`（10 年）、用户表里 `remember_token` 一直不动
- 关键识别特征：迁移文件 / model 无 `expires_at`；无定时任务清理 stale token
- 确认方法：标 `static-confirmed`

**反例 9：多租户 session 共享 store 缺 tenant_id 校验**

- 抽象规则：session value 含 user_id 但无 tenant_id，鉴权中间件只校 user_id
- 具体场景：SaaS 多租户系统，user A 属 tenant 1，但 session 不写 tenant 维度，跨子域 cookie 让 user A 在 tenant 2 的子系统也能登
- 关键识别特征：session 写入 / 读取代码不出现 `tenant_id` / `org_id`
- 确认方法：标 `static-confirmed` 推 graybox 实测

### 易混淆案例

**反例 10：闭源 SSO / OIDC 委托背后的 session 管理**

- 抽象规则：项目鉴权委托给 Okta / Auth0 / 自建 SSO，本地代码看不到 session ID 生成与撤销
- 具体场景：`spring-security-oauth2-client` / `passport-openidconnect` 处理 token，应用层只拿 access token
- 关键识别特征：登录 redirect 到 IdP，回调拿 code → token；本地无显式 session ID 生成代码
- 排除方法：标 `static-unknown`，标注 SSO 边界；本地能审的只是 token 拿到后如何存储 / 传递

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到代码层 pattern + 配置链追踪为止。

下列情形数据流追到能力边界，必须落 `needs_review` + `confidence=static-unknown`，**不允许**默认为 not_vulnerable：

1. **远程 session store**（Redis / Memcached / DB session）—— 写 / 读 / 失效路径有相当部分在外部服务。处置：审本地的 session key 设计、序列化方式、过期策略，但远端 store 的鉴权 / TLS / 隔离配置看相应配置文件 + 标 unknown。
2. **JWT secret 在 Vault / KMS / env**—— 静态拿不到真实 secret 强度。处置：追"取 secret 的代码路径"（确认是从安全注入还是 fallback 默认值），fallback 默认是弱字符串直接判 `static-confirmed`；纯 Vault 注入标 `static-unknown` + 备注。
3. **SSO / OIDC / SAML 委托**—— token 签发 / 撤销在 IdP 侧。处置：边界处停手，本地只审 token 接入后的存储 / 鉴权链路。
4. **Cookie 经反向代理 / CDN 改写**—— Nginx / CloudFront 可能剥离或添加 cookie 属性。处置：标 `static-unknown` + 备注"需结合部署链路确认"，最终结论交由 graybox。
5. **反射 / 动态分派的鉴权调用**—— Java `Method.invoke` / Python `getattr` 决定调哪个 auth handler。处置：标 `static-unknown` 记录反射点行号。
6. **闭源 / 无源码依赖**—— 三方 jar / dll / so / 闭源 SDK（典型如自研 SSO 客户端）。处置：依赖图谱标 `unknown` 推 [dependency-decompile](../dependency-decompile/SKILL.md)；不能直接判 not_vulnerable。
7. **运行时配置切换**—— dev / prod 不同 `secure` / `sameSite` 取值（profile / feature flag 控制）。处置：每个分支独立审，不能只看 dev 分支结论。
8. **AOP / Filter / Middleware 链**—— Spring `@Aspect` / Express middleware / Django middleware 修改 session / cookie 上下文。处置：列出拦截器实现，独立确认是否引入属性补全或剥离。

**底线**：本能力写"该子系统会话安全已达标"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

与 `pentest/auth-comprehensive` 的边界：白盒看的是代码层 session / cookie / jwt 实现是否合规；构造 fixation 攻击 / 篡改 cookie / forge JWT / 实测旧 token 是否还能访问授权资源等动态验证，是黑盒的事。白盒命中的 `static-confirmed` 候选清单，作为黑盒探测的优先入口。

---

## 12. 修复建议

### 源头治理（首选）

- **session ID 用 CSPRNG**：Java `new SecureRandom().nextBytes(new byte[32])`；Node.js `crypto.randomBytes(32).toString('hex')`；Python `secrets.token_urlsafe(32)`；Go `crypto/rand.Read(buf[:])`。**不要**用 `Math.random` / `Random` / `currentTimeMillis()` / 自增 ID 当 token。
- **Cookie 三属性 + 最小化 Path/Domain 必备**，Express 示例：

  ```javascript
  app.use(session({
      secret: process.env.SESSION_SECRET,
      cookie: { httpOnly: true, secure: true, sameSite: 'strict', maxAge: 3600000 },
      resave: false, saveUninitialized: false, rolling: true,
  }));
  ```

- **登录后 regenerate session ID**：Java `request.changeSessionId()`；PHP `session_regenerate_id(true)`；Python/Django `auth.login(request, user)` 默认 cycle_key；Node.js `req.session.regenerate(cb)`。
- **注销服务端真清 session**：调框架的 `invalidate` / `destroy` / `cycle_key`，不只 `clearCookie`；同步写 JWT 撤销表（jti 黑名单）。
- **JWT 用强算法 + 短过期 + 撤销表**，Java + jjwt 示例：

  ```java
  String token = Jwts.builder().subject(userId)
      .expiration(Date.from(Instant.now().plus(15, ChronoUnit.MINUTES)))
      .id(UUID.randomUUID().toString()) // jti for revocation
      .signWith(privateKey, Jwts.SIG.RS256).compact();
  // verify 时强制算法白名单
  Jwts.parser().verifyWith(publicKey).requireAlgorithm("RS256").build().parseSignedClaims(token);
  ```

- **多租户加 tenant 维度**：session key 形如 `session:<tenant_id>:<sid>`，每次读取时鉴权中间件断言 `session.tenant_id == request.tenant_id`。
- **"记住我" 限时 + 单设备绑定 + 可撤销**：DB 存 token 哈希 + `expires_at` + `device_id` + 注销 / 改密时清除 + 旋转。

### 边界过滤（次选，深度防御）

- HTTPS-only 部署 + HSTS + `Set-Cookie: Secure` 三重锁
- 反代层补齐 cookie 属性（如 Nginx `proxy_cookie_path / proxy_cookie_domain`），但应用层属性仍要齐
- 应用层登录 / 改密 / 注销操作做异常监控（频次 / 跨地登录）

### 兜底拒绝

- session 超时合理（idle 30min + absolute 12h 量级）
- 错误响应不暴露 session / token 内部结构
- 数据库 session store 字段最小权限，避免业务账号能读全表

### 参考

- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP JSON Web Token for Java Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_for_Java_Cheat_Sheet.html)
- [references/weak-session-id-generation.md](references/weak-session-id-generation.md)
- [references/session-fixation-missing-regeneration.md](references/session-fixation-missing-regeneration.md)
- [references/cookie-security-misconfiguration.md](references/cookie-security-misconfiguration.md)
