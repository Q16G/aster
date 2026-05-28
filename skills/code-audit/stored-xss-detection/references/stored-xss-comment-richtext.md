# 评论/富文本存储型 XSS（Comment & Rich Text Stored XSS）

## 攻击链概述

用户提交的评论、简介、富文本内容 → 数据库持久化 → 详情页/管理后台/审核页读取 → 无转义渲染（`innerHTML` / `v-html` / `th:utext` / `dangerouslySetInnerHTML`）→ 前台用户或管理员浏览器执行恶意脚本。

**这是存储型 XSS 最经典、最高频的形态。** 关键不在于 sink 本身，而在于输入经过持久化后跨请求周期在另一个页面渲染。

## 通用代码示例

### Java Spring + Thymeleaf

#### 写入（source → persist）

```java
// ❌ 评论内容原样入库
@PostMapping("/comment/add")
public Result addComment(@RequestBody CommentDTO dto, HttpSession session) {
    Comment comment = new Comment();
    comment.setUserId((Long) session.getAttribute("userId"));
    comment.setContent(dto.getContent()); // 用户可控，无清洗
    commentService.save(comment);
    return Result.ok();
}
```

#### 读取 + 渲染（persist → read → sink）

```html
<!-- ❌ th:utext 不做 HTML 转义，直接输出原始 HTML -->
<div class="comment-body" th:utext="${comment.content}"></div>
```

```html
<!-- ✅ th:text 自动转义 -->
<div class="comment-body" th:text="${comment.content}"></div>
```

#### 安全写法

```java
// ✅ 入库前使用 OWASP Sanitizer 清洗
PolicyFactory policy = new HtmlPolicyBuilder()
    .allowElements("b", "i", "em", "strong", "a", "p", "br")
    .allowAttributes("href").onElements("a")
    .requireRelNofollowOnLinks()
    .toFactory();
comment.setContent(policy.sanitize(dto.getContent()));
commentService.save(comment);
```

### Vue + Spring Boot API

#### 前端渲染（sink）

```vue
<!-- ❌ v-html 直接渲染后端返回的 HTML -->
<template>
  <div class="post-content" v-html="post.content"></div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
const post = ref({})
onMounted(async () => {
  const res = await fetch(`/api/posts/${postId}`)
  post.value = await res.json()
})
</script>
```

```vue
<!-- ✅ 使用 DOMPurify 清洗后再渲染 -->
<template>
  <div class="post-content" v-html="sanitizedContent"></div>
</template>

<script setup>
import DOMPurify from 'dompurify'
import { ref, computed, onMounted } from 'vue'
const post = ref({})
const sanitizedContent = computed(() => DOMPurify.sanitize(post.value.content || ''))
onMounted(async () => {
  const res = await fetch(`/api/posts/${postId}`)
  post.value = await res.json()
})
</script>
```

### React

```jsx
// ❌ dangerouslySetInnerHTML 直接渲染
function CommentItem({ comment }) {
  return <div dangerouslySetInnerHTML={{ __html: comment.content }} />;
}

// ✅ 使用 DOMPurify
import DOMPurify from 'dompurify';
function CommentItem({ comment }) {
  return <div dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(comment.content) }} />;
}
```

### PHP

```php
// ❌ 写入：原样存储
$content = $_POST['content']; // 用户可控
$stmt = $pdo->prepare("INSERT INTO comments (user_id, content) VALUES (?, ?)");
$stmt->execute([$_SESSION['user_id'], $content]);

// ❌ 读取+渲染：echo 直出
$row = $pdo->query("SELECT content FROM comments WHERE id = $id")->fetch();
echo "<div class='comment'>" . $row['content'] . "</div>";

// ✅ 渲染时转义
echo "<div class='comment'>" . htmlspecialchars($row['content'], ENT_QUOTES, 'UTF-8') . "</div>";
```

### Python Flask + Jinja2

```python
# ❌ 写入
@app.route('/posts', methods=['POST'])
@login_required
def create_post():
    content = request.form['content']
    post = Post(author_id=current_user.id, content=content)
    db.session.add(post)
    db.session.commit()
    return redirect(url_for('view_post', id=post.id))
```

```html
{# ❌ |safe 过滤器关闭 Jinja2 自动转义 #}
<div class="post-body">{{ post.content | safe }}</div>

{# ✅ 默认自动转义（不加 |safe） #}
<div class="post-body">{{ post.content }}</div>
```

## Sink 清单（本模式适用的）

| Sink | 框架/语言 | 说明 |
|------|----------|------|
| `th:utext` | Thymeleaf (Java) | 无转义输出，`th:text` 才转义 |
| `v-html` | Vue | 直接设置 innerHTML |
| `dangerouslySetInnerHTML` | React | 名称本身就是警告 |
| `innerHTML` / `outerHTML` | 原生 JS | DOM 直接赋值 |
| `\| safe` / `mark_safe()` | Jinja2 / Django | 关闭自动转义 |
| `<%- ... %>` | EJS | 无转义输出，`<%= %>` 才转义 |
| `{{{ ... }}}` | Handlebars / Mustache | 三花括号无转义 |
| `echo $var` | PHP | 无 `htmlspecialchars` 包裹 |
| `<jsp:out escapeXml="false">` | JSP | 显式关闭转义 |

## 审计方法

1. **搜索危险 sink**：用 `rg` 搜索 `th:utext|v-html|dangerouslySetInnerHTML|innerHTML|mark_safe|\|safe|\{\{\{|escapeXml.*false`
2. **反查 sink 数据来源**：从 sink 变量名追踪到 service/DAO 读取方法，确认读取的是持久化数据
3. **追踪到写入入口**：确认该字段的写入接口是否接受用户可控输入
4. **检查净化链**：在 source→persist→read→sink 全链路中，是否有任何一处做了有效净化（位置正确 + 语义正确）
5. **确认危害场景**：该内容在哪些页面展示？前台用户看到？管理员看到？邮件通知中？
