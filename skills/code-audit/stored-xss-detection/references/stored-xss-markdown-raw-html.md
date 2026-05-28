# Markdown 原生 HTML 注入（Markdown Raw HTML Injection）

## 攻击链概述

用户提交含原始 HTML 标签的 Markdown 内容 → 数据库持久化 → Markdown→HTML 渲染引擎将原始 HTML 保留在输出中 → 页面展示渲染结果 → 浏览器执行注入的脚本或事件处理器。

**核心问题**：大多数 Markdown 库默认允许在 Markdown 中嵌入原始 HTML（这是 CommonMark 规范的一部分）。开发者常假设"Markdown 是安全的文本格式"，但实际上 Markdown 渲染输出就是 HTML，且默认包含用户注入的原始 HTML 标签。

## 通用代码示例

### Java — commonmark-java

```java
// ❌ 默认配置：允许原始 HTML 通过
import org.commonmark.parser.Parser;
import org.commonmark.renderer.html.HtmlRenderer;

Parser parser = Parser.builder().build();
HtmlRenderer renderer = HtmlRenderer.builder().build();
String html = renderer.render(parser.parse(userMarkdown));
// 输入: "# Title\n<img src=x onerror=alert(1)>"
// 输出: "<h1>Title</h1>\n<img src=x onerror=alert(1)>"
```

```java
// ✅ 禁用原始 HTML
HtmlRenderer renderer = HtmlRenderer.builder()
    .escapeHtml(true) // 将 HTML 标签转义为实体
    .build();
// 输出: "<h1>Title</h1>\n&lt;img src=x onerror=alert(1)&gt;"
```

```java
// ✅ 或使用 OWASP Sanitizer 清洗渲染后的 HTML
PolicyFactory policy = new HtmlPolicyBuilder()
    .allowElements("h1", "h2", "h3", "p", "a", "code", "pre", "ul", "ol", "li", "em", "strong", "blockquote")
    .allowAttributes("href").onElements("a")
    .requireRelNofollowOnLinks()
    .toFactory();
String safeHtml = policy.sanitize(renderer.render(parser.parse(userMarkdown)));
```

### Python — markdown / mistune

```python
# ❌ Python markdown 库默认允许原始 HTML
import markdown
html = markdown.markdown(user_input)
# 输入: "Normal text\n<script>alert(1)</script>"
# 输出: "<p>Normal text</p>\n<script>alert(1)</script>"
```

```python
# ✅ 方法 1：使用 bleach 在渲染后清洗
import bleach
import markdown

html = markdown.markdown(user_input)
safe_html = bleach.clean(html,
    tags=['h1','h2','h3','h4','p','a','em','strong','code','pre',
          'ul','ol','li','blockquote','hr','br','table','thead',
          'tbody','tr','th','td'],
    attributes={'a': ['href', 'title'], 'td': ['align'], 'th': ['align']})
```

```python
# ✅ 方法 2：mistune 3.x 默认转义 HTML
import mistune
md = mistune.create_markdown(escape=True)
safe_html = md(user_input)
```

### Go — goldmark / blackfriday

```go
// ❌ goldmark 默认安全（WithRendererOptions 可能破坏）
// 但 blackfriday v2 默认允许原始 HTML
import "github.com/russross/blackfriday/v2"

html := blackfriday.Run([]byte(userInput))
// 输入: "test\n<img src=x onerror=alert(1)>"
// 输出包含: <img src=x onerror=alert(1)>
```

```go
// ✅ goldmark 默认行为是安全的（转义原始 HTML）
import "github.com/yuin/goldmark"

var buf bytes.Buffer
md := goldmark.New() // 默认 WithRendererOptions(html.WithXHTML()) 不含 WithUnsafe()
md.Convert([]byte(userInput), &buf)
// 原始 HTML 被转义

// ❌ 但如果加了 WithUnsafe()，就不安全了
md := goldmark.New(
    goldmark.WithRendererOptions(html.WithUnsafe()), // 允许原始 HTML
)
```

```go
// ✅ 使用 bluemonday 清洗
import "github.com/microcosm-cc/bluemonday"

p := bluemonday.UGCPolicy()
safeHTML := p.Sanitize(string(html))
```

### JavaScript / Node.js — marked / markdown-it

```javascript
// ❌ marked 默认允许原始 HTML（v4 之前）
const marked = require('marked');
const html = marked.parse(userInput);
// 输入: "test\n<img src=x onerror=alert(1)>"
// 输出包含: <img src=x onerror=alert(1)>
```

```javascript
// ✅ marked v4+：启用 sanitize 或使用 DOMPurify
const { marked } = require('marked');
const DOMPurify = require('dompurify');
const html = DOMPurify.sanitize(marked.parse(userInput));
```

```javascript
// ✅ markdown-it：默认转义 HTML
const md = require('markdown-it')();
const html = md.render(userInput);
// 原始 HTML 被转义

// ❌ 但如果启用了 html 选项
const md = require('markdown-it')({ html: true }); // 允许原始 HTML
```

## Markdown 库安全默认值对照

| 库 | 语言 | 默认是否允许原始 HTML | 禁用方式 |
|----|------|-------------------|---------|
| commonmark-java | Java | **是** | `HtmlRenderer.builder().escapeHtml(true)` |
| flexmark-java | Java | **是** | `HtmlRenderer.builder().escapeHtml(true)` 或 `Parser.builder().extensions(禁用 HTML 扩展)` |
| Python markdown | Python | **是** | 无内建选项，需 bleach 后处理 |
| mistune 3.x | Python | 否（默认转义） | — |
| blackfriday v2 | Go | **是** | 后处理 bluemonday |
| goldmark | Go | 否（默认转义） | 不要加 `WithUnsafe()` |
| marked (< v4) | JS | **是** | DOMPurify 后处理 |
| markdown-it | JS | 否（默认转义） | 不要设 `html: true` |

## 识别信号

| 信号 | 说明 |
|------|------|
| 项目引入 Markdown 渲染库但无 sanitizer 依赖 | 渲染后未清洗 |
| Markdown 渲染配置含 `html: true` / `WithUnsafe()` / 无 `escapeHtml(true)` | 显式或隐式允许原始 HTML |
| 渲染结果通过 `\| safe` / `mark_safe()` / `v-html` / `dangerouslySetInnerHTML` 输出 | Markdown HTML 输出再次进入无转义 sink |
| CMS / 知识库 / Wiki 功能使用 Markdown | 多用户可编辑内容，攻击者可注入 HTML |

## 审计方法

1. **识别 Markdown 渲染库**：搜索 `markdown` / `commonmark` / `blackfriday` / `goldmark` / `marked` / `markdown-it` / `mistune` 依赖
2. **检查渲染配置**：是否启用了 `escapeHtml` / 是否禁用了 `html` 选项 / 是否加了 `WithUnsafe()`
3. **检查后处理**：渲染后是否经过 sanitizer（DOMPurify / bleach / bluemonday / OWASP Sanitizer）
4. **追踪渲染输出**：渲染后的 HTML 是否通过安全方式输出（自动转义模板）还是通过危险 sink 输出（`v-html` / `|safe`）
5. **交叉验证**：不同展示场景（前台 / 管理后台 / 移动端 / 邮件通知）是否都经过了同一条净化链
