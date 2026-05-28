---
name: 存储型XSS-专项链路检测
description: 检测存储型 XSS 风险；当用户输入会先进入数据库、缓存、对象存储或文件系统，再在页面、后台预览、富文本、Markdown、邮件模板等场景中二次展示时触发；适用于代码审计、SAST 结果复核与上传/内容渲染链路分析。
tags: xss,stored-xss,persistent-xss,code-audit,dataflow,render
trigger_keywords: 存储型XSS,stored xss,persistent xss,富文本XSS,评论XSS,markdown预览,svg上传,html预览,二次展示XSS
when-to-use: 当需要专门分析“用户输入 -> 持久化 -> 二次展示”链路，或者普通 XSS sink 规则不足以确认高危 XSS 时
allowed-tools: read_file,list_files,rg,bash,list_skills,load_skills
user-invocable: true
argument-hint: "<target_path> [target_url]"
arguments:
  - target_path
  - target_url
---

# 存储型 XSS 专项链路检测

## 目标

把“普通 XSS sink 命中”进一步收敛为**存储型 XSS 的完整证据链**，重点回答五个问题：

1. 用户输入从哪里进入系统？
2. 输入是否被持久化到数据库、缓存、对象存储或文件系统？
3. 后续是通过哪条读取路径再次取出？
4. 最终是否进入真实的 HTML / 富文本 / 预览 sink？
5. 中间是否缺少有效净化，且能够形成真实危害？

这个 skill 只聚焦**存储型 / 持久型 XSS**，不把所有反射型、DOM 型、纯前端 sink 都混在一起。

## 为什么适合单独做成 skill

- 单条规则擅长抓直接 sink，例如 `innerHTML`、`mark_safe()`、`dangerouslySetInnerHTML`、`v-html`。
- 存储型 XSS 更难的部分不在 sink 本身，而在于把**输入、持久化、读取、渲染、净化缺口**串成同一条链。
- 因此这里优先采用“规则初筛 + 数据流确认 + 场景化核查”的编排方式，而不是试图靠一条通用规则覆盖所有框架和业务流转。

## 执行策略

- 优先复用现有 skill：
  - `sast-scan`：找已知 XSS sink、富文本绕过、模板绕过、上传后渲染点
  - `dataflow-analysis`：追踪 source / persistence / render sink 是否真连通
  - `file-upload`：当链路涉及 SVG、HTML、Markdown、Office、导入预览时补做上传侧分析
  - `agent-browser`：当目标可运行时，用浏览器验证实际触发效果和展示上下文
- 若运行时支持 `load_skill`，按上述顺序按需加载，不要一开始把所有 skill 全量展开。

## 参考案例

执行本 skill 前，应先阅读 `references/` 下的案例文件以建立攻击链模式认知：

- [stored-xss-comment-richtext.md](references/stored-xss-comment-richtext.md) — 评论/富文本经典存储型 XSS（source→DB→detail page→innerHTML/v-html/th:utext）
- [stored-xss-file-preview.md](references/stored-xss-file-preview.md) — 文件上传后预览执行（SVG/HTML/Markdown 上传→存储→在线预览→浏览器执行）
- [stored-xss-markdown-raw-html.md](references/stored-xss-markdown-raw-html.md) — Markdown 原生 HTML 注入（Markdown 内容含 `<script>` → 渲染器保留原始 HTML → 页面执行）
- [stored-xss-sanitizer-position-error.md](references/stored-xss-sanitizer-position-error.md) — Sanitizer 位置/时机错误（清洗在入库侧而非输出侧、部分渠道绕过清洗链）

## 核心判定链路

最小闭环必须尽量逼近下面这条链：

`用户输入 -> 持久化点 -> 二次读取 -> 渲染 sink -> 缺少有效净化 -> 实际触发效果`

若缺少任意关键一环，结论应降级：

| 缺失环节 | 结论 |
|---------|------|
| 只有 source + sink，没有持久化 | 更像反射型/DOM 型，不能直接算存储型 |
| 有 source + 持久化，但未定位读取与展示路径 | `suspected` |
| 有完整链路，但净化是否有效不明确 | `suspected` |
| 完整链路成立，且净化缺失或可绕过 | `confirmed` |

## 第一阶段：找高价值候选入口

优先从以下业务语义入手，而不是全仓盲搜：

- 评论、帖子、工单、客服消息、聊天消息
- 富文本、简介、个人资料、商品描述、活动文案
- Markdown、CMS 内容、公告、帮助中心、邮件模板
- 文件上传后的预览内容：SVG、HTML、Markdown、导入文件、模板文件
- 后台审核、运营预览、客服查看、管理端详情页

参见 [stored-xss-comment-richtext.md](references/stored-xss-comment-richtext.md) 和 [stored-xss-file-preview.md](references/stored-xss-file-preview.md) 中的高价值入口示例。

建议先用 `rg` 缩小候选面：

```bash
rg -n "comment|content|html|richtext|rich_text|markdown|bio|profile|description|template|preview|svg|upload" <target_path>
```

```bash
rg -n "Create\\(|Save\\(|Update\\(|Insert\\(|Exec\\(|PutObject|Upload|WriteFile|writeFile|INSERT INTO|UPDATE .* SET" <target_path>
```

## 第二阶段：确认输入与持久化点

重点找“用户可控输入”是否真的被落地：

### 常见输入入口

- HTTP 参数：query、form、JSON body、multipart 字段
- GraphQL、WebSocket、RPC 入参
- 文件上传后的文件内容、文件名、元数据
- 批量导入：CSV、Excel、Markdown、HTML、模板包
- 第三方回调或消息队列中被后续展示的字段

### 常见持久化点

- ORM：`Create`、`Save`、`Update`、`Updates`、`insert`、`upsert`
- 原生 SQL：`INSERT`、`UPDATE`
- NoSQL / KV：MongoDB、Redis hash、文档库
- 对象存储：S3、OSS、MinIO
- 文件系统：本地文件、缓存文件、模板文件、导出中间文件

如果输入只在内存中短暂流转、没有进入持久化层，通常不应归为存储型 XSS。

对文件上传类持久化，参见 [stored-xss-file-preview.md](references/stored-xss-file-preview.md)。

## 第三阶段：定位二次读取与展示路径

这一阶段是存储型 XSS 与普通 sink 检测的分水岭。要回答“谁在什么场景把它拿出来展示”。

### 高风险读取场景

- 列表页、详情页、搜索结果页
- 管理后台、审核页、客服工作台、运营预览页
- Markdown 预览、富文本回显、模板预览
- HTML 邮件、站内信、通知模板渲染
- 上传文件在线预览、内容抽取结果页、导入结果回显

### 常见危险 sink

- 前端：`innerHTML`、`outerHTML`、`insertAdjacentHTML`
- React / Vue：`dangerouslySetInnerHTML`、`v-html`
- 模板和服务端：`mark_safe()`、`safe` 过滤器、`template.HTML`、原样输出 HTML 的 helper
- Markdown / 富文本：允许原始 HTML、关闭 sanitize、错误白名单
- 响应直写：`response.write()`、Servlet/JSP 原样输出、自定义 HTML 拼接
- 文件预览：把上传的 SVG、HTML、Markdown 转成可执行 DOM 片段

建议优先用现有 `sast-scan` 找 sink，再围绕命中点往上追读取来源。参见 [stored-xss-comment-richtext.md](references/stored-xss-comment-richtext.md) 中的 Sink 清单。

## 第四阶段：检查净化与编码缺口

不要只看“有没有调 sanitizer”，而要看它是否在**正确的位置**、对**正确的内容类型**生效。

### 常见有效净化线索

- DOMPurify / sanitize-html
- Python `bleach`
- Go `bluemonday`
- Java OWASP Java HTML Sanitizer
- 明确的 allowlist 规则，且渲染侧不会再把已净化内容重新拼接进 HTML

### 常见失效模式

- 只在入库时净化，但后续又做 HTML 拼接或模板二次包装
- 只做字符串替换，没有基于 DOM/HTML 语义净化
- Markdown 渲染开启原始 HTML，或 SVG 预览允许脚本/事件属性
- 管理端、预览端、导出端绕过了主站的净化链

若无法证明净化在最终渲染侧仍然有效，不能因为”看起来调用过 sanitizer”就直接排除风险。参见 [stored-xss-sanitizer-position-error.md](references/stored-xss-sanitizer-position-error.md) 中的常见失效模式。

## 第五阶段：场景化确认

### 场景一：评论 / 富文本 / 简介入库后展示

检查：

- 请求参数是否写入评论、简介、富文本字段
- 详情页、个人页、后台审核页是否回显该字段
- 回显是否经过 HTML 渲染而非纯文本编码

### 场景二：Markdown / CMS / 模板内容

参见 [stored-xss-markdown-raw-html.md](references/stored-xss-markdown-raw-html.md) 中的 Markdown 库安全默认值对照表。

检查：

- Markdown 是否允许原始 HTML
- CMS 内容是否在多端复用，某一端关闭了 sanitize
- 模板预览是否把用户内容塞进 HTML 模板变量

### 场景三：上传文件内容二次渲染

检查：

- SVG、HTML、Markdown、导入文件是否会被在线预览
- 文件名、title、meta 是否被拼进页面 DOM
- 文件内容解析后是否进入富文本或管理端详情页

此类场景优先联动 `file-upload` skill。

### 场景四：后台 / 管理端 / 审核页

检查：

- 前台内容是否会被后台原样查看
- 管理端是否为了“方便运营”关闭了转义或启用了富文本直出
- 是否存在“前台安全，后台不安全”的差异渲染链

## 数据流确认要点

当 `sast-scan` 只给出 sink 或弱线索时，使用 `dataflow-analysis` 重点确认：

1. sink 参数往上追，是否能追到 `request/body/form/upload` 等用户输入
2. source 往下追，是否进入 `Create/Save/Insert/WriteFile/PutObject`
3. 从持久化对象继续追，是否在另一个 handler / service / template / component 中再次读取并渲染

优先确认“跨函数、跨文件、跨请求周期”的链路，这正是普通 XSS 规则最容易漏掉的部分。

## 闭环验证要求

- 仅发现 `innerHTML`、`mark_safe()`、`v-html` 之类 sink，不足以判定存储型 XSS。
- 仅发现“用户输入被入库”，但没有展示点，也不足以判定存储型 XSS。
- `confirmed` 必须尽量具备以下证据中的大部分：
  - 可控输入字段
  - 明确持久化点
  - 明确读取路径
  - 明确渲染 sink
  - 缺少有效净化或净化可绕过
  - 能说明真实危害：前台执行、后台执行、运营端执行、邮件客户端执行、预览执行
- 若目标可运行，优先使用浏览器或最小 PoC 复核实际触发效果；若只能静态审计，也必须把链路说明完整，不能只给单点命中。

## 结论模板

每条发现至少输出：

- 入口：`<接口/参数/上传字段>`
- 持久化：`<ORM/SQL/对象存储/文件写入位置>`
- 二次读取：`<列表页/详情页/后台/预览/模板>`
- 渲染 sink：`<innerHTML / mark_safe / v-html / Markdown raw HTML ...>`
- 净化缺口：`<缺失/位置错误/可绕过>`
- 危害：`<前台用户执行 / 管理员执行 / 运营端执行 / 邮件端执行>`
- 结论：`confirmed / suspected / not vulnerable`

## 何时不该用这个 skill

- 只是在找普通反射型 XSS、DOM XSS，且已经有明显直接 sink
- 目标没有持久化链路，只是一次性回显
- 用户只需要快速跑一遍通用 XSS 规则，而不是确认存储型风险
