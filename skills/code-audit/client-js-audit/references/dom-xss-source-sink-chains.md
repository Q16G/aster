# DOM XSS 典型 Source→Sink 链（DOM XSS Source-Sink Chains）

## 漏洞模式

攻击者通过客户端可控的 DOM 属性（source）注入恶意内容，该内容未经净化直接传入 DOM 操作 API（sink），在受害者浏览器中执行。与服务端 XSS 不同，DOM XSS 的 payload 不经过服务端——整个攻击链在浏览器内完成。

**核心原则：任何从 URL（hash / query / path）、`document.referrer`、`window.name`、`postMessage` 等客户端可控来源读取的数据，如果未经净化就进入 DOM 写入 API，都构成 DOM XSS。**

## Source 清单

| Source | API | 说明 |
|--------|-----|------|
| URL fragment | `location.hash` | `#` 后的内容，不发送到服务端 |
| URL query | `location.search` / `URLSearchParams` | `?` 后的参数 |
| URL 全文 | `location.href` / `document.URL` / `document.documentURI` | 完整 URL |
| Referrer | `document.referrer` | 引荐页 URL，可被攻击者构造 |
| Window name | `window.name` | 跨页面持久化，可被前一页设置 |
| postMessage | `event.data` | 跨窗口/iframe 通信 |
| Web Storage | `localStorage.getItem()` / `sessionStorage.getItem()` | 可能被其他页面污染 |
| Cookie | `document.cookie` | 可被子域设置 |

## Sink 清单

| Sink | API | 危险度 |
|------|-----|--------|
| HTML 写入 | `innerHTML` / `outerHTML` / `insertAdjacentHTML` / `document.write` | 高 |
| jQuery HTML | `.html()` / `.append()` / `.prepend()` / `.after()` / `.before()` / `.replaceWith()` | 高 |
| 代码执行 | `eval()` / `setTimeout(str)` / `setInterval(str)` / `Function(str)` | 极高 |
| 导航 | `location.href =` / `location.assign()` / `location.replace()` | 中（配合 `javascript:` 协议） |
| 框架特定 | Vue `v-html` / React `dangerouslySetInnerHTML` / Angular `bypassSecurityTrustHtml` | 高 |
| 模板引擎 | Handlebars `{{{triple}}}` / EJS `<%- %>` / Pug `!=` | 高 |

## 攻击代码示例

### 经典链：location.hash → innerHTML

```javascript
// ❌ 从 URL hash 读取内容并直接写入 DOM
const tab = location.hash.substring(1); // source
document.getElementById('content').innerHTML = '<h2>' + tab + '</h2>'; // sink
// 攻击: http://target.com/page#<img src=x onerror=alert(document.cookie)>
```

```javascript
// ✅ 使用 textContent（纯文本，不解析 HTML）
const tab = location.hash.substring(1);
document.getElementById('content').textContent = tab;
```

### jQuery 链：URL 参数 → $.html()

```javascript
// ❌ 从 URL 参数读取并写入 jQuery DOM
const params = new URLSearchParams(location.search);
const name = params.get('name'); // source
$('#greeting').html('Welcome, ' + name + '!'); // sink
// 攻击: http://target.com/page?name=<img src=x onerror=alert(1)>
```

```javascript
// ✅ 使用 $.text()
$('#greeting').text('Welcome, ' + name + '!');
```

### document.referrer → innerHTML

```javascript
// ❌ 在 404 页面显示来源
const ref = document.referrer; // source — 攻击者可构造引荐页
document.getElementById('back-link').innerHTML = 
    'Return to <a href="' + ref + '">' + ref + '</a>'; // sink
// 攻击者页面: <a href="http://target.com/nonexistent">click</a>
// 其中攻击者页面 URL 含有 XSS payload
```

```javascript
// ✅ 使用 DOM API 构建元素
const a = document.createElement('a');
a.href = document.referrer;
a.textContent = document.referrer;
document.getElementById('back-link').textContent = 'Return to ';
document.getElementById('back-link').appendChild(a);
```

### eval 链：URL 参数 → eval

```javascript
// ❌ JSON 回调处理
const callback = new URLSearchParams(location.search).get('callback');
eval(callback + '(' + JSON.stringify(data) + ')'); // source → eval sink
// 攻击: http://target.com/api?callback=alert(1)//
```

```javascript
// ✅ 使用白名单校验 callback 名
const callback = new URLSearchParams(location.search).get('callback');
if (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(callback) && window[callback]) {
    window[callback](data);
}
```

### Vue v-html 链

```vue
<!-- ❌ URL 参数直接进入 v-html -->
<template>
  <div v-html="announcement"></div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
const announcement = ref('')
onMounted(() => {
  const params = new URLSearchParams(location.search)
  announcement.value = params.get('msg') || 'Welcome'
})
</script>
```

```vue
<!-- ✅ 使用 v-text 或 DOMPurify -->
<template>
  <div v-text="announcement"></div>
</template>
```

### React dangerouslySetInnerHTML 链

```jsx
// ❌ URL hash 进入 dangerouslySetInnerHTML
function PreviewPanel() {
  const content = decodeURIComponent(window.location.hash.slice(1));
  return <div dangerouslySetInnerHTML={{ __html: content }} />;
}

// ✅ 使用 DOMPurify
import DOMPurify from 'dompurify';
function PreviewPanel() {
  const content = decodeURIComponent(window.location.hash.slice(1));
  return <div dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(content) }} />;
}
```

## 识别信号

| 信号 | 说明 |
|------|------|
| `location.hash` / `location.search` / `location.href` 赋值给变量后进入 DOM 操作 | 经典 DOM XSS 链 |
| `document.referrer` / `window.name` 被使用 | 非直觉的 source，容易被忽略 |
| `innerHTML` / `.html()` / `document.write` 拼接了变量 | 危险 sink，需追查变量来源 |
| URL 路由框架将 URL 片段渲染到页面 | SPA 应用常见模式 |
| `eval` / `setTimeout(string)` / `new Function(string)` | 代码执行 sink |

## 审计方法

1. **搜索 sink**：`rg "innerHTML|outerHTML|document\.write|insertAdjacentHTML|\.html\(|eval\(|setTimeout\(|setInterval\(|new Function\(|v-html|dangerouslySetInnerHTML|bypassSecurityTrust"` 在 JS/TS/Vue/JSX 文件中
2. **追踪 sink 的数据来源**：从 sink 变量反向追踪，确认是否来自客户端可控 source
3. **检查净化**：source 到 sink 之间是否有 DOMPurify / textContent / encodeURIComponent 等安全操作
4. **检查框架保护**：React 默认转义 JSX 表达式（安全），但 `dangerouslySetInnerHTML` 绕过了保护；Vue `{{ }}` 自动转义（安全），但 `v-html` 绕过了保护
