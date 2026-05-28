# Sanitizer 位置/时机错误（Sanitizer Position Error）

## 攻击链概述

系统确实调用了 sanitizer，但清洗发生在**错误的位置或时机**——在输入侧清洗后，读取侧又做了 HTML 拼接、模板渲染或二次加工，导致清洗后的安全内容重新变得危险。或者，多个展示渠道中只有部分经过了清洗链，其他渠道绕过了清洗。

**核心原则**：净化必须在**最终渲染点之前**生效，而不是在**输入入库时**。入库时清洗只能作为纵深防御，不能替代输出时的转义/净化。

## 漏洞模式

### 模式 1：入库时清洗 + 输出时拼接

```
用户输入 → sanitizer 清洗 → 数据库存储（干净内容）
                                    ↓
                              读取干净内容 → 拼接到 HTML 模板 → 拼接引入新的注入点
```

### 模式 2：主渠道清洗 + 副渠道未清洗

```
用户输入 → 数据库存储
              ↓                    ↓
         前台详情页（有 sanitizer）    管理后台/邮件/导出（无 sanitizer）
              ✅ 安全                  ❌ 有漏洞
```

### 模式 3：清洗后再编码/解码

```
用户输入 → sanitizer 清洗 → URL encode / Base64 encode → 数据库存储
                                                            ↓
                                        读取 → decode → 直接渲染（decode 后恢复了恶意内容）
```

## 通用代码示例

### Java — 入库清洗 + 模板拼接绕过

```java
// ❌ 入库时清洗了 content，但渲染时拼接了未清洗的 title
@PostMapping("/article/save")
public Result saveArticle(@RequestBody ArticleDTO dto) {
    PolicyFactory policy = new HtmlPolicyBuilder()
        .allowElements("b", "i", "p", "br")
        .toFactory();
    Article article = new Article();
    article.setTitle(dto.getTitle());  // title 未清洗！
    article.setContent(policy.sanitize(dto.getContent())); // content 已清洗
    articleService.save(article);
    return Result.ok();
}
```

```html
<!-- ❌ title 通过 th:utext 无转义输出 -->
<h1 th:utext="${article.title}"></h1>
<!-- content 已经干净了，但 title 是用户原始输入 -->
<div th:utext="${article.content}"></div>
```

```java
// ✅ 所有用户可控字段都要清洗，或渲染时统一用 th:text
article.setTitle(Jsoup.clean(dto.getTitle(), Safelist.none())); // title 也清洗
```

### PHP — 存储时过滤 + 读取后拼接

```php
// ❌ 入库时用 strip_tags 清洗
$comment = strip_tags($_POST['comment'], '<b><i><a>');
$pdo->prepare("INSERT INTO comments (content) VALUES (?)")->execute([$comment]);

// ❌ 读取时做了 HTML 拼接，引入了注入点
$row = $pdo->query("SELECT content, username FROM comments WHERE id = $id")->fetch();
// username 未经清洗，直接拼入 HTML
echo "<div class='comment'><span class='author'>" . $row['username'] . "</span>: " . $row['content'] . "</div>";
```

```php
// ✅ 输出时统一转义所有用户来源字段
echo "<div class='comment'><span class='author'>"
    . htmlspecialchars($row['username'], ENT_QUOTES, 'UTF-8')
    . "</span>: "
    . htmlspecialchars($row['content'], ENT_QUOTES, 'UTF-8')
    . "</div>";
```

### Python — 主渠道安全 + 邮件渠道绕过

```python
# 前台展示（✅ 有 sanitizer）
@app.route('/ticket/<int:tid>')
def view_ticket(tid):
    ticket = Ticket.query.get_or_404(tid)
    safe_content = bleach.clean(ticket.content, tags=['b','i','p','br','a'])
    return render_template('ticket.html', ticket=ticket, content=safe_content)

# ❌ 邮件通知（无 sanitizer，直接拼接 HTML 邮件）
def send_ticket_notification(ticket):
    html_body = f"""
    <html><body>
    <h2>新工单通知</h2>
    <p>内容：{ticket.content}</p>
    </body></html>
    """
    # ticket.content 是原始用户输入，未经清洗
    send_email(to=admin_email, subject="新工单", html=html_body)
```

```python
# ✅ 邮件渠道也要清洗
def send_ticket_notification(ticket):
    safe_content = bleach.clean(ticket.content, tags=[], strip=True)
    html_body = f"""
    <html><body>
    <h2>新工单通知</h2>
    <p>内容：{safe_content}</p>
    </body></html>
    """
    send_email(to=admin_email, subject="新工单", html=html_body)
```

### Go — 清洗后又做字符串替换

```go
// ❌ 先清洗，然后用模板替换引入新的 HTML
func renderComment(content, username string) string {
    p := bluemonday.UGCPolicy()
    safeContent := p.Sanitize(content)

    // 替换 @mention 为带链接的 HTML — 但 username 未清洗！
    result := strings.ReplaceAll(safeContent,
        "@"+username,
        "<a href='/user/"+username+"'>@"+username+"</a>")
    return result
}
// 如果 username 是 '"><img src=x onerror=alert(1)>'，清洗后的内容又被注入了
```

```go
// ✅ 在所有拼接完成后再做最终清洗
func renderComment(content, username string) string {
    mentionHTML := "<a href='/user/" + template.HTMLEscapeString(username) +
        "'>@" + template.HTMLEscapeString(username) + "</a>"
    withMentions := strings.ReplaceAll(content, "@"+username, mentionHTML)

    p := bluemonday.UGCPolicy()
    p.AllowElements("a")
    p.AllowAttrs("href").OnElements("a")
    return p.Sanitize(withMentions) // 最终清洗
}
```

## 常见失效模式

| 失效模式 | 说明 |
|---------|------|
| 入库清洗 + 输出不转义 | 清洗了字段 A，但字段 B 未清洗，输出时 A 和 B 一起进入 HTML |
| 部分字段清洗 | 只清洗了 content，忽略了 title / username / filename 等字段 |
| 主渠道清洗 + 副渠道绕过 | 前台页面有转义，管理后台/邮件/导出/API 直出未转义 |
| 清洗后拼接 | 清洗后的安全内容与未清洗的内容拼接成 HTML |
| 清洗后解码 | 清洗后做了 URL decode / Base64 decode / HTML entity decode，恢复了恶意内容 |
| 字符串替换引入 HTML | `str_replace` / `strings.ReplaceAll` 将用户数据拼入 HTML 标签 |
| 非递归清洗 | `str_replace('<script>', '', $input)` 可被 `<scr<script>ipt>` 绕过 |

## 识别信号

| 信号 | 说明 |
|------|------|
| sanitizer 调用在 service/DAO 层（入库侧）而非 controller/template 层（输出侧） | 清洗位置靠前，输出侧可能绕过 |
| 同一数据在多个页面/渠道展示 | 检查每个渠道是否都经过清洗 |
| 清洗后有字符串拼接操作 | `String.format` / `f-string` / `fmt.Sprintf` / 模板字面量拼接 HTML |
| 只对 content 类字段清洗，忽略 metadata 字段 | title / name / filename / tag 可能也是用户可控的 |
| 项目有邮件通知/导出/预览等辅助功能 | 这些功能常不在主安全审查范围内 |

## 审计方法

1. **定位 sanitizer 调用点**：搜索 `DOMPurify` / `bleach` / `bluemonday` / `HtmlPolicyBuilder` / `Jsoup.clean` / `strip_tags` / `htmlspecialchars` 的调用位置
2. **判断清洗位置**：在 source→persist→read→sink 链路中，清洗发生在哪一步？是在入库前还是在输出前？
3. **检查清洗后的操作**：清洗后是否还有字符串拼接、模板渲染、字符替换、编解码操作？
4. **检查字段覆盖**：被清洗的字段集合是否覆盖了所有用户可控字段？有没有字段"漏网"？
5. **枚举展示渠道**：同一数据在前台、管理后台、邮件、导出、预览等渠道是否都经过了清洗？
