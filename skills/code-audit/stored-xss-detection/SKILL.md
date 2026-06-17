---
name: stored-xss-detection
description: >-
  存储型 XSS 白盒数据流审计——按"用户输入 → 持久层（DB / 文件 / Redis）→ 回读 → 模板 / 前端
  输出"跨持久层介质追 source-sink 可达性，识别 encoder 与输出上下文（HTML body / attr / JS / URL /
  CSS）不匹配、富文本白名单缺口、Markdown 渲染保留原始 HTML、二阶回读路径漏 sanitize。
when-to-use: 当需要专门分析“用户输入 -> 持久化 -> 二次展示”链路，或者普通 XSS sink 规则不足以确认高危 XSS 时
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 存储型 XSS 白盒数据流审计

## 1. 触发线索 / 适用信号

按"输入字段命名 + 模板引擎 + DB 表字段"三维识别本能力命中场景（不按业务命名）。

**代码 pattern 维度**（grep 命中模式）：
- 用户字段写入：`INSERT INTO` / `UPDATE` 接 `comment` / `title` / `description` / `nickname` / `bio` / `content` / `remark` / `name` 列
- 模板 raw 输出：`th:utext` / `<#noautoesc>` / `?no_esc` / `{{ var|safe }}` / `<%- %>` / `v-html` / `dangerouslySetInnerHTML` / `innerHTML\s*=`
- 富文本/Markdown：`marked(` / `markdown-it` / `Quill` / `TipTap` / `wangEditor` / `tinymce`
- SSR 直写：`response.write(html)` / `res.send('<...>'+ ...)` / `template.HTML(...)` (Go)

**文件结构 / 命名约定维度**：
- `src/main/resources/templates/`（Spring + Thymeleaf）/ `WEB-INF/jsp/` / Freemarker `.ftl`
- Django `templates/*.html` + `views.py`
- Express `views/*.ejs` / `*.pug` / `*.hbs`
- Vue `*.vue` SFC、React `*.tsx` / `*.jsx` SSR 入口（`server.tsx` / `entry-server.ts`）
- `*Mapper.xml` 里 `INSERT/UPDATE` 用户字段；ORM `models/` 含富文本字段类型

**依赖 / 注解维度**：
- `pom.xml` 含 `thymeleaf` / `freemarker` / `velocity` / `owasp-java-html-sanitizer`
- `package.json` 含 `dompurify` / `sanitize-html` / `marked` / `markdown-it` / `vue` / `react-dom/server` / `ejs`
- `requirements.txt` 含 `django` / `jinja2` / `bleach` / `markdown`
- `go.mod` 含 `html/template`（安全默认）vs `text/template`（无 escape）

**反向信号**（不命中本能力）：
- source 只是 URL query 直接回显（属反射型 XSS / dom-xss）
- 输入未经任何持久层介质，纯单请求生命周期（属反射 / DOM）
- 输出位置纯文本（`{{ var }}` auto-escape 默认、`textContent`、`th:text`）且无 raw 旁路

---

## 2. 造成原因（共享章节）

source 是任何用户可控输入（HTTP 参数 / 上传文件内容 / 文件名与 meta / 第三方回调字段），sink 是模板或前端的 HTML 渲染上下文：HTML body / attribute / JavaScript / URL（`href` / `src`）/ CSS / SVG / Markdown 原生 HTML。**与反射型 XSS 的根本区别**在于 source 经持久层介质（DB / 文件系统 / Redis / 对象存储 / 消息队列），跨请求 / 跨用户生命周期触发。

**任何 source 入库时未做 HTML 转义、回读到 sink 时 encoder 不匹配输出上下文，即构成存储型 XSS**——浏览器解析时把数据当作脚本指令执行。安全的默认形态有两条路径：（a）入库前用白名单 sanitizer（如 OWASP Java HTML Sanitizer / DOMPurify / bleach / bluemonday）把内容净化为受控 HTML；（b）输出时按上下文走匹配 encoder（HTML body 用 `&lt;` 编码、attribute 用 quote+entity、JS 上下文用 `\xHH`、URL 上下文校验协议白名单）。

encoder 必须**匹配输出上下文**：HTML entity encoding 救不了 `<script>` 内的 JS 上下文，URL encoding 救不了 `javascript:` 协议注入。富文本场景下白名单必须覆盖**标签 + 属性 + URL 协议**三层，缺一即被绕过。

---

## 3. 领域 source-sink 数据流模型

**代码层 source 集合**（按持久层介质）：
- **DB 字段**：用户输入入库的列（`comment.content` / `user.bio` / `article.body` / `document.title`）；任何 `INSERT` / `UPDATE` 端点写入的列即潜在 source
- **文件内容 / 文件名 / meta**：上传后落盘的 SVG / HTML / Markdown / Office 内容；文件名进 DB
- **Redis / KV 缓存**：会话级用户数据缓存、富文本编辑稿缓存
- **二阶 source**：已入库字段被另一段 SQL 回读后再入新表的中继场景
- **跨服务回调**：MQ / RPC / Webhook 写入的字段被前台展示

**代码层 sink 集合**（按输出上下文）：
- **服务端模板 raw 输出**：
  - Thymeleaf `th:utext="${var}"` / `[(${var})]` 非转义语法
  - Freemarker `<#noautoesc>...${var}...</#noautoesc>` / `${var?no_esc}`
  - Django `{{ var|safe }}` / `{% autoescape off %}`
  - Jinja2 `{{ var|safe }}` / `Markup(...)`
  - Velocity `$!{var}` 在 strict-ref 关闭时
  - Go `text/template` 全部输出 / `html/template` 中 `template.HTML(var)` 强类型旁路
- **前端框架 raw 渲染**：
  - Vue `v-html="var"` / 手动 `el.innerHTML = ...`
  - React `dangerouslySetInnerHTML={{ __html: var }}`
  - Angular `[innerHTML]="var"`（默认走 DomSanitizer，但 `bypassSecurityTrustHtml` 会旁路）
- **JS DOM API**：`innerHTML` / `outerHTML` / `insertAdjacentHTML` / `document.write` / `eval` / `new Function`
- **SSR 直写**：`response.write(string)` / `res.send(htmlString)` / Servlet `out.print(string)`
- **Markdown 渲染器 raw HTML 通道**：`marked({ sanitize: false })` / `markdown-it({ html: true })` / `commonmark` 默认允许原始 HTML
- **富文本编辑器回显**：Quill Delta → HTML、TipTap JSON → HTML 时白名单缺失

**数据流追踪规则**：
- **跨持久层追踪**：列出所有写入字段 X 的端点 → 列出所有回读字段 X 的端点 → 写读交叉集合即候选；任一回读路径用 raw sink 即真洞
- **跨服务边界**：本服务范围内追到出站调用即停，下游服务由对应实例独立审计
- **sanitize 位置识别**：入库前 sanitize（防一阶）vs 回读后 sanitize（防二阶）；同字段两处都做才能算"全链路覆盖"
- **encoder 上下文匹配**：模板默认 auto-escape 是 HTML body 上下文的 encoder，不适用 attribute 无引号 / JS 嵌入 / URL 协议位置
- **闭源依赖**：自定义 sanitizer / 闭源富文本编辑器后端规则不可见时入 §11 静态分析边界

---

## 4. 常见类型（共享章节）

| 类型 | 输出上下文 / 触发位置 | 关键识别特征 |
|---|---|---|
| **HTML body 上下文 XSS** | `<div>{{var}}</div>` 走 raw 输出 | sink 直接渲染 `<script>` / `<img onerror>` 等标签 |
| **Attribute 上下文 XSS** | `<a title="{{var}}">` 缺引号 / 引号未转义 | 单/双引号可破出属性边界 |
| **JS 上下文 XSS** | `<script>var x = '{{var}}'</script>` | 嵌入到 `<script>` 内，需 JS 字符串 escape |
| **URL 上下文 XSS** | `<a href="{{var}}">` / `<img src="{{var}}">` | `javascript:` / `data:text/html` 协议未过滤 |
| **CSS 上下文 XSS** | `<style>...{{var}}...</style>` / `style="..."` | `expression()` / `url(javascript:...)` |
| **SVG 上下文 XSS** | 上传 SVG 在线预览 / `dangerouslySetInnerHTML` 渲染 SVG | SVG 内 `<script>` / `onload` 可执行 |
| **Mutation XSS（mXSS）** | DOMPurify 默认配置经 innerHTML 解析后变异 | 浏览器 HTML parser 兼容性导致清洗后 DOM 变形 |
| **富文本白名单绕过** | Quill / TipTap / wangEditor 后端 sanitize 不覆盖 `formaction` / `srcset` 等冷门属性 | 白名单只覆盖常见标签属性，遗漏冷门触发 |
| **Markdown 原生 HTML 注入** | `marked` / `markdown-it` `html: true` | Markdown 中含原始 HTML 标签被保留 |
| **二阶回读漏 sanitize** | A 端点 sanitize 入库、B 端点不再做 / 用错 encoder | "已审过 → 不再 sanitize" 的二次路径 |

---

## 5. 入口点定位

按项目结构同时找**写入点**（INSERT/UPDATE）和**回读输出点**（模板 / 列表 / 详情接口）。

> 下列框架 / 项目类型仅作类似项目示例，不限于此；以目标实际栈为准。

| 栈 | 写入点典型位置 | 回读 / 输出点典型位置 |
|---|---|---|
| Java / Spring + Thymeleaf | `@PostMapping`/`@PutMapping` → `*Service` → `*Repository.save()` / `*Mapper.xml` INSERT | `src/main/resources/templates/*.html` 找 `th:utext` / `[( ... )]`；JSON 返回 + 前端 Vue/React `v-html` |
| Java + JSP / Freemarker | 同上 | `WEB-INF/jsp/*.jsp` 找 `<%= %>` 无 `fn:escapeXml`；`.ftl` 找 `<#noautoesc>` / `?no_esc` |
| Python / Django | `views.py` POST handler → `Model.objects.create()` / `.save()` | `templates/*.html` 找 `{{ var\|safe }}` / `{% autoescape off %}` |
| Python / Flask + Jinja2 | `request.form/json` → SQLAlchemy `session.add(...).commit()` | `templates/*.html` 找 `{{ var\|safe }}` / `Markup(...)` / `render_template_string(...)` |
| Node.js / Express + EJS/Pug/HBS | `req.body` → Sequelize/TypeORM/mongoose `.create()` / `.save()` | `views/*.ejs` 找 `<%- var %>`；Pug `!{var}`；Handlebars `{{{var}}}` |
| Vue SSR | API 端点 | `*.vue` 找 `v-html`；`entry-server.ts` 渲染管线手写 `innerHTML` |
| React SSR | API 端点 | `*.tsx` / `*.jsx` 找 `dangerouslySetInnerHTML`；`renderToString` 入口 |
| Go / Gin + html/template | handler → Gorm `db.Create(...)` | `.tmpl` / `.html` 模板；`template.HTML(s)` 强类型旁路；`text/template` 全无转义 |
| PHP / Laravel + Blade | Controller → Eloquent `Model::create()` | Blade `{!! $var !!}`（raw）vs `{{ $var }}` |

**通用建议**：用 [sast-scan](../sast-scan/SKILL.md) 先粗筛模板 raw 输出 sink 与 ORM 写入点；用 [dataflow-analysis](../dataflow-analysis/SKILL.md) 跨函数追写入字段在哪些查询/接口被回读；路由文件（Spring 注解 / Express `routes/` / Django `urls.py` / Gin `router.go`）作为入口锚点反向追 sink 候选；闭源富文本 / 闭源 sanitizer 见 §11 静态分析边界。

---

## 6. 跨框架代码变体

下表覆盖主流模板引擎 / 前端框架的"安全形态 vs 危险形态"对照。

| 框架 / 上下文 | 安全形态（escape） | 危险形态（raw） |
|---|---|---|
| **Thymeleaf** | `th:text="${var}"`（auto-escape） | `th:utext="${var}"` / `[(${var})]` |
| **Freemarker** | auto-escape ON（`<#ftl auto_esc=true>`） / `${var?html}` | `<#noautoesc>${var}</#noautoesc>` / `${var?no_esc}` |
| **JSP** | `<c:out value="${var}"/>` / `fn:escapeXml(var)` | `<%= var %>` / `${var}` EL 直出 |
| **Velocity** | strict-ref + 显式 escape tool | `$!{var}` / `$var` 默认无转义 |
| **Django templates** | `{{ var }}`（auto-escape） | `{{ var|safe }}` / `{% autoescape off %}` / `mark_safe(var)` |
| **Jinja2** | `{{ var }}`（auto-escape，需 env 启用） | `{{ var|safe }}` / `Markup(var)` / `render_template_string(user_input)` |
| **Vue** | `{{ var }}` / `:text="var"` / `v-text` | `v-html="var"` / 手写 `el.innerHTML = var` |
| **React** | `{var}` JSX 默认 escape | `dangerouslySetInnerHTML={{__html: var}}` |
| **Angular** | `{{ var }}` / 默认 `[innerHTML]` 走 DomSanitizer | `bypassSecurityTrustHtml(var)` / `[innerHTML]` 后传入 trusted |
| **EJS** | `<%= var %>`（escaped） | `<%- var %>` |
| **Pug** | `#{var}` / `=var`（escaped） | `!{var}` / `!=var` |
| **Handlebars** | `{{var}}`（escaped） | `{{{var}}}` / `triple-stash` |
| **Mustache** | `{{var}}` | `{{{var}}}` / `{{&var}}` |
| **Blade（Laravel）** | `{{ $var }}` | `{!! $var !!}` |
| **Go html/template** | 默认按上下文 auto-escape | `template.HTML(s)` / 切换到 `text/template` |
| **JS DOM** | `el.textContent = var` / `el.setAttribute('title', var)` | `el.innerHTML = var` / `insertAdjacentHTML` / `document.write` |
| **Markdown / marked** | `marked(src, { sanitize: true })`（旧版）/ 配 DOMPurify 后处理 | `marked(src)` 默认保留原始 HTML（v4+ 移除 sanitize 必须外挂） |
| **Markdown / markdown-it** | `MarkdownIt({ html: false })` 默认 | `MarkdownIt({ html: true })` |

**富文本 / sanitizer 特殊位**：
- **DOMPurify**：默认配置可能被 mXSS 绕过；`ADD_TAGS` / `ADD_ATTR` 扩白名单时需评估
- **OWASP Java HTML Sanitizer**：使用 `Sanitizers.FORMATTING.and(Sanitizers.LINKS)` 等链式 policy；遗漏 policy 即不防对应类
- **bleach（Python）**：默认 `ALLOWED_TAGS` 不含 `style` / `iframe`，但允许的属性里若有 `style` 仍可注入 CSS XSS
- **bluemonday（Go）**：`UGCPolicy()` 是 user-generated content 默认策略；自定义 policy 易遗漏 URL scheme 校验

---

## 7. 思考检查点（共享章节）

加载本 skill 时按这些问题思考：

- 这个用户字段从写入端点入库，被哪些 SELECT 回读？每个回读出口的输出位置（HTML body / attr / JS / URL / CSS）encoder 是否匹配？
- 是不是所有写入路径都做了 sanitize？只要一处遗漏，回读 sink 是 raw 输出即真洞——同字段多写入端点要全部覆盖。
- 富文本白名单是否真覆盖**标签 + 属性 + URL 协议**三层？`onerror` / `srcset` / `formaction` / `data:` / `javascript:` 协议遗漏即被绕过。
- Markdown 渲染器是否启用 sanitize？`marked` v4+ 已移除内建 sanitize 选项，必须外挂 DOMPurify；`markdown-it` 默认 `html: false` 但项目可能改 `true`。
- 是不是二阶？"已审过 → 不再 sanitize" 的二次回读路径常见——同字段在 SELECT 后被拼进新 HTML 上下文（如复制到通知模板、邮件 HTML、导出 PDF）。
- 是否有跨用户角色的差异渲染链——前台 sanitize、管理后台为"运营方便"用 raw 输出？

---

## 8. 检测方法论 / 数据流追踪

> 本能力**只到 static-confirmed**——动态利用证据走对应黑盒 / graybox skill。本节方法论描述白盒数据流追踪，不规定 plan / step 编排。

### Step 0：基线侦察

- 加载 [project-framework-analysis](../project-framework-analysis/SKILL.md) 输出，识别 web 框架 / 模板引擎 / 前端框架 / ORM / sanitizer 库及版本
- 列出本项目持久层模型所有字段（`models/` / `*Mapper.xml` / `entity/`），用户可控字段集合作为 source 候选
- 已有 [sast-scan](../sast-scan/SKILL.md) 输出时优先消费其 high_noise_patterns + needs_dataflow_confirmation 桶里 XSS 相关条目

### Step 1：grep 出所有写入点

```bash
# ORM 写入
rg -n '\.(Create|Save|Update|Updates|insert|create|save|upsert)\s*\(' <target_path>
# 原生 SQL 写入
rg -n -i 'INSERT INTO|UPDATE\s+\w+\s+SET' <target_path>
# MyBatis XML 写入
rg -n -i '<insert\b|<update\b' --type xml <target_path>
# 上传落盘
rg -n '(PutObject|WriteFile|writeFile|fs\.write|os\.WriteFile)' <target_path>
```

把命中按"写入字段"分类，得到字段级别的写入点清单。

### Step 2：grep 出所有 raw 输出 sink

```bash
# 服务端模板 raw
rg -n 'th:utext|<#noautoesc|\?no_esc|\|safe\b|mark_safe|Markup\(|<%- |!\{|\{!\!|template\.HTML\(' <target_path>
# 前端 raw
rg -n 'v-html|dangerouslySetInnerHTML|\.innerHTML\s*=|insertAdjacentHTML|document\.write' <target_path>
# Markdown raw
rg -n 'marked\s*\(|MarkdownIt\s*\(|markdown-it.*html\s*:\s*true' <target_path>
# 绕过 / 显式信任
rg -n 'bypassSecurityTrustHtml|trustAsHtml|template\.HTML\(|ADD_TAGS' <target_path>
```

每条命中标记输出上下文（HTML body / attr / JS / URL / CSS）。

### Step 3：写读交叉

对每个写入字段，跨函数追踪它在哪些 SELECT / Find / Query 被读出 → 读出后流向哪些响应 / 模板上下文。

工具加速：调用 [dataflow-analysis](../dataflow-analysis/SKILL.md) 做跨函数 source-to-sink 追踪；DB 字段维度的跨端点关联可结合 grep 字段名同名字段（`comment.content` 在 Mapper / Repository / Controller / template 中出现）。

写读交叉成立的字段 = **候选**——进入 Step 4 判 sanitize。

### Step 4：判 sanitize 实际位置

对每个候选字段追：
- **入库前 sanitize**：写入 handler / Service / pre-save hook 是否调用 sanitizer（OWASP HTML Sanitizer / DOMPurify(server) / bleach / bluemonday）
- **回读后 sanitize**：读出后在 Controller / 模板渲染前是否调用 sanitizer
- **encoder 上下文匹配**：模板默认 escape 仅适用 HTML body；attr 无引号 / JS 嵌入 / URL 协议位置需独立检查
- **白名单覆盖度**：富文本 sanitizer 的 allowed tags / attributes / URL schemes 是否覆盖 `javascript:` / `data:` 协议、`on*` 事件、`style` 属性

判定形态：
- **入库前未 sanitize + 回读 raw 输出 + 字段在写读交叉** → **static-confirmed**
- **入库前 sanitize 但用了不匹配上下文的 encoder**（如只做 HTML body escape，回读位置在 attr 无引号） → **static-confirmed**
- **入库前 sanitize 但白名单缺 URL scheme / on*** → **static-confirmed**（按缺口具体位置）
- **写读交叉成立 + sanitize 链完整 + encoder 匹配** → **not_vulnerable**

### Step 5：富文本特殊审

白名单类 sanitizer 的配置审：
- DOMPurify 配置项 `ALLOWED_TAGS` / `ALLOWED_ATTR` / `ALLOWED_URI_REGEXP` / `FORBID_*` / `ADD_TAGS` 实际值
- OWASP Java HTML Sanitizer 的 policy 组合是否覆盖所有可执行属性
- bluemonday `Policy` 自建时检查 `AllowAttrs(...).OnElements(...)` 是否遗漏 URL scheme 校验

mXSS 风险：DOMPurify 默认配置在某些 HTML parser 兼容性场景下可被绕过，标 `static-confirmed` 并附 PoC 范式或交黑盒验证。

### Step 6：二阶通道扫描

- 列出所有 INSERT / UPDATE 端点的写入字段
- 对每个写入字段 grep 哪些 SELECT 引用，每个回读点独立看 sanitize 状态
- 重点：跨子系统（前台 / 管理后台 / 客服 / 邮件模板 / 导出 PDF）复用同字段时差异渲染常出问题

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 所有用户可控字段写入点已枚举（按字段维度 grep 完）
- [ ] 所有 raw 输出 sink（模板 / 前端 / Markdown）已枚举
- [ ] 写读交叉集合已构造，每个 (write, read, field) 三元组单独看 sanitize 状态
- [ ] 富文本字段 sanitizer 配置已审（白名单标签 / 属性 / URL scheme 三层）
- [ ] Markdown 渲染器 `html` 选项 / 外挂 DOMPurify 已确认
- [ ] 二阶通道（同字段多回读点）已扫描完
- [ ] 跨子系统差异渲染（前台 vs 后台 vs 邮件模板）已对照
- [ ] 闭源 sanitizer / 闭源富文本编辑器后端 / 动态模板选择路径已标 `static-unknown`

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本漏洞特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 `result-with-file` 机器消费、按 (write_endpoint, read_endpoint, field) 三元组的覆盖完整性核验；产物聚合或省略会让整条链路失效，因此是刚性要求。

### 白盒判定上限

本能力作为白盒数据流审计原子能力，判定上限为 `static-confirmed`，**不等于动态 confirmed**。

**static-confirmed（落 `status=needs_review` + `confidence=high_confidence`）**：
- 写入路径不 sanitize 或 sanitize 链缺口明确（白名单遗漏 URL scheme / on* / `style`）
- 回读模板使用 raw 输出 sink（`th:utext` / `v-html` / `dangerouslySetInnerHTML` / `innerHTML` / `{{|safe}}` / `<%- %>` / Markdown raw HTML）
- 字段在 (write, read) 交叉集合
- encoder 与输出上下文不匹配（HTML body encoder 用在 attr 无引号 / JS / URL 位置）

**static-unknown（落 `status=needs_review` + 标注 unknown）**：
- 动态模板渲染器选择（feature flag / 配置驱动）
- SSR 与 CSR 双路径并存且追踪不完
- 富文本 sanitizer 版本 / 配置闭源
- 自定义 sanitizer 实现无源码可见
- 闭源富文本编辑器后端规则不可见

**not_vulnerable（落 `status=not_vulnerable`）**：
- 字段不在写读交叉
- 所有写入路径 sanitize + 所有回读路径 encoder 匹配输出上下文
- 富文本走严格白名单且覆盖标签 / 属性 / URL scheme 三层
- 字段全程纯文本上下文（`textContent` / `th:text` / `{{ var }}` auto-escape 且无 raw 旁路）

### 升级路径（白盒不能独立给 confirmed）

- 走 graybox 流程：用白盒候选三元组指导黑盒端测试
- 黑盒端在浏览器实际渲染触发，按 `pentest/xss-detection` 等 skill 的闭环要求收可观测效果证据
- 黑盒拿到强证据后，结论从 `static-confirmed` 升为 `confirmed`

**禁止**白盒独立判 `confirmed`——无浏览器实际渲染触发证据，仅静态可达不构成动态利用。

### 产物契约（必须遵守）

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/stored-xss-detection.jsonl`，不等汇总阶段回头整理（why："事后总结"是聚合 / 区间 / "等"省略的根源）：

```json
{
  "id": "stored-xss-001",
  "title": "评论 content 字段经 v-html 回显未 sanitize",
  "severity": "high",
  "cwe": "CWE-79",
  "source": "comment.content (INSERT in POST /api/comment)",
  "sink": "v-html in CommentList.vue:42",
  "entry_point": "GET /article/:id",
  "status": "needs_review",
  "confidence": "high_confidence",
  "file_location": "src/views/CommentList.vue:42",
  "source_report": "stored-xss-detection",
  "description": "..."
}
```

字段约束：
- `id` 带 `stored-xss-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒默认 `needs_review`）
- `(write_endpoint, read_endpoint, field)` **三元组任一不同即各自独立成行**——同字段在多个回读入口点 / 多个写入端点出现时禁止合并折叠
- `source` 填写入端点 + 字段；`sink` 填回读输出 API + file:line；`entry_point` 填回显页面的 HTTP 入口点（method+URL）
- `file_location` 填 `file:line`，不留空、不写区间

**禁止**：
- 聚合计数（"5 处 v-html + 3 处 dangerouslySetInnerHTML"）—— 丢失具体位置，下游无法消费
- "等" / "..." / "（其余 N 处略）" 省略 finding
- 同字段多回读路径折叠为一行

### 反例义务（必须遵守）

> **why**：白盒"未发现存储型 XSS / 已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统该维度安全"。

写"未发现存储型 XSS"或"已防护"前，产物必须包含：
- 所有用户可控字段的写入点完整清单（按字段维度）
- 所有 raw 输出 sink 完整清单（按文件 file:line）
- 完整的 (write_endpoint, read_endpoint, field) 三元组交叉清单
- 每个三元组的判定结果（sanitize 完整 / 缺口 / unknown）
- `static-unknown` 单元格的具体原因（动态模板 / 闭源 sanitizer / 自定义实现）

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库（共享章节）

### FP（看似命中实际不构成）

**反例 1：`v-html` 接收已 sanitize 过的 DOMPurify 输出**

- 抽象规则：sink 命中 `v-html` 但内容已在前置 computed / pinia getter 内经 DOMPurify 净化
- 具体场景：`<div v-html="purifiedContent">` + `computed: { purifiedContent() { return DOMPurify.sanitize(this.raw) } }`
- 关键识别特征：sink 接收的不是 raw source，而是 sanitize 结果；DOMPurify 配置无 `ADD_TAGS` 扩白名单
- 排除方法：追 v-html 表达式上游、确认 DOMPurify 调用、核对配置无 `ALLOW_DATA_ATTR` / `ADD_URI_SAFE_ATTR` 扩散；归 `not_vulnerable`

**反例 2：Thymeleaf `th:utext` 配合自定义 dialect 强制 escape**

- 抽象规则：`th:utext` 看似 raw 但项目用自定义 dialect 在渲染管线插入 sanitize
- 具体场景：项目实现 `IDialect` 在 utext 前调用 OWASP Java HTML Sanitizer
- 关键识别特征：`@Configuration` 注册了自定义 dialect，dialect 实现里有 sanitizer 调用
- 排除方法：读 dialect 实现确认是否对所有 utext 渲染前置 sanitize；归 `not_vulnerable` 或 `needs_review` 视配置

**反例 3：富文本编辑器（Quill / TipTap）自带服务端 sanitize**

- 抽象规则：编辑器后端 SDK 提供"Delta → 受控 HTML"转换，已含白名单
- 具体场景：项目通过编辑器后端 API 拿到的是已转换的 HTML，不直接接收前端原始内容
- 关键识别特征：写入接口接收的是编辑器后端转换结果（Delta JSON 或 sanitized HTML）而非裸 `<div>` 内容
- 排除方法：核对编辑器 SDK 文档及实际版本白名单覆盖；归 `needs_review` 至 `not_vulnerable`

**反例 4：Markdown 渲染启用 `markdown-it` + 外挂 DOMPurify**

- 抽象规则：`markdown-it({ html: false })` 默认拒绝原始 HTML，且渲染结果再过 DOMPurify
- 具体场景：渲染管线 `md.render(src) → DOMPurify.sanitize(html)`
- 关键识别特征：grep `markdown-it` 同位置含 `html: false` 或后续 sanitize 调用
- 排除方法：确认无 `html: true` 重写、确认 DOMPurify 配置不放宽；归 `not_vulnerable`

### FN（看似不命中实际是真洞）

**反例 5：Mutation XSS（DOMPurify 默认配置可绕过）**

- 抽象规则：DOMPurify sanitize 后的 HTML 经 `innerHTML` 重新 parse 时浏览器变形，触发 mXSS
- 具体场景：`element.innerHTML = DOMPurify.sanitize(input)` + 特定 HTML 构造触发 parser 变异
- 关键识别特征：sanitize 链看似完整，但 sink 仍是 `innerHTML` / `dangerouslySetInnerHTML`
- 确认方法：用 DOMPurify CHANGELOG / 已知 mXSS payload 测试；标 `static-confirmed` 推黑盒在浏览器实际渲染验证

**反例 6：URL 上下文里 `javascript:` 协议被放过**

- 抽象规则：HTML body sanitize 通过，但 `<a href="{{var}}">` 位置 sanitizer 不校验 URL scheme
- 具体场景：bleach 默认 `ALLOWED_PROTOCOLS = ['http', 'https', 'mailto']` 但项目自定义加了 `'javascript'`
- 关键识别特征：sink 是 `href` / `src` / `formaction` / `data` 等可执行 URL 属性
- 确认方法：审 sanitizer 的 URL scheme 白名单配置；标 `static-confirmed`

**反例 7：JSON 嵌入 HTML 时未做 `<` 编码**

- 抽象规则：服务端把数据序列化为 JSON 嵌入 `<script>var data = {{json}}</script>`，缺 `<` / `</script>` 转义
- 具体场景：Go `template.JS(...)` 用错、Jinja2 `|tojson` 旧版本不转义 `</script>`
- 关键识别特征：模板里 `<script>` 内嵌 JSON 字面量
- 确认方法：测试 `</script>` 闭合 + 注入；标 `static-confirmed`

**反例 8：React `dangerouslySetInnerHTML` 渲染 user-provided SVG**

- 抽象规则：SVG 内 `<script>` / `onload` 可执行，sanitizer 若按 HTML 白名单未覆盖 SVG 子集即漏
- 具体场景：用户上传 SVG → 详情页 `<div dangerouslySetInnerHTML={{__html: svgContent}}>`
- 关键识别特征：sink 接收 SVG 内容；DOMPurify 配置缺 `USE_PROFILES: { svg: true, svgFilters: true }` 或反之过宽
- 确认方法：测试 SVG `<script>` payload；标 `static-confirmed`

### 易混淆案例

**反例 9：闭源富文本编辑器后端 sanitize 规则不可见**

- 抽象规则：项目依赖闭源富文本编辑器 SaaS 后端，白名单规则不在源码中
- 具体场景：调用 `editor-saas.transform(content)` 后写入 DB，回读时直接 v-html
- 关键识别特征：依赖图谱里的 `unknown` 标注 + 调用形态是远程 API
- 排除方法：标 `static-unknown`，不能默认 not_vulnerable

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。CSP 影响不评估（属 [csp-audit](../csp-audit/SKILL.md)）。

下面这些情形数据流分析无法继续追踪，**必须标 `static-unknown`**，不允许默认为 not_vulnerable：

1. **动态模板路径 / feature flag**：SSR 与 CSR 双路径切换、A/B test 切换富文本编辑器版本、配置驱动的模板引擎选择（不同租户不同渲染器）→ 每条分支独立审计，至少标 `static-unknown` 列出分支条件。
2. **自定义 sanitizer 实现**：项目自建 `HTMLSanitizer` 类、字符串替换式 sanitizer（`replaceAll("<", "&lt;")` 形态）→ 规则集不完整即 `static-confirmed`；不可见或复杂控制流 `static-unknown`。
3. **闭源富文本编辑器后端**：三方编辑器 SaaS 调用（Quill Cloud / TinyMCE Cloud）/ 闭源 jar / npm package 内部 sanitize → 依赖图谱标 `unknown`，推 [dependency-decompile](../dependency-decompile/SKILL.md)；不能默认 not_vulnerable。
4. **跨服务 RPC 拿到的字段**：微服务架构下字段来自上游 RPC / Kafka 消费 → 本服务追到 RPC 入站即停，标 `static-unknown` 由上游独立审。
5. **SSR 与 CSR 双路径**：Vue / React SSR + hydration 客户端可能用 `innerHTML` 重渲 → 两端独立看 sink。
6. **运行时配置切换 sanitizer 行为**：DOMPurify `ADD_TAGS` 被环境变量扩展 → 读配置文件 / 环境变量定义；不可见标 `static-unknown`。
7. **反射 / 动态属性访问**：JS `el[attrName] = value` 中 `attrName` 动态决定 → 标 `static-unknown` 记录反射点。

**CSP 影响**：CSP 是浏览器侧的 mitigation，**本能力不评估**——CSP 强弱属 [csp-audit](../csp-audit/SKILL.md) 职责。白盒 `static-confirmed` 不因 CSP 强而降级，反之亦然。

**底线**：本能力写"该子系统无存储型 XSS"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

---

## 12. 修复建议（共享章节）

### 源头治理（首选）

- **入库前 sanitize**：用业界成熟白名单库
  - Java：OWASP Java HTML Sanitizer (`Sanitizers.FORMATTING.and(Sanitizers.LINKS).and(Sanitizers.BLOCKS)` 等组合)
  - JavaScript（服务端）：`isomorphic-dompurify` / `sanitize-html`
  - Python：`bleach.clean(input, tags=ALLOWED, attributes=..., protocols=ALLOWED_PROTOCOLS)`
  - Go：`bluemonday.UGCPolicy()` 或自建严格 policy
- **模板输出 auto-escape 默认开 + 显式 opt-out 走 sanitize**：禁止全局关闭 auto-escape；显式 raw 输出位置必须经 sanitize 后再传入
- **富文本走严格白名单**：标签 + 属性 + URL 协议三层都要覆盖；URL 协议白名单仅 `http`/`https`/`mailto`，禁 `javascript:` / `data:`
- **Markdown 渲染启用 sanitize**：
  - `marked` v4+：渲染结果外挂 DOMPurify
  - `markdown-it`：保持 `html: false`，自定义 token 替代原始 HTML 需求
- **JSON 嵌入 HTML 用 unicode escape**：`<` → `<`、`/` → `\/`，避免 `</script>` 闭合

### 边界过滤（次选，深度防御）

- **输出端二次 encoder 匹配上下文**：模板默认 auto-escape 仅适用 HTML body；attr 无引号 / JS 嵌入 / URL 协议位置独立处理
- **CSP 兜底**：在 [csp-audit](../csp-audit/SKILL.md) 指导下配置 `script-src` / `style-src` / `object-src` 收敛执行面；CSP 不替代 sanitize
- **HttpOnly Cookie**：限制 JS 访问会话标识，降低 XSS 利用价值（不阻断 XSS 本身）

### 兜底拒绝

- 富文本字段大小上限 + 内容类型校验
- 高危标签 / 属性 / 协议在 WAF 层补一层拒绝（仅作辅助）
- 错误响应不暴露内部模板路径 / sanitizer 配置

### 参考

- [OWASP XSS Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html)
- [OWASP DOM-based XSS Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/DOM_based_XSS_Prevention_Cheat_Sheet.html)
