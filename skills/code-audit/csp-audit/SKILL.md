---
name: csp-audit
description: >-
  Content Security Policy 策略静态审计——分解 directive、识别 unsafe-inline / unsafe-eval / 过宽
  source-list / 缺 frame-ancestors，比对最小权限基线。
when-to-use: 当项目设置了 CSP header 或 CSP meta 标签时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
---

# CSP 策略静态审计

## 1. 触发线索 / 适用信号

按"代码 pattern + 配置文件 + 中间件" 三维识别本能力命中场景。**与 frontmatter description 同步**。

**代码 pattern 维度**（grep 命中模式）：
- 响应头直写：`res.setHeader('Content-Security-Policy', ...)` / `response.headers['Content-Security-Policy'] = ...` / `w.Header().Set("Content-Security-Policy", ...)`
- HTML 模板里 `<meta http-equiv="Content-Security-Policy" content="...">`
- 服务端模板渲染 nonce：`<script nonce="${nonce}">` / `<script nonce="{{csp_nonce}}">`

**框架配置 / 中间件维度**：
- Spring Security：`http.headers().contentSecurityPolicy("...")` / `WebSecurityConfigurerAdapter` / `HeadersConfigurer`
- Spring 拦截器：`HandlerInterceptor` / `OncePerRequestFilter` 里写 CSP header
- Express：`helmet.contentSecurityPolicy({...})` / 自定义 middleware 写 header
- Django：`settings.py` 含 `CSP_DEFAULT_SRC` / `CSP_SCRIPT_SRC` / `MIDDLEWARE` 含 `csp.middleware.CSPMiddleware`
- Rails：`config/initializers/content_security_policy.rb` 含 `Rails.application.config.content_security_policy do |policy| ... end`
- Laravel：`app/Http/Middleware/` 含 `header('Content-Security-Policy', ...)`
- ASP.NET：`app.UseCsp(...)` / `NWebsec` 中间件
- Next.js / Nuxt：`next.config.js` 的 `headers()` 函数 / `nuxt.config.ts` 的 `security.headers.contentSecurityPolicy`

**配置文件 / 反向代理维度**：
- nginx：`add_header Content-Security-Policy "..." always;`
- Apache：`Header set Content-Security-Policy "..."`
- Caddy：`header Content-Security-Policy "..."`
- CDN / 边缘节点配置（Cloudflare Workers / AWS Lambda@Edge）写 header

业务命名只作粗筛——只要最终落到 `Content-Security-Policy` header 或 meta 标签的位置就是审计候选。

---

## 2. 造成原因

CSP 设计为浏览器侧的纵深防御层——当应用层过滤失效让 XSS payload 进入页面后，CSP 通过浏览器拒绝执行未授权脚本来兜底拦截。**策略本身弱化、缺失或包含过宽 source-list，让浏览器丧失这一兜底能力**。

具体成因：
- `unsafe-inline`：允许内联 `<script>` 与事件属性（`onclick=...`）执行，注入的 XSS payload 直接生效，CSP 等同未启用
- `unsafe-eval`：允许 `eval()` / `Function()` / `setTimeout(string)` 动态执行字符串，DOM XSS 与模板注入路径不被拦截
- 过宽 source-list（`*` / `https:` / `data:`）：攻击者可托管恶意脚本到匹配域；`data:` 在 `script-src` 里等价于任意脚本执行
- 缺 `frame-ancestors`：浏览器不阻止页面被嵌入恶意 iframe，clickjacking 攻击仍可达
- nonce / hash 未启用：无法区分合法内联脚本与注入脚本，只能依赖 `unsafe-inline` 兜底
- Report-Only 模式部署但无监控：策略不实际阻断且报告无人看，等同没设

任何 source 未经最小权限收敛就被声明到 sink directive，即构成 CSP 弱化——浏览器侧防御层被打穿，应用层一旦出 XSS 没有补救机会。

---

## 3. 领域 source-sink 数据流模型

**代码层 source 集合**（用户可控的脚本拼接点——CSP 是兜底层，source 与 XSS 同源）：
- 服务端模板原样输出：Thymeleaf `th:utext` / Freemarker `${var?no_esc}` / Jinja2 `{{ var | safe }}` / EJS `<%- var %>`
- 客户端动态写入：React `dangerouslySetInnerHTML` / Vue `v-html` / Angular `[innerHTML]` 绑定
- 动态求值：`eval(userInput)` / `new Function(userInput)` / `setTimeout(string)` / `setInterval(string)`
- 内联 `<script>` 拼接用户数据：`<script>var x = "${user}";</script>` 形态
- 配置驱动 CSP 字符串构造：从配置文件 / 环境变量 / 数据库读取 directive 值再拼成 CSP 字符串

**代码层 sink 集合**（每个 directive 是一个独立 sink 维度，**审计单位是 directive 而非 CSP 字符串整体**）：
- `script-src` / `script-src-elem` / `script-src-attr`：内联脚本与外部脚本的来源
- `style-src` / `style-src-elem` / `style-src-attr`：内联与外部样式来源
- `img-src` / `font-src` / `media-src` / `connect-src`：资源加载与 XHR / fetch 目标
- `frame-src` / `child-src` / `frame-ancestors`：iframe 嵌入控制
- `object-src` / `base-uri` / `form-action`：插件 / base 标签劫持 / 表单提交
- `default-src`：未单独设的 directive 兜底
- `report-uri` / `report-to`：策略违规上报通道

**数据流追踪规则**：
- CSP 策略本身有两种形态——**静态字符串字面量**（直接读源码即可分解）和**动态构建**（运行时拼接，需追到生成代码）
- nonce 注入要追两端：CSP header 里 `'nonce-{value}'` 的 value 生成位置 + 模板里 `<script nonce="...">` 的 value 注入位置；两端必须用同一随机源每请求生成
- 跨层追踪：反向代理（nginx / CDN）的 `add_header` 可能覆盖或被应用层 header 覆盖——以**浏览器实际接收顺序**为准
- 框架边界：Spring `HeadersConfigurer` / Express `helmet` 默认值可能被业务代码覆盖；Django `CSP_*` 设置被 view 级 `@csp_update` 装饰器局部修改
- 闭源 / 边缘节点：CDN override 不在白盒范围（参 §11）

---

## 4. 常见类型

CSP 弱化的主流变体（按已知主流覆盖，不追求穷举）：

| 类型 | 静态识别特征 | 白盒识别难点 |
|---|---|---|
| **`unsafe-inline` on script-src** | `script-src` 含 `'unsafe-inline'` 且无 nonce / hash | 易识别；需进一步看是否声明 nonce 但模板未使用 |
| **`unsafe-eval` on script-src** | `script-src` 含 `'unsafe-eval'` | 需评估业务是否真的依赖 eval（如 Angular JIT、模板引擎运行时编译） |
| **过宽 source-list** | `*` / `https:` / `http:` / 含通配子域 `*.example.com` | 通配域下托管的 JSONP / 老旧文件上传可成绕过点（参 §10） |
| **`data:` schema in script-src** | `script-src` 含 `data:` | 等价 `unsafe-inline`，浏览器执行任意脚本 |
| **缺 `frame-ancestors`** | 未声明 `frame-ancestors` 且未配 `X-Frame-Options` | 仅按 default-src 兜底无效，`frame-ancestors` 不 fall back 到 default-src |
| **缺 `object-src 'none'`** | 未显式禁用插件 | Flash / Java applet 已淘汰但部分浏览器仍解析 |
| **缺 `base-uri 'self'`** | 未限制 `<base>` 标签 | 注入 `<base>` 可改变相对路径解析，把脚本指向攻击者域 |
| **Report-Only 部署** | header 用 `Content-Security-Policy-Report-Only` | 策略不实际阻断；若无监控等同未设 |
| **nonce 固定值** | nonce 是常量 / 配置项 / 弱随机源 | 攻击者可猜到 nonce 后注入合法脚本 |
| **nonce 声明但未使用** | header 含 nonce 但模板里没打 nonce | 业务依赖 `unsafe-inline` 兜底，nonce 形同虚设 |

---

## 5. 入口点定位

按项目结构找 CSP 策略声明位置——CSP 通常集中在 1-3 处，定位后可枚举所有 directive。

> 下列框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准。

### Java / Spring 项目

- `*SecurityConfig.java` / `WebSecurityConfigurerAdapter` 子类：`http.headers().contentSecurityPolicy("...")`
- 自定义 `HandlerInterceptor` / `OncePerRequestFilter` 实现里写 header
- `application*.yml` / `*.properties` 含 `spring.security.headers.csp` 类配置
- `pom.xml` 是否含 `spring-security-web` 确认有无内置 CSP 支持

### Node.js / Express / Next.js

- `app.js` / `server.js` / `index.js`：`app.use(helmet.contentSecurityPolicy({...}))`
- 自定义 middleware 文件（`middleware/*.js`）：`res.setHeader('Content-Security-Policy', ...)`
- `next.config.js` / `next.config.mjs` 的 `async headers()` 返回数组里 `Content-Security-Policy`
- `package.json` 看 `helmet` / `@nestjs/helmet` / `next` 版本

### Python / Django / FastAPI

- Django：`settings.py` 含 `CSP_*` 配置项；`MIDDLEWARE` 含 `csp.middleware.CSPMiddleware`；view 级 `@csp_update` / `@csp_replace` / `@csp_exempt`
- FastAPI / Flask：自定义 middleware 写 header；`flask-talisman` 配置块
- `requirements.txt` 看 `django-csp` / `flask-talisman` 版本

### Ruby / Rails

- `config/initializers/content_security_policy.rb`：`Rails.application.config.content_security_policy do |policy| ... end`
- `ApplicationController` 里 `content_security_policy` block 局部覆盖

### PHP / Laravel

- `app/Http/Middleware/` 含写 CSP header 的中间件
- `app/Providers/AppServiceProvider.php` 可能注册全局 header

### 反向代理 / 静态资源服务器

- nginx：`nginx.conf` / `sites-available/*.conf` 含 `add_header Content-Security-Policy "..." always;`
- Apache：`.htaccess` / `httpd.conf` 含 `Header set Content-Security-Policy`
- HTML 模板：`grep -r 'http-equiv="Content-Security-Policy"' templates/ views/ public/`

### 通用建议

- 优先 `rg -i 'content-security-policy' --type-add 'web:*.{html,erb,vue,jsx,tsx,ejs}' --type web -t js -t ts -t java -t py -t rb -t php -t conf` 一次性枚举所有声明位置
- 多层声明（应用层 + 反向代理 + HTML meta）需同时定位——浏览器接收顺序可能让后置声明被忽略
- nonce 注入需同时定位"生成端"（CSP header）和"使用端"（HTML 模板）

---

## 6. 跨框架代码变体

| 框架 / 平台 | 安全形态（最小权限 + nonce） | 危险形态（弱化） |
|---|---|---|
| **Spring Security** | `csp.policyDirectives("script-src 'self' 'nonce-{nonce}'; object-src 'none'; base-uri 'self'")` + 配套 nonce filter | `csp.policyDirectives("script-src 'self' 'unsafe-inline' 'unsafe-eval'")` |
| **Express + helmet** | `helmet.contentSecurityPolicy({ directives: { scriptSrc: ["'self'", (req,res) => `'nonce-${res.locals.nonce}'`], objectSrc: ["'none'"] }})` | `helmet.contentSecurityPolicy({ directives: { scriptSrc: ["'self'", "'unsafe-inline'", "*"] }})` |
| **Express 原生** | `res.setHeader('Content-Security-Policy', `script-src 'self' 'nonce-${nonce}'; object-src 'none'`)` | `res.setHeader('Content-Security-Policy', "default-src *; script-src * 'unsafe-inline'")` |
| **Django (django-csp)** | `CSP_SCRIPT_SRC = ("'self'",)`, `CSP_INCLUDE_NONCE_IN = ('script-src',)` + 模板 `<script nonce="{{ request.csp_nonce }}">` | `CSP_SCRIPT_SRC = ("'self'", "'unsafe-inline'", "'unsafe-eval'")` |
| **Rails** | `policy.script_src :self, "'nonce-#{SecureRandom.base64(16)}'"` + `nonce_generator -> request { SecureRandom.base64(16) }` | `policy.script_src :self, :unsafe_inline, :unsafe_eval, '*'` |
| **Next.js** | `headers()` 返回带 nonce 的 CSP；nonce 通过 middleware 注入 `<Script nonce={nonce}>` | `headers()` 返回 `script-src 'unsafe-inline' 'unsafe-eval'` |
| **nginx** | `add_header Content-Security-Policy "script-src 'self'; object-src 'none'; frame-ancestors 'self'" always;` | `add_header Content-Security-Policy "default-src *"` |
| **HTML meta** | `<meta http-equiv="Content-Security-Policy" content="script-src 'self' 'nonce-{{nonce}}'">` | `<meta http-equiv="Content-Security-Policy" content="default-src *; script-src * 'unsafe-inline'">` |

**通用弱化点**（所有平台都适用）：
- nonce 声明但模板未使用 → 业务靠 `unsafe-inline` 兜底，nonce 形同虚设
- `Content-Security-Policy-Report-Only` 部署但未配 report-uri 或无监控 → 等同未设
- 多层声明冲突（应用层严 + nginx 宽，或反向）→ 以浏览器接收的策略合并规则为准
- view 级覆盖（Django `@csp_update` / Rails controller block）放宽全局策略 → 局部漏洞面

---

## 7. 思考检查点

加载本 skill 时按这些问题思考：

- 每个 directive 是不是最小权限？通配 `*` / `https:` / `data:` 是否有必要，能否收敛到具体域 + nonce？
- 声明了 nonce / hash，但模板里所有 inline 脚本都真的打了 nonce 吗？只要漏一处就退回 `unsafe-inline` 兜底
- Report-Only 模式实际收报告吗？report-uri / report-to 配的端点有人看吗？
- CSP 字符串是不是动态生成的？生成代码里有没有用户可控分支让 directive 被放宽？
- 多层声明（应用 / 反向代理 / meta）是否冲突？浏览器实际收到的是哪一份策略？
- `frame-ancestors` 单独看——它**不** fall back 到 default-src，缺失就不防 clickjacking

---

## 8. 检测方法论 / 数据流追踪

### Step 0：基线侦察

- 加载 `project-framework-analysis` 输出的项目结构 / 框架识别
- 用 §5 的 grep 命令一次性枚举所有 CSP 声明位置
- 区分层级：应用代码层 / 反向代理层 / HTML meta 层
- 确认部署形态：`Content-Security-Policy` 还是 `Content-Security-Policy-Report-Only`

### Step 1：提取并分解 directive

对每个声明位置：
1. 提取 CSP 字符串原文（若是动态构建，追到拼接代码，列出所有可能的最终值）
2. 按 `;` 分割成 directive 列表
3. 每个 directive 按空格分解成 source 列表
4. 列出未声明的 directive（依赖 `default-src` 兜底，但 `frame-ancestors` / `report-uri` / `base-uri` / `form-action` **不** fall back）

### Step 2：危险关键字检测

对每个 directive 的 source 列表，按 §4 表格识别危险关键字：
- `'unsafe-inline'` / `'unsafe-eval'` / `'unsafe-hashes'`
- `*` / `https:` / `http:` / `data:` / `blob:` / `filesystem:`
- 通配子域 `*.example.com`（看 example.com 下是否托管用户可控内容）

### Step 3：nonce / hash 一致性核验

若 directive 声明了 `'nonce-...'` 或 `'sha256-...'`：
1. 找 nonce 生成代码：是否每请求随机？随机源是否密码学安全（`SecureRandom` / `crypto.randomBytes` 而非 `Math.random` / 时间戳）？
2. 找模板里的 `<script>` / `<style>` 标签：是否所有 inline 都打了 nonce？
3. 若 nonce 声明但模板未使用：业务靠 `unsafe-inline` 兜底，nonce 是装饰

### Step 4：最小权限基线对比

以 OWASP CSP Cheat Sheet 推荐基线对照：
- `default-src 'self'` / `'none'`
- `script-src 'self' 'nonce-{random}'`（或 strict-dynamic）
- `object-src 'none'`
- `base-uri 'self'` / `'none'`
- `frame-ancestors 'self'` / `'none'`
- `form-action 'self'`

### Step 5：部署模式与监控

- 是 Report-Only 还是 Enforced？
- 配了 `report-uri` / `report-to`？端点是否实际接收并有人看？
- 多层声明（应用 + nginx + meta）合并后浏览器实际策略是什么？

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 所有 CSP 声明位置已枚举（应用层 / 反向代理层 / HTML meta 层）
- [ ] 每个声明位置的 directive 列表已分解，未声明的 directive 已识别
- [ ] `script-src` / `style-src` 已核验 unsafe-inline / unsafe-eval / `data:` / 过宽 source
- [ ] `frame-ancestors` / `base-uri` / `object-src` / `form-action` 单独看（不 fall back 到 default-src）
- [ ] 声明 nonce / hash 时已核验生成端随机性 + 使用端覆盖率
- [ ] Report-Only 模式已记录监控状态
- [ ] 动态构建的 CSP 字符串已追到生成代码，无用户可控分支
- [ ] 多层声明的合并冲突已分析

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性以 [closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游汇总与 coverage-ledger 一致性消费，聚合或省略会让链路失效，因此是刚性要求。

### 白盒判定上限

本能力作为白盒原子能力，判定上限为 `static-confirmed`（CSP 字符串静态可达且含弱化），**不等于动态 confirmed**。

| 状态 | 判定条件 | 升级路径 |
|---|---|---|
| `static-confirmed`（落 `status=needs_review`） | CSP 策略文本静态可达 + 含 `unsafe-inline` / `unsafe-eval` / `data:` 等弱化关键字 / 过宽 source-list / 关键 directive 缺失 | 黑盒在浏览器实际触发 XSS 验证 CSP 未拦截 → `confirmed` |
| `static-unknown`（落 `status=needs_review` + 标注 unknown） | CSP 动态构建无法追到最终值 / nonce 注入路径在模板系统的运行时行为不可见 / view 级覆盖装饰器的实际触发条件不可见 | 推 graybox 看运行时实际下发的 CSP header |
| `not_vulnerable`（落 `status=not_vulnerable`） | CSP 策略静态可达 + 严格最小权限 + nonce 端到端核验通过 + 无危险关键字 | — |

**禁止**白盒独立判 `confirmed`——无浏览器实际拦截失败证据，仅静态弱化不构成动态利用。

### 产物契约（必须遵守）

> **为什么这里是「必须」**：产物结构是下游机器消费的接口，按 (CSP 声明位置, directive) 单元独立成行，聚合 / 省略会让 coverage-ledger 完整性闸门失效。

每确认一条弱化候选**立即** append 一行到 `shared/coverage-ledger/findings/csp-audit.jsonl`，不等汇总阶段回头整理。产物结构对齐 [sast-scan](../sast-scan/SKILL.md) §9 jsonl 字段：

```json
{
  "id": "csp-001",
  "title": "script-src 含 'unsafe-inline' 且无 nonce 兜底",
  "severity": "high",
  "cwe": "CWE-693",
  "source": "user-controlled inline injection",
  "sink": "script-src directive",
  "entry_point": "GET /dashboard",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "file_location": "config/SecurityConfig.java:48",
  "source_report": "csp-audit",
  "description": "..."
}
```

字段约束：
- `id` 带 `csp-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒默认 `needs_review`）
- `confidence ∈ static-confirmed | static-unknown`
- `(声明位置, directive)` 二元组任一不同即独立成行——同一文件含 `unsafe-inline` 与 `unsafe-eval` 分两行
- `entry_point` 填该 CSP 实际生效的路由 / 路径前缀；全局生效填 `*` 或 `systemic`
- `file_location` 填 `file:line`，动态构建的 CSP 填生成代码位置

### 反例义务（必须遵守）

> **why**：CSP 审计的"已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游误信浏览器侧防御层有效。

写"CSP 已最小权限"或"已防 XSS 兜底"前，产物必须包含：
- 所有 CSP 声明位置完整清单（grep 覆盖证据）
- 每个声明位置的 directive 列表与 source 分解
- nonce / hash 端到端核验结论（生成端随机性 + 使用端覆盖率）
- 多层声明合并冲突分析（应用 / 反向代理 / meta）
- `static-unknown` 单元格的具体原因（动态构建 / 运行时注入 / 边缘节点）

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：`unsafe-inline` 但同时启用 nonce 且所有 inline 都打了 nonce**

- 抽象规则：含 `unsafe-inline` 的 directive 同时含 `'nonce-...'` 时，**支持 nonce 的浏览器忽略 `unsafe-inline`**（向后兼容旧浏览器才保留）
- 具体场景：`script-src 'self' 'unsafe-inline' 'nonce-abc123'` + 模板里所有 `<script>` 都打了 `nonce="abc123"`
- 关键识别特征：directive 同时含 nonce + `unsafe-inline`；模板渲染层确认所有 inline 标签都打了 nonce
- 排除方法：核验 nonce 生成端随机性（密码学安全源）+ 使用端覆盖率（无漏打 nonce 的 inline 标签）→ 标 `not_vulnerable`

**反例 2：Report-Only 模式已配套监控告警**

- 抽象规则：Report-Only 模式不阻断但若有监控可作为 staging 阶段的合理选择
- 具体场景：`Content-Security-Policy-Report-Only` + `report-uri /csp-violations` + 后端订阅告警
- 关键识别特征：report-uri 端点有 handler + 日志接入告警系统
- 排除方法：读 report-uri handler 实现 + 告警配置 → 若属预生产观察阶段标 `not_vulnerable`（带注释说明非长期方案）

**反例 3：某 directive 缺失但被 default-src 兜底**

- 抽象规则：除 `frame-ancestors` / `base-uri` / `form-action` / `report-uri` 等特例外，未声明的 fetch directive 由 `default-src` 兜底
- 具体场景：策略只有 `default-src 'self'; script-src 'self'`，未显式声明 `img-src` / `font-src`
- 关键识别特征：default-src 已收敛到 `'self'` / `'none'`，缺失 directive 属 fetch 类
- 排除方法：核对 [CSP 规范的 fall back 表](https://www.w3.org/TR/CSP3/)；fetch 类标 `not_vulnerable`，非 fetch 类（`frame-ancestors` / `base-uri` 等）独立判

### FN（看似不命中实际是真洞）

**反例 4：CSP 看起来严格但 `'self'` 域下托管了 JSONP / 老旧文件上传**

- 抽象规则：`script-src 'self'` 只约束域，域下若有 JSONP 端点 / 用户上传的 JS / 老旧 swfobject 等，攻击者可借合法域绕过
- 具体场景：`script-src 'self'` + 同域 `/api/jsonp?callback=alert(1)` 端点存在
- 关键识别特征：grep 同域内是否有 `callback=` / `jsonp=` 类参数处理 / 用户可上传 JS 的端点
- 确认方法：列同域所有可被 `<script src>` 引用的端点，逐一看是否输出用户可控 JS → 升级到 `static-confirmed`

**反例 5：nonce 是固定值而非每次随机**

- 抽象规则：nonce 必须密码学安全随机且每请求生成；常量 / 配置项 / 弱随机源等价于无 nonce
- 具体场景：`'nonce-${process.env.CSP_NONCE}'` 从环境变量读 / `'nonce-${Date.now()}'` 用时间戳
- 关键识别特征：nonce 生成代码不是 `crypto.randomBytes` / `SecureRandom` 等密码学源；或 nonce 在请求间复用
- 确认方法：追 nonce 生成代码 → 攻击者可猜值 → 升级到 `static-confirmed`

**反例 6：nonce 声明但模板里未使用**

- 抽象规则：header 含 nonce 但模板渲染层没把 nonce 注入 `<script>` 标签 → 业务靠 `unsafe-inline` 兜底，nonce 形同虚设
- 具体场景：Spring `HeadersConfigurer` 写了 nonce，但 Thymeleaf 模板里全是 `<script>...</script>` 无 nonce 属性
- 关键识别特征：模板里 inline 脚本数 > 打 nonce 的脚本数
- 确认方法：grep 模板目录 `<script>` 与 `<script nonce` 数量差 → 升级到 `static-confirmed`

### 易混淆案例

**反例 7：动态构建的 CSP 含用户可控分支**

- 抽象规则：CSP 字符串由配置 / 数据库 / 请求参数拼接，存在路径让用户控制 directive 放宽
- 具体场景：根据租户配置追加 `script-src` 域 / 根据 feature flag 切换严格-宽松策略
- 关键识别特征：CSP 不是字面量，含 `+` / template literal / `format` 拼接
- 处置方法：追到所有可能的最终值；不能枚举完整时标 `static-unknown`

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到源码 / 配置 / 模板的字面量与可达字符串构造为止。

下面这些情形数据流分析无法继续追踪，**必须**标 `static-unknown`，**不允许**默认为 not_vulnerable：

1. **动态生成的 CSP 字符串**——从数据库 / 远程配置 / 运行时上下文读取 directive 值后拼接。处置：能追到所有可能值则列出，否则标 `static-unknown`。
2. **Feature flag 切换的策略**——根据租户 / 灰度 / 环境分支切换严格-宽松。处置：每个分支独立审计；不能只看默认分支下结论。
3. **服务端模板渲染时的 nonce 注入**——nonce 在请求级注入到模板的运行时行为；模板里有多少 inline 脚本静态可见，但 nonce 是否实际注入到每个标签需运行时验证。处置：模板侧 grep 静态覆盖率作为下限；动态升级需 graybox。
4. **CDN / 边缘节点 override**——Cloudflare Workers / AWS Lambda@Edge / Akamai EdgeWorkers 可能在边缘改写 CSP header。处置：标 `static-unknown`，记录边缘配置文件路径（若可读）；不可读的配置不在白盒范围。
5. **反向代理与应用层的合并冲突**——nginx `add_header` 与应用层 `setHeader` 的浏览器实际接收顺序取决于代理转发模式（`always` 修饰符 / `proxy_pass_header` 配置）。处置：能确定接收顺序则按合并后策略判；不能确定则标 `static-unknown`。
6. **闭源依赖中间件**——三方安全中间件（如商业 WAF SDK）内部写 CSP header。处置：依赖图谱标 `unknown`，推反编译 / 文档查阅；不能直接 not_vulnerable。
7. **浏览器版本兼容性差异**——`script-src-elem` vs `script-src` 在旧浏览器的支持差异、`strict-dynamic` 的浏览器覆盖、nonce + `unsafe-inline` 的回退行为。处置：本能力**不评估**浏览器兼容性差异下的实际防护效果——超出静态审计范围，标注"按目标浏览器矩阵单独评估"。

**底线**：写"该项目 CSP 已最小权限"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

---

## 12. 修复建议

### 源头治理（首选）

**最小权限基线**（参 OWASP CSP Cheat Sheet）：

```text
default-src 'self';
script-src 'self' 'nonce-{random}' 'strict-dynamic';
style-src 'self' 'nonce-{random}';
img-src 'self' data:;
font-src 'self';
connect-src 'self';
object-src 'none';
base-uri 'self';
frame-ancestors 'self';
form-action 'self';
report-uri /csp-violations;
upgrade-insecure-requests;
```

**关键替代**：
- nonce / hash 替代 `unsafe-inline`：每请求密码学安全随机生成 nonce + 模板渲染层注入到所有 inline `<script>` / `<style>`
- 删 `unsafe-eval`：不依赖 `eval` / `new Function` / `setTimeout(string)`；Angular 等需 JIT 编译的框架改用 AOT
- 收敛 source-list：用具体域替代 `*` / `https:`；CDN 资源用 SRI（Subresource Integrity）+ 具体域
- 显式声明 `frame-ancestors` / `base-uri` / `object-src` / `form-action`——这些不 fall back 到 default-src
- 启用 `report-uri` / `report-to` 收集策略违规事件，配套监控告警

### 部署节奏（次选，深度防御）

- 先 `Content-Security-Policy-Report-Only` + report-uri 观测一段时间，确认无业务破坏
- 分析 report 调整策略
- 切换为 `Content-Security-Policy` 实际阻断

### 兜底拒绝

- CSP 不是 XSS 唯一防御：应用层输出编码、模板自动转义、`HttpOnly` Cookie 仍是必要前置
- CSP 不防 DOM 操作以外的 XSS 路径（如 PostMessage 滥用）——按场景配合其他控制

### 参考

- [OWASP Content Security Policy Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Content_Security_Policy_Cheat_Sheet.html)
- [W3C CSP Level 3](https://www.w3.org/TR/CSP3/)
- [Google CSP Evaluator](https://csp-evaluator.withgoogle.com/) — 策略评估辅助工具
