---
name: xss-testing
description: >-
  XSS 跨站脚本多上下文综合检测——分诊反射型 / 存储型 / DOM 型 / mXSS / Blind XSS，按 HTML 内容 / 属性 / JS / CSS / URL 上下文选择逃逸 payload。
  流量中用户输入回显到响应 HTML / 前端 JS 读取 `location.*` 或 `document.referrer` 流入 DOM sink / 模板渲染未转义 / CSP 缺失或弱配置时使用。
when-to-use: 当用户输入回显在页面中、搜索结果含用户输入、错误消息反射用户输入（表单、搜索、评论、个人资料）时，黑盒验证反射/存储/DOM 型 XSS
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# XSS：多上下文综合检测（黑盒）

## 1. 触发线索 / 适用信号

以下是已知的 XSS 触发线索，作为**基线起点**而非必检硬清单。结合流量与响应特征动态调整：

- 适用且已完成 → `[x] done`
- 明确不适用 → `[-] n/a (原因)`（原因要具体到响应特征）
- 基线未列出但实际发现 → `[+] added (来源)`

**响应特征命中信号**（漏洞-specific）：
- 探针字符串（如 `xss_probe_12345`）在响应 HTML body 中以**未转义**形式出现
- 响应里出现 `<` / `>` / `"` / `'` 原样回显（未变成 `&lt;` / `&gt;` / `&quot;` / `&#39;`）
- 探针出现在 `<script>...</script>` 块、`on*=` 事件属性、`href=` / `src=` 属性、`style=` 属性、`<style>` 块中
- 响应里探针虽被转义但**位置在 JS 字符串字面量内**（`var x = "..."`），可用引号闭合
- 响应头 `Content-Security-Policy` 缺失 / 含 `unsafe-inline` / `unsafe-eval` / 通配 `*` source
- 前端 JS 代码（响应内嵌 / 引用的 .js）含 `innerHTML` / `outerHTML` / `document.write` / `eval` / `setTimeout(str)` / `Function(str)` 等 DOM sink，且读取 `location.hash` / `location.search` / `document.referrer` / `postMessage.data`

**入口类型粗筛**（仅作类似场景示例 不限于此）：
- 搜索 / 列表 / 错误页（查询词或参数值回显在结果页 / 提示页）
- 评论 / 简介 / 文章正文 / 富文本 / 工单 / 消息（存储型主战场）
- 跳转中转 / 中间页（`next` / `target` / `return_url` 参数回显）
- WebSocket / SSE 实时推送（前端 `innerHTML = msg.body` 形态）
- 后端模板编辑器、广告创意 body_html、商品描述、Bio——任何"用户写入 → 后续被他人或自己看到"的字段

业务命名只作粗筛——sink 语义相同就属此范围。**JSON body 任一字段、header / cookie 任一字段、URL fragment、postMessage data 等都是 XSS 候选**，不限于"看起来像富文本"的字段名；具体参数位置由 HAR + 前端 DOM 实际渲染推导，不在此预设清单。

## 2. 造成原因

source 是任何用户可控输入（query / body / header / cookie / 路径参数 / URL fragment / 已入库再回读的字段 / 跨窗口 `postMessage` / WebSocket 推送）。sink 是 HTML 渲染上下文：`<body>` HTML 内容、HTML 属性值（含 `on*` 事件 / `href` / `src` / `style`）、`<script>` 块内 JS 字符串字面量、`<style>` 块内 CSS 字符串、URL 协议位置（`href="..."` / `src="..."` / `window.location`）、以及前端 DOM 写入 sink（`innerHTML` / `outerHTML` / `document.write` / `insertAdjacentHTML` / `eval` / `setTimeout(str)` / `setInterval(str)` / `Function(str)` / jQuery `.html()`）。

**任何 source 未经"上下文匹配的编码"即输出到 sink，即构成 XSS**——攻击者控制的字符串改变了浏览器的解析模式（HTML 解析器、JS 解析器、CSS 解析器、URL 解析器之一）。模板引擎的"自动 HTML 转义"只对默认 HTML 内容上下文生效，**属性 / JS / CSS / URL 上下文需要专用编码**；任何"显式标记可信"通道（Go `template.HTML(...)` / `template.JS(...)` / Vue `v-html` / React `dangerouslySetInnerHTML` / Angular `bypassSecurityTrust*`）都绕过自动转义。

CSP 不是免疫层：`unsafe-inline` / `unsafe-eval` / 反射型 nonce 重复使用 / 通配 source（`*` / `data:` / `blob:` 不受限）/ 旧版 `script-src` 缺 `default-src` 兜底，都让 CSP 形同虚设。DOM XSS 还可能绕过纯服务端 CSP（如基于现有 trusted script 的 gadget 利用）。

## 3. 响应信号映射

> 列出 XSS 黑盒可观测的**响应通道集合**（observation-channel）——执行上下文 / 渲染槽位 / 侧信道形态。输入位置（query / body / header / cookie / path / fragment / postMessage / WebSocket / second-order 等）从 HAR + 前端 DOM 实际推导，本节不预设清单。

**响应观察通道集合**（observation-channel）：
- `html-body-render`：探针原样出现在 HTML body（标签内容上下文），`<` / `>` 未转义
- `attribute-context`：探针出现在属性值（含单引号 / 双引号 / 无引号三种子上下文）
- `js-string-literal`：探针出现在 `<script>` 块内 JS 字符串字面量（双引号 / 单引号 / 反引号）
- `js-non-string`：探针直接进入 JS 表达式位置（如 `var x = {{ raw }}`）
- `css-context`：探针出现在 `<style>` 块或 `style=""` 属性
- `url-context`：探针进入 `href` / `src` / `action` / `formaction` / `window.location.*` 等 URL 槽位
- `dom-sink-write`：前端 JS 把 source 写入 `innerHTML` / `document.write` / `eval` 类 sink（DOM XSS 主通道）
- `mutation-mxss`：探针经 sanitizer 处理后进入 DOM，但浏览器 `innerHTML` 序列化-再解析造成属性 / 标签突变
- `oob-blind`：纯存储 / Blind 场景下，payload 在管理后台 / 客服面板 / 报表页执行，通过带外回连（DNS / HTTP / cookie 外泄）证明
- `csp-bypass-observation`：CSP 响应头允许 inline / unsafe-eval / 通配 source / 反射型 nonce，执行通道仍开放

## 4. 常见类型

| 类型 | 触发条件 | 响应特征 | 观察通道 |
|---|---|---|---|
| **反射型 XSS（Reflected）** | 单次请求参数即时回显并执行 | 同请求响应里 source 未编码出现在执行上下文 | `html-body-render` / `attribute-context` / `js-string-literal` |
| **存储型 XSS（Stored）** | source 先入库，回读时渲染并执行 | 写入端点不报错，回读端点页面执行脚本 | `html-body-render` + `secondary-readback` |
| **DOM 型 XSS（DOM-based）** | 前端 JS 把客户端 source 写入 DOM sink | 服务端响应不变，浏览器 DOM 中 payload 生效 | `dom-sink-write`（`location.hash` / `postMessage` / `referrer` → `innerHTML` / `eval`） |
| **mXSS（Mutation XSS）** | DOMPurify / sanitizer 处理后 `innerHTML` 再解析造成结构变异 | 输入"安全 HTML" 经 `innerHTML` 序列化-再解析后变成可执行结构 | `mutation-mxss` |
| **Blind XSS** | payload 在攻击者不可见后台执行（客服 / 管理 / 日志查看页） | 写入端点无回显，带外回连显示在另一面板触发 | `oob-blind` |
| **Self / 突变 XSS via CSP gadget** | CSP 阻止直接 inline 但允许已加载的脚本 gadget | 利用现有 JS 库的 sink（jQuery `$()` / AngularJS `ng-app` 模板）触发 | `csp-bypass-observation` |
| **Universal / 跨上下文 XSS** | 属性 / JS / URL 多上下文同时回显，逃逸路径多选 | 同一 source 在多个 sink 出现 | 多通道叠加 |

## 5. 侦察输入

按以下方式从侦察输出中筛 XSS 候选：

**从 HAR / 端点账本**：
- 参数名含 `q` / `search` / `keyword` / `name` / `title` / `content` / `body` / `comment` / `bio` / `description` / `next` / `return` / `redirect` / `utm_*` / `lang` / `theme`
- 端点路径含 `/search` / `/comment` / `/post` / `/article` / `/profile` / `/error` / `/login`（含错误回显）
- 响应 `Content-Type: text/html` 且响应体出现请求中的字符串原样片段
- 响应头无 `Content-Security-Policy` / 含 `unsafe-inline`
- 响应内嵌 / 引用的 .js 文件出现 `innerHTML` / `document.write` / `eval` / `location.hash` / `postMessage` 关键字

**从业务场景**（由页面级建模输出，见 `{page}-page-model.md` 语义节）：

> 下列业务场景仅作类似场景示例 不限于此；以页面级模型实际输出为准。

- 电商：商品标题 / 描述 / 评价 / 店铺简介 / 广告创意 body_html
- SaaS / 协作：工单内容 / 工作项描述 / 项目说明 / 富文本笔记
- 内容 / 论坛：文章正文 / 评论 / 用户签名 / Bio / 私信
- 客服 / 管理后台：用户提交的内容会在客服 / 运营面板渲染（Blind XSS 主场）
- 即时通讯 / 推送：WebSocket / SSE 消息渲染（前端 `innerHTML` 形态）

**从身份切换 HAR 对比**：
- 普通用户写入 → 管理员后台回读的字段是 Blind XSS 候选
- 跨子系统的同名展示页（多个端口 / 子域 / 业务条线）每个独立测，不假设"在 A 测过 B 也安全"
- 前端 SPA 路由（仅 hash 变化的页面）单独测 DOM 型

业务命名（如 "comment" / "bio"）只作粗筛——sink 语义相同就是候选，**JSON body 任一字段都是 XSS 候选**，不限于"看起来像富文本"的字段名。

## 6. 框架 / 引擎响应指纹

通过响应（错误关键字 / header / 行为）推断后端模板引擎 / 前端框架 / 净化库 / WAF / CSP 策略，优化 payload 选择。

### 后端模板引擎响应指纹

| 响应特征 | 推断引擎 | payload 选择 |
|---|---|---|
| 响应头 `X-Powered-By: Express` / `Set-Cookie: connect.sid` + EJS / Pug 错误 | Node EJS / Pug / Handlebars | 自动转义按引擎不同；`{{{...}}}` 三花括号是 Handlebars 不转义形态 |
| `Server: nginx` + `JSESSIONID` cookie + Thymeleaf / FreeMarker / Velocity 报错关键字 | Java Spring（Thymeleaf / FreeMarker / Velocity / JSP） | `th:utext` / `<#noescape>` / `$!{...}` 是不转义通道 |
| `Set-Cookie: PHPSESSID` + Twig / Blade / Smarty 报错 | PHP（Twig / Blade / Smarty / 原生 echo） | Blade `{!! !!}` / Twig `\|raw` / 原生 `echo` 直渲 |
| `Server: gunicorn` / `Werkzeug` traceback + Jinja2 关键字 | Python Django / Flask（Jinja2 / DTL） | `\|safe` / `{% autoescape off %}` / `mark_safe()` 是不转义通道 |
| Go 默认响应头不带框架，但响应里出现 `template:` 关键字报错 | Go html/template / text/template | `template.HTML(...)` / `template.JS(...)` / `template.HTMLAttr(...)` 显式标记可信；text/template 无自动转义 |
| 响应里出现 `Razor` / `@Html.Raw` / `X-Aspnet-Version` | ASP.NET Razor | `@Html.Raw(...)` 不转义；`@:` 文本块 |

### 前端框架响应指纹

| 响应特征 | 推断框架 | DOM 风险点 |
|---|---|---|
| HTML 含 `<div id="app">` + 引用 vue.js / vue.runtime.js | Vue | `v-html` 直渲；`v-bind` 到 `:href` / `:src` 时协议未限制 |
| HTML 含 `<div id="root">` + react 标记 / `__NEXT_DATA__` | React / Next.js | `dangerouslySetInnerHTML={{__html: x}}`；`href={x}` 接受 `javascript:` |
| HTML 含 `ng-app` / `ng-controller` / `[(ngModel)]` | AngularJS / Angular | AngularJS `{{...}}` 模板沙箱可逃逸（旧版）；Angular `bypassSecurityTrustHtml` |
| `<script src="*.svelte.js">` / svelte 编译产物特征 | Svelte | `{@html ...}` 不转义 |
| 引用 jQuery 且页面有 `$('#x').html(...)` 形态 | jQuery | `.html()` / `.append(str)` / `$.parseHTML` 直接 sink |

### 净化库 / 安全库指纹

| 响应特征 | 推断库 | 绕过方向 |
|---|---|---|
| 用户输入富文本被部分过滤但保留 `<a>` / `<img>` / `<svg>` | bluemonday / DOMPurify / sanitize-html | mXSS 突变 / 属性注入 / SVG payload / 标签嵌套混淆 |
| `<script>` 被剥离但 `on*=` 属性保留 | 简易黑名单 sanitizer | `<img onerror>` / `<svg onload>` / `<details ontoggle>` |
| 已编码 `<` 为 `&lt;` 但属性内引号未编码 | 编码不完整（仅 HTML 内容编码） | 属性闭合：`" onfocus="alert(1)` |

### CSP 响应指纹

| 响应头特征 | CSP 强度 | 利用方向 |
|---|---|---|
| 无 `Content-Security-Policy` 头 | 无 CSP | 任意 inline / 外链 script，反射型直接生效 |
| `default-src 'self'; script-src 'self' 'unsafe-inline'` | 弱 CSP | inline script 可执行，反射型直接生效 |
| `script-src 'self'`（无 `unsafe-inline`） | 中等 CSP | 寻找已加载 JS 的 gadget；JSONP 端点利用；DOM sink 利用 |
| `script-src 'nonce-<random>'` 且 nonce 每次刷新变化 | 强 CSP | 仅 DOM 型可能（CSP 不阻止已在白名单 script 执行 sink） |
| `script-src 'strict-dynamic'` + nonce | 最强 CSP | 极难绕过，仍需检查 trusted script 是否安全 |

### WAF 响应指纹

| 响应特征 | WAF | 绕过方向 |
|---|---|---|
| `cf-ray` header + `Attention Required` 拦截页 | Cloudflare | HTML 实体编码 / Unicode 转义 / 大小写混用 / 拆分关键字 |
| `request blocked` + 阿里云 LOGO | 阿里云 WAF | URL 编码 / Base64 / 嵌入式注释 / svg / data: URL |
| `imunify360` / `incapsula` / `akamai` 拦截特征 | 商业 WAF | 同上 + tag mutation |

**响应指纹仅作辅助判断**——真实的 sink 验证仍需在 §9 闭环要求章节定义的"浏览器实际执行"可观测效果证据。

## 7. 思考检查点

加载本 skill 时按这些问题思考：

- 这个参数在 HAR 里出现在哪个位置？响应里的探针落在 HTML 内容 / 属性 / JS / CSS / URL 哪个上下文？
- 同端点其他参数是否走同一段模板？（同模板下其他字段往往是候选）
- 是反射型 / 存储型 / DOM 型？写入端点和回读端点分别在哪？是否需要新会话验证存储型？
- 探针被部分编码了吗？`<` `>` 编码但 `"` 未编码 → 属性闭合可行；HTML 编码但 JS 字符串内 → 引号闭合可行
- 后端是不是有 `template.HTML(...)` / `v-html` / `dangerouslySetInnerHTML` / `\|raw` / `\|safe` 类显式可信通道？
- CSP 是否存在？`unsafe-inline` 有吗？nonce 是否真随机？有没有 JSONP / 内联 script 的 gadget？
- 前端 JS 是否从 `location.hash` / `location.search` / `document.referrer` / `postMessage` 读取后写入 `innerHTML` / `eval`？
- 这条 payload 是否会被客服 / 管理面板渲染？（Blind XSS 需要带外通道）

## 8. 检测方法论 / 决策树

### 全局约束（默认保守预算）

- 单参数最多 15 次请求（含基线 / 探针 / 确认 / 浏览器执行 1 次）；同一轮只改 1 个参数
- 优先无害 payload（标签渲染 / `<b>` / 控制台 `console.log` 标记），确认上下文后再升级到 `alert` / 带外回连
- 浏览器执行验证使用一次性独立窗口 / 隔离 profile，避免污染主会话
- 存储型 XSS 必须**先确认能清理**（删除测试条目 / 改回内容）才写入
- 一旦"确认浏览器执行"立即停止进一步探测

### Step 0：基线采集

1. 发送原始请求 2-3 次，记录基线响应特征（响应头 / CSP / body 模板）
2. 标记并忽略动态字段（时间戳 / 随机 nonce / sessionToken）
3. 抓取响应内嵌 / 引用 JS 中的 `innerHTML` / `document.write` / `eval` / `location.*` / `postMessage` 关键字位置

### Step 1：探针定位（上下文识别）

1. 发送含**唯一标记字符串**（如 `xPrObE9k2`）的请求，覆盖所有候选参数位置
2. 在响应中 `grep` 标记，定位每个出现点的**上下文**：
   - HTML 标签内容（`<div>xPrObE9k2</div>`）
   - HTML 属性值（`<input value="xPrObE9k2">` / `<input value='...'>` / `<input value=...>` 无引号）
   - `<script>` 块内 JS 字符串（`var x = "xPrObE9k2"` / `'...'` / `` `...` ``）
   - `<script>` 块内 JS 非字符串位置（直接表达式 `var x = xPrObE9k2`）
   - `<style>` 块 / `style=""` 属性
   - URL 槽位（`href="..."` / `src="..."` / `location.href = "..."`）
3. 标注每个出现点的编码状态：`<` `>` `"` `'` `&` 是否被转义；记入"上下文 + 编码画像"

### Step 2：上下文逃逸探测

| 上下文 | 探测 payload | 成功标志 |
|---|---|---|
| HTML 标签内容 | `<b>xss</b>` → `<svg onload=console.log(1)>` | DOM 出现 `<b>` 节点 / 控制台输出 |
| HTML 属性（双引号） | `" autofocus onfocus="console.log(1)` | 属性被闭合 → 新事件挂载 |
| HTML 属性（单引号） | `' autofocus onfocus='console.log(1)` | 同上 |
| HTML 属性（无引号） | ` autofocus onfocus=console.log(1)` | 空格分隔生效 |
| JS 字符串（双引号） | `";console.log(1);//` | JS 执行 |
| JS 字符串（单引号） | `';console.log(1);//` | 同上 |
| JS 模板字面量 | ` ${console.log(1)} ` | 反引号内表达式生效 |
| URL 槽位（href / src） | `javascript:console.log(1)` | 协议触发 |
| CSS 上下文 | `</style><svg onload=...>` 或 `expression(...)` (旧 IE) | 标签逃逸 |
| DOM sink（location.hash） | `#<img src=x onerror=console.log(1)>` | 前端写入 DOM |

输出：候选逃逸集合（quote_type + escape_needed + tag_allowed + event_allowed）。

### Step 3：框架 / CSP 指纹判断

按 §6 指纹表识别后端引擎 / 前端框架 / 净化库 / CSP 强度。结论标注置信度（high / medium / low）。

### Step 4：策略决策树

```
探针未转义出现在 HTML 内容上下文 + 无 CSP / unsafe-inline → 反射型直接 <script> / <svg onload>
探针在属性上下文 + 引号未编码         → 属性闭合 + 事件处理器（onfocus / onerror / onmouseover）
探针在 JS 字符串字面量 + 引号未编码    → 字符串闭合 + 表达式注入
探针在 URL 槽位                       → javascript: 协议（注意 CSP）
源是 location.hash / postMessage      → DOM 型；服务端响应不变也算
写入端点入库但当前响应无回显           → 找回读端点 → 新会话访问验证 → 若回读端点在客服 / 管理面板走 Blind XSS（带外）
CSP 强（nonce / strict-dynamic）       → 寻找 gadget；DOM 型仍可能
净化库存在（DOMPurify / bluemonday）   → 试 mXSS（嵌套 svg / template / noscript / 注释突变）
```

### Step 5：浏览器执行确认 + 防误报

- **强证据（满足其一即 confirmed）**：浏览器 DevTools 控制台输出受控字符串；DOM 中出现注入的标签节点（可截图 / 节点路径可指认）；带外服务器收到含一次性 token 的回连请求
- **弱证据（仅 suspected）**：响应里有未编码字符 / 编码不完整 / CSP 缺失，但浏览器未实际执行（CSP 阻止 / sanitizer 拦截 / 上下文不可逃逸）
- **拒绝条件**：探针出现在响应里但被完整 HTML 实体编码（`&lt;` `&gt;` `&quot;` `&#39;` `&amp;`）且属性 / JS / CSS / URL 上下文均未发现 source——not_vulnerable

### Payload 范式与编码绕过

- WAF 关键字过滤 → HTML 实体（`&#x6f;nerror`）/ Unicode 转义（`alert`）/ 大小写混用 / 拆分 / 注释插入
- 引号过滤 → 无引号属性（`onfocus=alert(1)`）/ 反引号模板字面量 / `String.fromCharCode(...)` / `eval(atob('...'))`
- 标签过滤 → `<svg>` / `<math>` / `<details>` / `<iframe srcdoc>` / 自定义元素 `<x-x onload>`
- 事件过滤 → 罕见事件（`onpointerrawupdate` / `onanimationend` / `onfocusin` / `ontoggle`）/ 自定义事件 + dispatchEvent
- HTML 编码绕过 → URL 编码（`%3C`）/ 双重编码（`%253C`）/ Unicode (`<`)/ HTML 实体（数字 / 命名 / 短形式）
- CSP `unsafe-inline` 缺失但有白名单 host → 找 JSONP / 可上传脚本的同源 / 站内 base href 接管

### 基线检查项（按 §4.3 三态标注）

- [ ] 单参数所有候选位置（query / body / header / cookie / path / fragment）都过了 Step 0-1
- [ ] 同端点其他参数同步分诊
- [ ] DOM 型独立评估（前端 JS 中的 source → sink 链路）
- [ ] 二阶 / 存储型：从入库端点追到所有回读端点（含客服 / 管理后台 → Blind XSS）
- [ ] CSP 头 / 净化库 / 模板引擎不转义通道已识别并标注

## 9. 闭环要求（必须遵守）

> 闭环判定（confirmed / suspected / not_vulnerable）以 `common/closure-verification.md` 为准。下面只列本漏洞特有的可观测信号。

**confirmed（必须挂浏览器实际执行证据）**：
- **反射型**：构造 URL → 浏览器访问 → DevTools 控制台显示受控输出 / DOM 中出现注入节点；截图 + 控制台日志可指认
- **存储型**：写入端点 → 新会话或匿名浏览器访问回读端点 → 浏览器执行脚本；写入 / 回读 / 执行三段证据齐全
- **DOM 型**：构造 URL（含 hash / 参数） → 浏览器访问 → 前端 JS 把 source 写入 sink → 控制台 / DOM 改写可证明
- **mXSS**：sanitizer 处理后的 payload 经 `innerHTML` 序列化-再解析后产生新执行节点，可截图
- **Blind XSS**：管理 / 客服 / 报表面板渲染时，带外服务器收到含一次性 token 的回连请求（DNS / HTTP / cookie 外泄）
- **CSP gadget**：在 CSP 限制下找到 gadget 真实执行（如 JSONP / AngularJS 模板沙箱逃逸）的控制台证据

**suspected（落 `status=needs_review`）**：
- 探针未编码出现在响应里，但本地浏览器测试 CSP 阻止 / sanitizer 拦截 / 上下文不可逃逸（执行未发生）
- 仅看到属性 / 标签 / 引号未转义，未做浏览器执行验证
- 存储型仅完成"写入"未做"新会话访问 + 浏览器执行"
- 单次执行未稳定复现（页面刷新后未触发）

**not_vulnerable（落 `status=not_vulnerable`）**：
- 同模板下所有参数均经上下文匹配的编码（HTML 内容 → 实体；属性 → 属性编码；JS → JS 转义；URL → URL 编码）且 sanitizer 配置无已知绕过
- 该端点不返回 HTML（纯 JSON API 且前端不渲染该字段）
- CSP 严格（`strict-dynamic` + nonce）且无可用 gadget，且 DOM 型路径同样不可达

**禁止**仅凭"特殊字符未过滤" / "响应中看到 `<` 未编码" / "CSP 缺失" / "payload 在响应里出现" 判 confirmed——这些只到 suspected，**必须确认浏览器实际执行**。

### 反例义务（必须遵守）

> **why**：反例义务属于交付契约——"该子系统无 XSS"或"已防护"结论是覆盖完整性的产物声明，缺失反向验证清单会让下游误信"该维度全站安全"。XSS 上下文众多，仅测富文本 / 评论无法支撑 site-wide 安全声明。

写"未发现 XSS"或"已防护"前，产物必须包含：
- 测过的 XSS 候选端点完整清单（按 sink 语义枚举：所有"用户输入 → HTML 渲染上下文"端点，**不**按业务命名筛选；含反射回显 / 存储回读 / DOM sink / WebSocket 推送 / `template.HTML(...)` / `v-html` / `dangerouslySetInnerHTML` 等全部触发线索）
- 每端点测过的上下文 payload（HTML 内容 / 属性 / JS 字符串 / URL / CSS，按上下文分别测）
- 每端点的浏览器执行证据或明确的"未执行原因"（CSP 阻止 / sanitizer 拦截 / 上下文不可逃逸）

清单不完整 → 结论降级为 `partial-coverage` 并显式声明未覆盖范围。

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：响应里看到未编码的 `<` 但实际不在执行上下文**

- 抽象规则：未编码字符 ≠ 可执行 XSS
- 具体场景：探针 `<xPrObE>` 出现在 `<!-- xPrObE -->` HTML 注释里、`<textarea>xPrObE</textarea>` 里、`<title>xPrObE</title>` 里
- 关键识别特征：探针位置在注释 / textarea / title / script 块外 / iframe srcdoc 等"特殊解析"上下文
- 排除方法：尝试用对应闭合（`-->` / `</textarea>` / `</title>`）逃逸，看是否真能跳出包裹

**反例 2：响应里 `<script>` 标签被剥离但属性仍可注入**

- 抽象规则：`<script>` 拦截不等于全面防御
- 具体场景：写入 `<script>alert(1)</script>` 被剥成空，但 `<img src=x onerror=alert(1)>` 仍生效
- 关键识别特征：sanitizer 黑名单只覆盖 `<script>` / `<iframe>`，不覆盖事件属性 / svg / 自定义元素
- 排除方法：换 svg / details / 事件属性 payload 验证；若全部被剥才属真防御

**反例 3：本地有 CSP `default-src 'self'` 但反射型仍 confirmed**

- 抽象规则：CSP 限制 inline 不等于 DOM 型 / gadget 也安全
- 具体场景：反射型测试浏览器拦截 inline script 执行，但同站有 JSONP 端点 / AngularJS 模板可被 gadget 利用
- 关键识别特征：CSP 含 `'self'` 但站内有可被攻击者控制返回内容的同源资源
- 排除方法：枚举同源资源 + 已加载脚本的可滥用 sink，再下结论

### FN（看似不命中实际是真洞）

**反例 4：业务命名非"富文本" / "评论" 跳过测试**

- 抽象规则：业务命名不是筛选依据，sink 语义才是
- 具体场景：广告创意 `body_html` / 商品详情 `description` / 工单 `content` / Bio `intro` / utm 参数 / next 跳转参数，凡回显到 HTML 上下文都是候选
- 关键识别特征：参数名"看起来不像富文本"但响应里被渲染为 HTML
- 确认方法：先发探针，若探针在 HTML body / 属性 / JS / URL 任一上下文出现，即按 §8 决策树处理

**反例 5：跨子系统隐式推广（漏报高发模式）**

- 抽象规则：在子系统 A 测过 ≠ 子系统 B 也安全
- 具体场景：测了 user-portal 评论无 XSS，admin-portal 渲染用户内容的面板未测就推断安全；多个子系统都用 Go html/template，但某子系统出现 `template.HTML(userInput)` 显式可信包装
- 关键识别特征：不同子系统、不同团队、不同时期开发；admin 端常复用普通端代码但实现可能各异
- 确认方法：admin / user / 第三方接入端每个子系统独立做反例义务自检；grep 所有 `template.HTML(` / `v-html=` / `dangerouslySetInnerHTML` 形态

**反例 6：DOM 型被忽略（服务端响应不变）**

- 抽象规则：服务端响应不变不等于无 XSS
- 具体场景：`/page#<img src=x onerror=...>` 的 hash 被前端 `document.getElementById('x').innerHTML = location.hash.slice(1)` 写入
- 关键识别特征：URL 含 hash / 前端 JS 读 `location.*` / `document.referrer` / `postMessage` 后写 DOM sink
- 确认方法：浏览器访问构造 URL，DevTools 观察 DOM / 控制台

**反例 7：mXSS 被 sanitizer "看似安全"的 HTML 漏掉**

- 抽象规则：经 sanitizer 处理后再次 `innerHTML` 可能产生突变
- 具体场景：DOMPurify 处理 `<noscript><p title="</noscript><img src=x onerror=...>">` 类嵌套，再被 `innerHTML` 二次解析时结构改变
- 关键识别特征：用户输入富文本 → 服务端 / 客户端 sanitizer → 前端 `innerHTML` 二次写入
- 确认方法：追 sanitizer 输出后是否还经 `innerHTML` / `outerHTML` 路径；查 DOMPurify / bluemonday 已知 mXSS CVE 是否适用

### 易混淆案例

**反例 8：HttpOnly cookie 让 XSS 看似无害**

- 抽象规则：HttpOnly 只防 JS 读 cookie，不防 XSS 本身
- 具体场景：评论 XSS 成功执行 alert，但攻击者无法窃取 sessionId
- 关键识别特征：cookie `HttpOnly` 属性存在
- 处理方法：XSS 仍 confirmed，但**危害评估**注明无法直接劫持会话；可改用钓鱼 / DOM 改写 / 内部 API 滥用 / CSRF 协同利用

**反例 9：自反射 XSS（Self-XSS）危害评估**

- 抽象规则：仅自己看到的 XSS 危害有限但仍属漏洞
- 具体场景：用户设置自己的 Bio 含 payload，只有该用户登录时自己页面执行
- 关键识别特征：内容仅自己可见 / 无社工诱导路径 / 无其他用户读取
- 处理方法：仍记录为 confirmed-low；若存在公开页 / 客服后台 / 关注者 feed 读取路径则升级

## 11. 测试安全边界

> 破坏性 / 不可逆动作的闭环边界以 `common/closure-verification.md`《破坏性 / 不可逆动作的闭环边界》节为准。下面只列 XSS 特有的破坏点。

**禁止**对真实业务数据 / 真实用户执行以下动作来"挂可观测效果"：

- 在真实公开内容（首页 banner / 公告 / 高曝光评论区）写入 `alert` / 弹窗 / 跳转类 payload——会影响真实用户体验
- 写入持续触发 `window.open` / `location.href` / 大流量带外回连的 payload（会被真实访问者触发并消耗资源）
- 用真实管理员账号 / 客服账号触发 Blind XSS 的 payload 时，不能让管理面板产生不可逆操作（如自动审批 / 删除其他用户数据 / 触发外发邮件 / 触发支付）
- 用 XSS 直接窃取真实用户的 cookie / token 并外发到非授权服务器
- 在生产环境的真实用户消息 / 工单 / 订单字段写入持久化 payload 而不清理

**允许**的非破坏验证手段：

- **哨兵自证**：建测试账号 / 测试条目（前缀 `sastx_sentinel_`），仅对哨兵执行 XSS 写入与回读验证；测试结束删除
- **非破坏 payload**：`console.log("sentinel_<token>")` / `document.title = "sentinel_<token>"` / `<img src=x>` 仅触发请求不弹窗
- **带外通道**：DNS / HTTP OOB（payload 含一次性 token，仅做"请求到达"证明，不携带 cookie / 真实数据）
- **隔离浏览器执行**：用一次性 profile / 隐身窗口验证 confirmed，避免污染主会话；执行后清理
- **CSP 报告通道**：观察 `report-uri` / `report-to` 触发的违规报告作为辅助证据（仅 suspected → confirmed 桥接信号，不单独构成 confirmed）

## 12. 修复建议

### 源头治理（首选）

- **上下文匹配的输出编码**：
  - HTML 内容上下文：HTML 实体编码（`&lt;` `&gt;` `&amp;` `&quot;` `&#39;`）
  - HTML 属性上下文：属性专用编码（含非字母数字字符全部转义）+ 始终用引号包裹属性
  - JS 字符串字面量：JS 转义（`\x3c` / `<`），避免 `</script>` 拼接
  - JS 非字符串位置：**禁止**把用户输入插入 JS 表达式位置——改成数据属性 `data-x="..."` 再前端读取
  - CSS 上下文：CSS 转义（`\3c` 形式）+ 限制为白名单字符
  - URL 槽位：协议白名单（http/https/相对路径）+ URL 编码；**禁止** `javascript:` / `data:` / `vbscript:` 协议
- **模板引擎默认转义**：使用 Go `html/template`（不是 `text/template`）、Jinja2 默认 `autoescape`、Twig 默认转义；显式不转义通道（`template.HTML(...)` / `\|safe` / `v-html` / `dangerouslySetInnerHTML` / `{!! !!}` / `\|raw` / `bypassSecurityTrust*`）必须经过严格审计并 inline 注释审计依据
- **前端 DOM 安全**：避免 `innerHTML` / `outerHTML` / `document.write` / `eval` / `setTimeout(string)` / `Function(string)`；改用 `textContent` / `setAttribute` / DOMPurify 净化后再写入
- **富文本字段**：使用经过验证的 HTML 净化库（bluemonday / DOMPurify / OWASP Java HTML Sanitizer），并定期升级以纳入 mXSS 修复

### 二阶 / 存储型

- **入库即净化**：富文本字段在写入前用净化库处理（白名单标签 / 属性 / 协议），并保留原文用于审计
- **回读必转义**：任何非富文本字段在回读渲染时仍走自动转义；不复用"入库已校验"假设
- **跨子系统一致**：用户内容被 admin / 客服 / 报表面板渲染时必须沿用相同净化策略

### 边界过滤（次选，深度防御）

- **CSP**：部署严格 CSP，禁止 `unsafe-inline` / `unsafe-eval`；使用 nonce 或 hash 白名单；`strict-dynamic` 是当前推荐形态
- **Trusted Types**（Chromium 系）：开启 `Content-Security-Policy: require-trusted-types-for 'script'`，从浏览器层限制 DOM sink
- **WAF 规则**：覆盖常见 XSS 关键字（`<script` / `onerror` / `javascript:`）——**仅作辅助**，不替代输出编码

### 兜底拒绝

- **Cookie 安全属性**：所有 session cookie 设 `HttpOnly` + `Secure` + `SameSite=Strict/Lax`
- **错误响应不回显输入**：错误页 / 调试页不把请求参数原文回显
- **管理后台隔离**：管理 / 客服 / 报表面板不直接渲染用户原始 HTML，使用纯文本视图或专用净化策略

### 参考

- [OWASP XSS Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html)
- [OWASP DOM based XSS Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/DOM_based_XSS_Prevention_Cheat_Sheet.html)
- [OWASP Content Security Policy Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Content_Security_Policy_Cheat_Sheet.html)
