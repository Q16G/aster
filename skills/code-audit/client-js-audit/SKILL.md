---
name: client-js-audit
description: >-
  客户端 JS 安全白盒审计——覆盖 DOM XSS（客户端 source 流到客户端 sink）、Token 与凭据客户端
  存储泄露、postMessage 跨源通信、客户端安全决策（鉴权 / 加解密 / feature flag）四个维度。
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
---

# 客户端 JS 安全白盒审计

## 1. 触发线索 / 适用信号

按 **代码 pattern + 框架信号 + 浏览器 API 使用**三维分类。

**代码 pattern 维度**（grep 命中模式）：
- DOM 直写 sink：`innerHTML` / `outerHTML` / `document.write` / `insertAdjacentHTML`
- 动态求值 sink：`eval(` / `new Function(` / `setTimeout(stringArg)` / `setInterval(stringArg)`
- 客户端可控 source：`location.hash` / `location.search` / `document.URL` / `document.referrer` / `window.name` / `URLSearchParams`
- postMessage 通信：`window.addEventListener('message'` / `window.postMessage(` / `iframe.contentWindow.postMessage(`
- 客户端存储：`localStorage.setItem` / `localStorage.getItem` / `sessionStorage.*` / `document.cookie`
- 前端鉴权判断：`if (user.role ===` / `if (isAdmin)` / `if (permissions.includes(` 等出现在 `*.vue` / `*.tsx` / `*.jsx` 的视图层

**框架信号维度**：
- Vue：`*.vue` 含 `v-html` / `v-bind:href` / 模板编译器配置
- React：`*.tsx` / `*.jsx` 含 `dangerouslySetInnerHTML` / `href={...}`
- Angular：`*.component.ts` 含 `[innerHTML]` / `bypassSecurityTrustHtml`
- Svelte：`*.svelte` 含 `{@html ...}`
- jQuery：`*.js` 含 `$(...).html(` / `$(...).append(` / `$.parseHTML`
- 客户端路由：`router/index.js`（Vue Router）/ `App.tsx` 的 React Router 配置

**浏览器 API 使用维度**：
- 跨 origin 通信：iframe 嵌入 / `window.open` / postMessage handler
- Service Worker / Web Worker：`navigator.serviceWorker.register` / `new Worker(`
- WebSocket：`new WebSocket(` 接收消息后写入 DOM
- 浏览器扩展或注入脚本上下文（`chrome.runtime` / `browser.runtime`）

业务命名（如方法名 `renderProfile`）只作粗筛——sink 语义相同就是审计候选。**与 `stored-xss-detection` 的分流口径**：本能力 source 是「客户端可控」（URL / hash / postMessage / 浏览器 API），持久化 source（DB → API → 渲染）走 `stored-xss-detection`，sink 集合可能重叠但 source 维度独立计数。

---

## 2. 造成原因（共享章节）

客户端 JS 安全的核心成因是**客户端可控 source 未经 sanitize 流向客户端 sink**，加之浏览器同源策略与本地存储的设计假设被违背：

- **DOM XSS**：`document.location` / `document.URL` / `document.referrer` / `window.name` / URL fragment / postMessage event.data 等客户端可控 source 流入 `innerHTML` / `eval` / `Function()` / `setTimeout(string)` 等 sink，攻击者构造的字符串改变了 DOM 结构或被求值执行。这类漏洞数据流完全在浏览器内，**服务端日志看不到**。
- **postMessage 越权**：handler 不校验 `event.origin` → 任意页面（含 `evil.com` 嵌入的 iframe）都能向受害页发消息触发其处理逻辑；发送端用 `targetOrigin = '*'` → 任意拦截到该 iframe 的页面都能读到消息内容。
- **客户端鉴权**：前端"角色 / 权限"判定只能控制 UI 显隐，**不等于鉴权**——后端无对应拦截时，攻击者直接拼接被隐藏的 URL 或调用被隐藏的 API 即可越权。
- **Token 存 localStorage**：localStorage 对同源 JS 全开放，一旦页面发生任何 XSS（含三方脚本被劫持），Token 全盘被读走；HttpOnly Cookie 设计上对 JS 隔离，正是为此。
- **敏感算法前端化**：加签 / 加密密钥嵌入 JS bundle → 攻击者下载 bundle 即可反解；前端做的"防爬虫签名"在攻击者眼里是公开算法。

---

## 3. 领域 source-sink 数据流模型

客户端 JS 安全的 source / sink 都在**浏览器侧**，数据流追踪要注意打包工具（webpack / vite / rollup）会重写代码——必要时看打包前源码而非 `dist/` 产物。

**客户端层 source 集合**：
- URL 与导航：`location.hash` / `location.search` / `location.pathname` / `location.href` / `URLSearchParams` 解析结果
- 文档与窗口：`document.URL` / `document.documentURI` / `document.referrer` / `document.baseURI` / `window.name`
- 跨窗口通信：`postMessage` event.data / event.origin（origin 本身可控，参 §10 反例）/ event.source
- WebSocket / EventSource：`ws.onmessage` event.data / `es.onmessage` event.data
- 本地存储（间接 source，已被 XSS 写入或被旧版本污染）：`localStorage.getItem` / `sessionStorage.getItem` / `document.cookie`
- DOM 可注入属性：`element.getAttribute('data-*')` 当属性值由攻击者注入时
- 浏览器扩展消息：`chrome.runtime.onMessage` / `window.addEventListener('message')` from content script

**客户端层 sink 集合**：
- DOM 直写：`Element.innerHTML` / `Element.outerHTML` / `document.write` / `document.writeln` / `Element.insertAdjacentHTML`
- 动态求值：`eval(string)` / `new Function(string)` / `setTimeout(string)` / `setInterval(string)`
- 危险属性赋值：`Element.setAttribute('on*', ...)` / `element.onclick = stringHandler` / `href = "javascript:..."` / `src = userControlledURL`
- jQuery / 库 sink：`$(el).html(...)` / `$.parseHTML(...)` / `$(el).append(htmlString)` / `$(el).after(htmlString)` / `$(el).prepend(htmlString)`
- 框架模板 sink：Vue `v-html` / React `dangerouslySetInnerHTML` / Angular `[innerHTML]` 配合 `bypassSecurityTrustHtml` / Svelte `{@html ...}`
- 模板引擎：Handlebars `{{{triple}}}` / Mustache 无转义块 / Underscore `_.template` 配合 `unescape`
- 导航 sink（开 `javascript:` 协议时可执行）：`location.href = ...` / `location.assign(...)` / `window.open(...)` / `<a href={userInput}>`

**数据流追踪规则**：
- 跨函数追踪：source 流向闭包变量 / 类成员 / Vuex / Redux store / event bus
- 跨文件追踪：组件 props 传递、Vue $emit / React props callback、router params
- 打包边界：`dist/*.js` 已混淆，**必须看源码仓库**（`src/`）；source map 缺失时降级 §11
- 框架 sanitize 边界：Vue 的 `{{ }}` 自动 HTML 转义、React `{value}` 自动转义、Angular DomSanitizer——只对**正确通道**生效，绕过通道（v-html / dangerouslySetInnerHTML / bypassSecurityTrust*）失效

---

## 4. 常见类型（共享章节）

| 类型 | 静态识别特征 | 白盒识别难点 |
|---|---|---|
| **DOM XSS（hash / search）** | `location.hash` / `location.search` → `innerHTML` / `eval` | 现代框架常用 hash 路由，需区分路由匹配（安全）和 hash 内容直接渲染（危险） |
| **postMessage 越权** | `addEventListener('message', handler)` handler 无 `event.origin` 校验 | origin 校验 typo（`includes` / 前后缀绕过）易漏检 |
| **客户端鉴权** | 视图层 `if (user.role === 'admin')` 控制 UI 显隐 | 必须交叉验证后端是否有独立鉴权——白盒只能标 `suspected`，需配合后端审计 |
| **localStorage Token 泄露** | `localStorage.setItem('token', ...)` / `localStorage.setItem('jwt', ...)` | 部分项目用 localStorage 缓存非敏感数据，需按键名语义判断 |
| **前端硬编码密钥** | JS 字面量含 `apiKey` / `secret` / 私钥 PEM 块 | 部分公钥（如 reCAPTCHA site key）合法暴露，需按算法语义判断 |
| **`eval(JSON.parse 备胎)`** | `try { JSON.parse(x) } catch { eval(x) }` 或老代码直接 `eval(jsonString)` | 旧 IE 兼容代码残留 |
| **jQuery `$()` 选择器解析 HTML** | `$(userInput)` 当 userInput 以 `<` 开头时被当 HTML 解析 | jQuery < 3.5.0 行为；现代项目可能升级后规则不再适用 |
| **Vue / React 模板编译注入** | Vue 编译期模板字符串含用户输入；React `React.createElement` 动态 type | 编译期注入只在运行时 compile 模式（如 Vue runtime-compiler 而非 runtime-only）触发 |
| **`href="javascript:..."` 协议** | `<a href={userInput}>` / `v-bind:href="userInput"` 缺协议白名单 | 框架不拦截 `javascript:` 协议，需手动校验 |
| **postMessage handler 进 eval / innerHTML** | message handler 内调 `eval(event.data)` 或 `el.innerHTML = event.data` | 即使 origin 校验对，handler 内部仍可能把 data 当代码执行 |

---

## 5. 入口点定位

按项目结构找客户端 JS 安全审计的 source / sink 候选位置：

> 下列框架 / 项目类型仅作类似项目示例，不限于此；以目标实际栈为准。

| 项目类型 | source / sink 高密度位置 |
|---|---|
| **Vue / Nuxt** | `src/views/*.vue` / `src/pages/*.vue` / `src/components/*.vue`（找 `v-html` / `v-bind:href` / `this.$route.hash`）；`src/router/index.js`（hash mode / `beforeEach` 守卫）；`src/utils/dom.js` / `src/utils/storage.js`（DOM / Token 封装）；`.env.*` 里 `VITE_*` / `VUE_APP_*` 前缀变量会进 bundle |
| **React / Next.js / Remix** | `src/pages/*.tsx` / `app/**/page.tsx`（`dangerouslySetInnerHTML` / `<a href={...}>`）；`src/components/*.tsx`（`useSearchParams` / `useLocation` 流向 DOM）；`src/stores/*.ts`（Zustand / Redux Token 存取）；`NEXT_PUBLIC_*` / `REACT_APP_*` 前缀变量进 client bundle |
| **Angular** | `src/app/**/*.component.{ts,html}`（`[innerHTML]` / `bypassSecurityTrustHtml`）；`*.service.ts`（Token / HTTP interceptor）；`*.guard.ts`（路由守卫鉴权判定） |
| **jQuery / 传统多页** | `public/*.html` 内嵌 `<script>`；`assets/js/*.js`（`$(...).html(` / `$(...).append(` / 老 `eval` / `setTimeout(string)`） |
| **postMessage / 扩展** | grep `addEventListener('message'` 找 handler；grep `<iframe` 找嵌入点核查 src 可控性；浏览器扩展 `manifest.json` 查 `content_scripts` / `permissions` |

通用建议：优先看**源码仓库**（`src/`），不是 `dist/` 打包产物——dist 混淆 + 没 source map 会让追源-sink 极难；若只有 dist 走 §11。与 [secret-detection](../secret-detection/SKILL.md) 协作扫前端硬编码密钥，与 [csp-audit](../csp-audit/SKILL.md) 协作评估 CSP 兜底（CSP 严格度直接影响 DOM XSS 利用难度）。

---

## 6. 跨框架代码变体

| 框架 | 安全形态 | 危险形态 |
|---|---|---|
| **Vue 2 / Vue 3** | `{{ value }}` 自动 HTML 转义；`v-bind:text-content` | `v-html="value"`；`v-bind:href="value"`（缺协议白名单） |
| **React** | `{value}` 自动转义；`<div>{userInput}</div>` | `dangerouslySetInnerHTML={{__html: value}}`；`<a href={userInput}>` |
| **Angular** | `{{value}}` / `[innerText]="value"`；DomSanitizer 默认 | `[innerHTML]="value"` + `DomSanitizer.bypassSecurityTrustHtml(value)` |
| **Svelte** | `{value}` 自动转义 | `{@html value}` |
| **jQuery** | `$(el).text(value)`；`$(el).attr('href', sanitizedUrl)` | `$(el).html(value)`；`$.parseHTML(value)`；`$(value)` 当 value 以 `<` 开头 |
| **原生 DOM** | `element.textContent = value`；`element.setAttribute('data-x', value)` | `element.innerHTML = value`；`element.setAttribute('onclick', value)` |
| **postMessage handler** | 第一行 `if (event.origin !== EXPECTED_ORIGIN) return;` + schema 校验 + 不进 sink | 无 origin 校验；origin 校验用 `includes` / `endsWith` 可绕过；handler 内 `eval(event.data)` |
| **postMessage 发送端** | `target.postMessage(data, 'https://trusted.example.com')` | `target.postMessage(data, '*')` |
| **Token 存储** | HttpOnly + Secure + SameSite=Strict Cookie；后端管理 session | `localStorage.setItem('token', jwt)`；`sessionStorage.setItem('jwt', ...)` |
| **客户端路由鉴权** | UI 层仅做显隐；后端每个 API 独立鉴权 | 仅前端路由守卫拦截，后端无对应鉴权 |
| **客户端加解密** | 仅做展示混淆 / 反爬，不当真正鉴权 | 把加签 / 加密当作"安全机制"且密钥在 bundle 里 |

**框架 sanitize 绕过点**（常见误判位）：
- **Vue v-html**：明确文档标记"危险"，但项目里常被当"渲染富文本"用——必须看上游有无 DOMPurify
- **Angular DomSanitizer**：`bypassSecurityTrustHtml` / `bypassSecurityTrustUrl` / `bypassSecurityTrustResourceUrl` 都是显式跳过 sanitize 的"逃生口"
- **React href**：React 不拦截 `javascript:` 协议，需手动 `if (!url.startsWith('http'))` 类校验

---

## 7. 思考检查点（共享章节）

加载本 skill 时按这些问题思考：

- 该 DOM sink 接收的字符串是否真到了**客户端可控 source**？经过 sanitize（DOMPurify 等）了吗？
- 该 postMessage handler 是否校验了 `event.origin`？校验是否是 `===` 严格相等，而不是 `includes` / `endsWith`（易绕过）？是否还校验 `event.source`？
- Token 是真的不存 localStorage / sessionStorage 吗？存的是不是 HttpOnly Cookie？
- 前端"权限"判断是否有后端对应鉴权？前端隐藏菜单后，直接拼 URL 调 API 是否仍能成功？
- 敏感算法（签名 / 加密）是否仅依赖前端？密钥是否在 JS bundle 里（含 `.env.*` 的 `VITE_*` / `NEXT_PUBLIC_*` / `VUE_APP_*` 前缀变量）？
- 跨子系统：用户 / admin 端是否共用同一段前端代码？admin 端的高权限 UI 控制是否同时是后端鉴权（极常见漏洞）？

---

## 8. 检测方法论 / 数据流追踪

### Step 0：基线侦察

- 加载 `project-framework-analysis` 输出，识别前端框架（Vue / React / Angular / Svelte / jQuery / 原生）、打包工具（webpack / vite / rollup）、客户端路由模式（hash / history）
- 定位前端项目根目录（monorepo 里可能有多个）
- 列出 `package.json` 依赖里的高风险库（jQuery < 3.5.0、Vue 2.6 模板编译器、DOMPurify 是否存在等）

### Step 1：grep DOM sink + postMessage handler

```bash
# DOM 直写 + 动态求值 sink
rg "\.innerHTML\s*=|\.outerHTML\s*=|document\.write\(|insertAdjacentHTML\(|\beval\(|new Function\(|setTimeout\([\"\\'`]|setInterval\([\"\\'`]"
# 框架特定 sink + jQuery sink
rg "v-html|dangerouslySetInnerHTML|\[innerHTML\]|bypassSecurityTrust|\{@html |\\\$\\([^)]+\\)\\.html\\(|\\\$\\.parseHTML\\("
# postMessage handler + 发送
rg "addEventListener\\(\\s*['\\\"]message['\\\"]|onmessage\\s*=|\\.postMessage\\("
# 客户端存储
rg "localStorage\\.setItem|sessionStorage\\.setItem|document\\.cookie\\s*="
# 客户端可控 source
rg "location\\.hash|location\\.search|document\\.referrer|window\\.name|URLSearchParams"
```

### Step 2：source 追踪（对每个 sink 倒推）

对 §1 命中的 sink，追上游：
1. sink 接收的变量是哪里来的？组件 prop / Vuex / Redux store / `useRouter().query` / `this.$route.hash`？
2. 上游若是路由参数，路由参数从哪进来？URL hash / query / postMessage / WebSocket？
3. 中间是否经过 sanitize（DOMPurify.sanitize / 框架自带转义 / 手写白名单 regex）？
4. 中间是否经过 JSON.parse + schema 校验（结构化数据流就不是字符串拼接了）？

工具加速：调用 `dataflow-analysis` MCP 工具做跨函数数据流追踪。

### Step 3：postMessage 专项

每个 `addEventListener('message', handler)` 都过这三关：
1. **origin 校验**：是否第一行就 `if (event.origin !== EXPECTED) return;`？
   - 严格 `===` 还是 `includes` / `endsWith`（易绕过，参 §10 反例 5）？
   - 白名单是单个 origin 还是数组？数组比对用 `.includes()` 还是 `===` 逐个比较？
2. **source 校验**：是否校验 `event.source === expectedWindow`？（防止同 origin 内多个 iframe 互相发消息伪装）
3. **data schema 校验**：是否 JSON.parse + 字段类型校验？还是直接当字符串拼到 sink？

发送端检查：`target.postMessage(data, targetOrigin)` 的 `targetOrigin` 是否是具体 URL 而非 `'*'`？

### Step 4：Token 与凭据存储

`rg "(localStorage|sessionStorage)\\.setItem"` 后按键名语义判断：是否含 `token` / `jwt` / `auth` / `credential` 语义（漏洞候选），还是单纯 UI 缓存（如 `theme` / `lang`，安全）。若是 Token：是否同时有 HttpOnly Cookie 通道（双轨可疑——若 localStorage 是冗余，应删；若是主通道，则是漏洞）。

### Step 5：前端权限判断

```bash
# 视图层鉴权判定
rg "user\\.role|isAdmin|hasPermission|permissions\\.includes" --type vue --type tsx --type jsx
# 路由守卫
rg "beforeEach|canActivate|requireAuth"
```

对每个命中：
- 该判定是否仅控制 UI 显隐？还是同时调用了后端 API（API 自带鉴权）？
- 隐藏的菜单 / 按钮对应的 API 端点，后端是否有独立鉴权？（白盒只能标 `suspected`，需配合后端审计或黑盒访问）

### Step 6：前端硬编码密钥扫描

与 `secret-detection` 协作。重点扫：
- `src/config/*.js` / `src/constants/*.ts`
- `.env*` 文件里 `VITE_*` / `NEXT_PUBLIC_*` / `VUE_APP_*` / `REACT_APP_*` 前缀变量
- 直接 grep `apiKey` / `secret` / `privateKey` / `-----BEGIN`

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

**DOM XSS 维度**：
- [ ] 所有 DOM 直写 sink（`innerHTML` / `outerHTML` / `document.write` / `insertAdjacentHTML`）+ 动态求值 sink（`eval` / `new Function` / `setTimeout(string)` / `setInterval(string)`）命中已倒推 source
- [ ] 框架 sink（`v-html` / `dangerouslySetInnerHTML` / `[innerHTML]` + `bypassSecurityTrust*` / `{@html}`）已判断 sanitize
- [ ] 客户端可控 source（`location.hash` / `location.search` / `document.referrer` / `window.name`）所有使用点已扫
- [ ] `href` / `src` 属性接收用户输入时有 `javascript:` 协议白名单
- [ ] 若项目仍用 jQuery，确认版本是否 < 3.5.0（影响 `$(userInput)` 解析 HTML 行为）

**postMessage 维度**：
- [ ] 所有 `addEventListener('message', ...)` handler 都校验 `event.origin`，且用严格 `===`（非 `includes` / `endsWith`）
- [ ] handler 校验 `event.source` 或至少校验 `event.data` schema
- [ ] 所有 `postMessage(data, target)` 发送端 target 不是 `'*'`

**Token / 凭据维度**：
- [ ] Token / JWT / 会话凭据未存 localStorage / sessionStorage
- [ ] 敏感数据未明文挂在 `window.*` 全局变量
- [ ] 前端 bundle 内无硬编码密钥 / 私钥 / 高权限 API key

**客户端决策维度**：
- [ ] 前端"角色 / 权限"判定有后端对应 API 鉴权（或标 `suspected` 待后端审计）
- [ ] feature flag 不承担安全决策；敏感算法密钥不嵌入前端 bundle

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 `dataflow-analysis` / 单漏洞 skill / `result-with-file` 机器消费；产物聚合或省略会让整条链路失效，因此是刚性要求。

### 白盒判定上限

本能力作为白盒原子能力，判定上限为 `static-confirmed`（客户端 source 到客户端 sink 数据流可达且中间无 sanitize / 无 origin 校验 / 无白名单），**不等于动态 confirmed**。升级到 `confirmed` 必须靠浏览器实际触发（注入 payload 触发 DOM 修改 / 触发 alert / 通过 origin 伪造发消息成功执行）。

**static-confirmed（白盒上限，落 `status=needs_review`）**：
- 客户端 source 到客户端 sink 数据流静态可达
- sink 是危险形态（`innerHTML` / `eval` / `v-html` / `dangerouslySetInnerHTML` / `[innerHTML]` + `bypassSecurityTrust*` / `{@html}` 等）
- 中间无 DOMPurify / 无框架自动转义（绕过通道）/ 无白名单
- postMessage 漏洞：handler 无 origin 校验 或 origin 校验有缺陷（参 §10 反例 5）

**static-unknown（落 `status=needs_review` + 标注 unknown）**：参 §11，构建产物未含 source map / 第三方 SDK（混淆）/ 运行时动态加载的脚本等情形

**not_vulnerable（落 `status=not_vulnerable`）**：
- sink 接收的字符串已被 DOMPurify.sanitize 或等价库处理
- postMessage handler 第一行严格 origin 校验 + 数据 schema 校验
- Token 存 HttpOnly + Secure + SameSite Cookie，localStorage 仅存非敏感缓存

**升级路径**（白盒不能独立给 confirmed）：
- DOM XSS：浏览器实际触发——构造 URL hash / 路由 / postMessage payload，看是否真触发 sink（弹 alert / 修改 DOM）
- postMessage 越权：构造跨 origin 页面发消息，看 handler 是否真执行
- Token 泄露：注入测试脚本调 `localStorage.getItem('token')`，看是否真读到 Token
- 客户端鉴权：黑盒直接调被前端隐藏的 API，看后端是否返回 200

**禁止**白盒独立判 `confirmed`——无可观测效果证据，仅静态可达不构成动态利用。

### 产物契约（必须遵守）

**为什么这里是「必须」**：产物结构是下游机器消费的接口，聚合 / 省略会让 `result-with-file` 计数闸门失效，并让单漏洞 skill 无法回溯到具体 file:line。

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/client-js-audit.jsonl`：

```json
{
  "id": "client-js-001",
  "title": "DOM XSS: location.hash 流入 innerHTML 无 sanitize",
  "severity": "high",
  "cwe": "CWE-79",
  "source": "location.hash",
  "sink": "Element.innerHTML",
  "file_location": "src/views/Profile.vue:42",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "source_report": "client-js-audit",
  "description": "..."
}
```

字段约束：
- `id` 带 `client-js-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒上限默认 `needs_review`）
- `confidence ∈ static-confirmed | static-unknown`
- `(file, sink_type, source_type)` **三元组任一不同即各自独立成行**——禁止合并折叠（例：同一文件 `Profile.vue` 里既有 `innerHTML` 又有 `v-html` 是 2 行；`location.hash → innerHTML` 与 `postMessage → innerHTML` 是 2 行）
- `file_location` 填 `file:line`，不留空、不写区间
- 用 `source_type` / `sink_type` 字段区分（如 `source_type=location_hash` / `sink_type=v_html`）

**禁止**：
- 聚合计数（"12 处 DOM XSS"）—— 丢失了具体位置
- "等" / "（其余略）" 省略 finding
- 只看 `dist/` 而不看 `src/` 就宣称"已完整审计"

### 反例义务（必须遵守）

写"未发现客户端 JS 漏洞"或"已防护"前，产物必须包含：
- 所有 DOM sink 候选位置完整清单（grep 覆盖证据）
- 所有客户端 source 候选位置完整清单
- 所有 postMessage handler 清单与 origin 校验结论
- 所有 Token 存储位置清单
- 所有前端"角色 / 权限"判定位置清单 + 是否标 `suspected` 待后端审计
- `static-unknown` 单元格的具体原因（source map 缺失 / 第三方 SDK / 动态脚本 / Web Worker 等）

清单不完整 → 结论降级为 `partial-coverage`。

---

## 10. 具象化反例库（共享章节）

### FP（看似命中实际不构成）

**反例 1：`innerHTML` 接收的是 DOMPurify 输出**

- 抽象规则：sanitize 后的字符串进 `innerHTML` 是预期安全用法
- 具体场景：`el.innerHTML = DOMPurify.sanitize(userInput)`
- 关键识别特征：sink 前一行就是 sanitize 调用；DOMPurify / sanitize-html 等知名库
- 排除方法：确认 DOMPurify 版本 ≥ 2.0（早期版本有绕过 CVE）；确认未传 `ALLOWED_TAGS` 包含 `<script>` 等极宽配置

**反例 2：React `dangerouslySetInnerHTML` 来自 Markdown 渲染器**

- 抽象规则：Markdown 库渲染后默认走 sanitize
- 具体场景：`<div dangerouslySetInnerHTML={{__html: marked(content, {sanitize: true})}} />`
- 关键识别特征：上游是 marked / markdown-it / remark 等，且启用了 sanitize 选项
- 排除方法：核对库版本（marked 4+ 删了 `sanitize` 选项，改用 DOMPurify 链式调用）；确认实际启用

**反例 3：postMessage handler 严格 origin 校验**

- 抽象规则：第一行就 `if (event.origin !== TRUSTED) return;` 后续逻辑安全
- 具体场景：`if (event.origin !== 'https://trusted.example.com') return; processData(event.data);`
- 关键识别特征：origin 校验是严格 `===`、白名单是字面量字符串
- 排除方法：核对 TRUSTED 常量定义，确认不是 `.com` / `trusted` 这类前缀匹配

**反例 4：localStorage 存非敏感 cache**

- 抽象规则：localStorage 不是绝对不能用，存非敏感数据合理
- 具体场景：`localStorage.setItem('theme', 'dark')` / `localStorage.setItem('lang', 'zh-CN')`
- 关键识别特征：键名语义是 UI / 偏好缓存，不是 token / jwt / auth
- 排除方法：枚举所有键名，按语义判定

### FN（看似不命中实际是真洞）

**反例 5：postMessage origin 校验 typo（partial match）**

- 抽象规则：用 `includes` / `endsWith` / 正则 partial match 校验 origin 都可绕过
- 具体场景：`if (!event.origin.includes('good.com')) return;`——`evil-good.com` 也命中；`if (!event.origin.endsWith('good.com')) return;`——`evilgood.com` 也命中
- 关键识别特征：origin 校验用了 `includes` / `endsWith` / `match` 而不是 `===`
- 确认方法：构造 `evil-good.com` / `goodAcom` 类 origin 测试

**反例 6：Vue `v-bind:href="userInput"` 缺 `javascript:` 协议拦截**

- 抽象规则：Vue / React 不拦截 `javascript:` 协议
- 具体场景：`<a v-bind:href="userInput">` / `<a href={userInput}>`，userInput 是 `javascript:alert(1)` 时点击触发
- 关键识别特征：href 接收变量且无协议白名单（如 `userInput.startsWith('http')`）
- 确认方法：grep `v-bind:href` / `href={` 看上游是否有协议校验

**反例 7：Vue 模板编译期 v-html 用户控制**

- 抽象规则：Vue runtime-compiler 模式下，模板字符串本身被编译——含用户输入的模板字符串等于 RCE
- 具体场景：`new Vue({template: userInput})` 或 `Vue.compile(userInput)`
- 关键识别特征：使用了 Vue 完整版（runtime-compiler）而非 runtime-only；动态 template 字符串
- 确认方法：核对 `vue.runtime.esm.js` vs `vue.esm.js` 引入；grep `template:` 后接变量的位置

**反例 8：Cookie Token 缺 HttpOnly / Secure / SameSite**

- 抽象规则：把 Token 写在 Cookie 上但缺关键属性 = localStorage 等价
- 具体场景：后端 `Set-Cookie: token=xxx`（无 HttpOnly）或前端 `document.cookie = 'token=xxx'`
- 关键识别特征：JS 能读到 Cookie 里的 Token；Cookie header 缺 `HttpOnly` / `Secure` / `SameSite=Strict|Lax`
- 确认方法：抓响应头看 Set-Cookie 属性；前端 grep `document.cookie` 看是否读 Token

**反例 9：客户端鉴权背后无后端鉴权**

- 抽象规则：前端隐藏菜单 + 后端无对应鉴权 = 越权
- 具体场景：admin 菜单 `v-if="user.role === 'admin'"` 隐藏，但 `/api/admin/users` 端点后端无鉴权
- 关键识别特征：前端鉴权判定 + 同 URL 在普通用户身份下直接请求成功
- 确认方法：白盒只能标 `suspected`，需黑盒访问对应 API 端点确认

### 易混淆案例

**反例 10：构建产物（`dist/`）扫不到真洞**

- 抽象规则：dist 是混淆后产物，sink / source 命名都被改名
- 具体场景：grep `innerHTML` 在 `dist/main.abc123.js` 里漏掉但 `src/views/Profile.vue` 里有
- 关键识别特征：扫描面只含 dist 而无 src
- 排除方法：确认扫描面以源码仓库为主；若仅有 dist 走 §11

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到源码 + AST 模式匹配为止。CSP 不评估（属 [csp-audit](../csp-audit/SKILL.md)）。

下面这些情形数据流分析无法继续追踪，**必须标 `static-unknown`**，不允许默认为 not_vulnerable：

1. **构建产物未含 source map**
   - 仅有 `dist/*.js` 混淆代码，无 source map，原始变量名 / 函数名不可恢复
   - **处置**：标 `static-unknown` 记录混淆来源；尝试启用 source map 重新构建源码仓库；不行则结论降级 `partial-coverage`

2. **第三方 SDK（混淆 / 闭源）**
   - 接入的 `analytics-sdk.min.js` / `payment-widget.js` / 三方聊天 widget——闭源混淆
   - 但该 SDK 注入到 DOM 或接受 postMessage
   - **处置**：标 `static-unknown` 记录 SDK 名 + 引入位置；推 dependency-decompile 或直接询问 SDK 提供方

3. **运行时动态加载脚本**
   - `document.createElement('script'); s.src = ...`
   - `import(dynamicURL)` ESM 动态导入
   - **处置**：标 `static-unknown` 记录加载点；若加载源固定可读，单独审计被加载脚本

4. **Web Worker / Service Worker 上下文**
   - Worker 上下文不能访问 DOM，sink 集合不同（但 Worker 可 postMessage 回主线程触发 DOM sink）
   - Service Worker 可拦截 fetch 改包响应
   - **处置**：标 `static-unknown` 记录 Worker 文件；按 Worker 上下文独立审计

5. **iframe 跨 origin 调用**
   - 嵌入的 iframe 来自其他 origin，本仓库看不到对方源码
   - **处置**：标 `static-unknown` 记录 iframe src；按对方域名判断是否需要扩大审计范围

6. **浏览器扩展注入脚本上下文**
   - content_script 注入到任意页面，DOM 上下文是被访问页的
   - **处置**：标 `static-unknown` 记录注入规则；按目标页独立审计

7. **运行时配置 / feature flag 驱动**
   - 加载哪段渲染逻辑取决于运行时配置（如 A/B 测试分支）
   - **处置**：每个分支独立审计；不能只看 default 分支结论

8. **打包工具的 tree-shaking / 代码分割**
   - 动态 import 按路由切分代码，源码扫描完整但运行时实际加载哪些 chunk 不确定
   - **处置**：本节静态可达性按源码完整算；运行时实际触发条件留给黑盒

**CSP 边界**：本能力**不评估** CSP——CSP 严格度影响 DOM XSS 利用难度但不消除漏洞。CSP 评估走 [csp-audit](../csp-audit/SKILL.md)。

**底线**：本能力写"该子系统无客户端 JS 漏洞"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级为 `partial-coverage`。

---

## 12. 修复建议（共享章节）

### 源头治理（首选）

- **DOM sink**：所有需要插入用户内容的位置，优先用安全替代——`textContent` 替代 `innerHTML`；框架自带 `{{ }}` / `{value}` 自动转义；需富文本走 DOMPurify.sanitize（最新版本）后再进 sink
- **`href` / `src` 属性**：协议白名单（`if (!/^https?:/.test(url)) return;`），拒绝 `javascript:` / `data:` 协议
- **postMessage handler**：
  - 第一行严格 origin 校验：`if (event.origin !== EXPECTED_ORIGIN) return;`
  - 校验 `event.source === expectedWindow`
  - 数据 schema 校验（JSON.parse + 字段类型 / 枚举值校验）
  - handler 内**不进** eval / innerHTML / 危险 sink
- **postMessage 发送端**：`target.postMessage(data, 'https://trusted.example.com')`，**禁止** `'*'`
- **Token / 凭据**：走 HttpOnly + Secure + SameSite=Strict / Lax Cookie；后端管理 session；localStorage 仅存非敏感缓存
- **前端权限判定**：仅做 UI 装饰（菜单显隐 / 按钮禁用），后端每个 API 独立鉴权
- **敏感算法**：加签 / 加密走后端，密钥不嵌入前端 bundle；前端只做请求构造，不做"安全决策"
- **Vue / React**：避免 runtime-compiler 模式；Vue 用 runtime-only 构建；React 不动态 createElement(userInput)

### 边界过滤（次选，深度防御）

- CSP 兜底：参 [csp-audit](../csp-audit/SKILL.md) 配置严格 CSP（禁 `unsafe-inline` / `unsafe-eval`，配 nonce / hash）
- DOMPurify 全局兜底：所有 v-html / dangerouslySetInnerHTML 入口统一封装

### 兜底拒绝

- 关键操作二次确认：高危操作走后端独立鉴权 + 二次验证（短信 / 邮件 / OTP）
- 错误响应不暴露 Token / 内部 URL / 内部 API path

### 参考

- [OWASP DOM XSS Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/DOM_based_XSS_Prevention_Cheat_Sheet.html)
- [OWASP HTML5 Security Cheat Sheet — Cross Document Messaging](https://cheatsheetseries.owasp.org/cheatsheets/HTML5_Security_Cheat_Sheet.html#cross-document-messaging)
- [OWASP JSON Web Token Cheat Sheet — Token Storage on Client Side](https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_for_Java_Cheat_Sheet.html#token-storage-on-client-side)
