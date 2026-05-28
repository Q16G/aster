# 关键安全头缺失与利用（Missing Critical Security Headers）

## 漏洞模式

Web 应用未设置关键安全响应头，削弱了浏览器的内置安全防护机制。单个安全头缺失通常是 LOW/INFO 风险，但多个缺失的组合会显著降低整体安全态势，并为其他漏洞（XSS、MITM、Clickjacking）提供利用条件。

**核心原则：安全头是纵深防御层——它们不修复漏洞，但限制漏洞的利用方式和影响范围。缺失安全头不直接构成漏洞，但降低了攻击者利用其他漏洞的门槛。**

## 安全头缺失对照表

| Header | 缺失/错误值 | 攻击方式 | 正确值 |
|--------|-----------|---------|--------|
| `Strict-Transport-Security` (HSTS) | 缺失 | SSL Strip：中间人将 HTTPS 降级为 HTTP，截获所有流量 | `max-age=31536000; includeSubDomains` |
| `X-Content-Type-Options` | 缺失 | MIME 嗅探：浏览器猜测 Content-Type，将 text/plain 当作 text/html 执行 | `nosniff` |
| `X-Frame-Options` | 缺失 | Clickjacking：攻击者将页面嵌入透明 iframe，诱导用户点击 | `DENY` 或 `SAMEORIGIN` |
| `Referrer-Policy` | 缺失或 `unsafe-url` | Referer 泄露：URL 中的敏感参数（token/session）随 Referer 头发送到外部站点 | `strict-origin-when-cross-origin` 或 `no-referrer` |
| `X-XSS-Protection` | `0` 或缺失 | IE/旧版 Chrome 的 XSS 过滤器被禁用 | `0`（现代做法：依赖 CSP，此 header 已废弃但设 0 避免 XSS auditor 的信息泄露） |
| `Permissions-Policy` | 缺失 | 页面可访问摄像头、麦克风、地理位置等敏感浏览器 API | `camera=(), microphone=(), geolocation=()` |

## 攻击场景示例

### HSTS 缺失 → SSL Strip 攻击

```
攻击流程：
1. 受害者在咖啡厅连接 WiFi
2. 受害者输入 http://bank.com（非 https）
3. 中间人拦截请求，代替受害者与 bank.com 建立 HTTPS 连接
4. 中间人将 HTTPS 响应转为 HTTP 返回给受害者
5. 受害者在 HTTP 页面输入用户名密码 → 中间人截获

有 HSTS 时：
浏览器记住了 bank.com 必须用 HTTPS
即使用户输入 http://bank.com，浏览器自动升级为 https://bank.com
中间人无法降级
```

代码中设置 HSTS：

```java
// ❌ 未设置 HSTS
@Configuration
public class SecurityConfig extends WebSecurityConfigurerAdapter {
    @Override
    protected void configure(HttpSecurity http) throws Exception {
        http.headers().disable(); // 禁用了所有安全头！
    }
}

// ✅ 启用 HSTS
@Override
protected void configure(HttpSecurity http) throws Exception {
    http.headers()
        .httpStrictTransportSecurity()
            .includeSubDomains(true)
            .maxAgeInSeconds(31536000);
}
```

### X-Content-Type-Options 缺失 → MIME 嗅探攻击

```
攻击流程：
1. 攻击者上传文件 "profile.jpg"，内容实际是 HTML+JS：
   <html><script>alert(document.cookie)</script></html>
2. 服务端返回 Content-Type: image/jpeg（按扩展名）
3. 如果没有 X-Content-Type-Options: nosniff：
   浏览器可能嗅探内容，发现是 HTML → 当作 HTML 渲染 → 脚本执行
4. 如果有 nosniff：
   浏览器严格按 Content-Type 处理 → 当作图片（解析失败，不执行脚本）
```

```php
// ❌ 未设置 nosniff
header('Content-Type: ' . $mimeType);
readfile($uploadedFile);

// ✅ 设置 nosniff
header('X-Content-Type-Options: nosniff');
header('Content-Type: ' . $mimeType);
readfile($uploadedFile);
```

### X-Frame-Options 缺失 → Clickjacking

```
攻击流程：
1. 攻击者创建页面 evil.com，用 iframe 嵌入 target.com/settings/delete-account
2. iframe 设置为透明（opacity: 0），覆盖在一个"领取奖品"按钮上
3. 受害者以为点击了"领取奖品"，实际点击了 iframe 中的"删除账号"按钮
4. 由于浏览器自动携带 target.com 的 cookie，操作成功执行
```

```html
<!-- 攻击者页面 -->
<style>
  iframe { position: absolute; top: 0; left: 0; width: 100%; height: 100%; opacity: 0; z-index: 2; }
  .bait { position: relative; z-index: 1; }
</style>
<div class="bait"><button>点击领取奖品</button></div>
<iframe src="https://target.com/settings/delete-account"></iframe>
```

```go
// ❌ 未设置 X-Frame-Options
func handler(w http.ResponseWriter, r *http.Request) {
    // 响应中无防框架嵌入的头
    w.Write([]byte(pageHTML))
}

// ✅ 设置 X-Frame-Options
func handler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("X-Frame-Options", "DENY")
    w.Write([]byte(pageHTML))
}
```

### Referrer-Policy 缺失 → Token 泄露

```
攻击流程：
1. 用户访问 https://app.com/reset-password?token=secret_token
2. 页面中有外部链接或资源（如 <img src="https://analytics.com/pixel.gif">）
3. 浏览器发送请求到 analytics.com 时，Referer 头包含完整 URL：
   Referer: https://app.com/reset-password?token=secret_token
4. analytics.com 的日志中记录了密码重置 token
```

```python
# ❌ 未设置 Referrer-Policy
@app.after_request
def add_headers(response):
    return response

# ✅ 设置 Referrer-Policy
@app.after_request
def add_headers(response):
    response.headers['Referrer-Policy'] = 'strict-origin-when-cross-origin'
    return response
```

## 检测方法（代码层）

| 搜索目标 | rg pattern |
|---------|-----------|
| HSTS 设置 | `rg "Strict-Transport-Security\|hsts\|httpStrictTransportSecurity"` |
| X-Content-Type-Options | `rg "X-Content-Type-Options\|nosniff\|contentTypeOptions"` |
| X-Frame-Options | `rg "X-Frame-Options\|frame-ancestors\|frameOptions"` |
| Referrer-Policy | `rg "Referrer-Policy\|referrerPolicy"` |
| Spring Security 安全头 | `rg "\.headers\(\)\."` 检查是否 `.disable()` 禁用了安全头 |
| 全局 Filter/Middleware | `rg "addHeader\|setHeader\|Header\(\)" -g "Filter*" -g "Middleware*" -g "*Interceptor*"` |

## 审计方法

1. **搜索安全头设置**：在全局 Filter、Middleware、配置文件中搜索安全头设置
2. **逐头检查**：对照上方对照表，逐一确认每个安全头是否设置且值正确
3. **检查框架默认值**：Spring Security 默认启用部分安全头，但 `http.headers().disable()` 会全部禁用
4. **检查 Nginx/Apache 配置**：安全头可能在反向代理层设置而非应用层
5. **区分全局 vs 局部**：安全头是否覆盖了所有响应？静态资源响应是否也包含？
