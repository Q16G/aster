# Session 固定攻击：登录后未重建 Session（Session Fixation — Missing Regeneration）

## 漏洞模式

用户登录成功后，服务端未重新生成 Session ID，导致攻击者可以预先设置一个已知的 Session ID，诱导受害者使用该 Session ID 登录。登录后该 Session ID 被提升为已认证状态，攻击者用同一 Session ID 即可劫持受害者会话。

**核心原则：每次权限级别变化（匿名→已认证、普通用户→管理员）时，必须重新生成 Session ID。旧 Session ID 必须失效。**

## 攻击流程

```
1. 攻击者访问应用，获得 Session ID: SESS=abc123
2. 攻击者通过某种方式让受害者使用同一 Session ID（URL 参数、Cookie 注入、Meta 标签）
3. 受害者使用 SESS=abc123 登录成功
4. 服务端将 SESS=abc123 标记为已认证，但未更换 ID
5. 攻击者用 SESS=abc123 访问应用 → 以受害者身份通过认证
```

## 通用代码示例

### Java Servlet / Spring

```java
// ❌ 登录成功后直接在旧 session 上设置属性，未重建 session
@PostMapping("/login")
public String login(@RequestParam String username, @RequestParam String password,
                    HttpSession session) {
    User user = userService.authenticate(username, password);
    if (user != null) {
        session.setAttribute("user", user);      // 在旧 session 上设置
        session.setAttribute("authenticated", true);
        return "redirect:/dashboard";
    }
    return "redirect:/login?error";
}
```

```java
// ✅ 登录成功后先销毁旧 session，再创建新 session
@PostMapping("/login")
public String login(@RequestParam String username, @RequestParam String password,
                    HttpServletRequest request) {
    User user = userService.authenticate(username, password);
    if (user != null) {
        request.getSession().invalidate();  // 销毁旧 session
        HttpSession newSession = request.getSession(true);  // 创建新 session
        newSession.setAttribute("user", user);
        newSession.setAttribute("authenticated", true);
        return "redirect:/dashboard";
    }
    return "redirect:/login?error";
}

// ✅ 或使用 Servlet 3.1+ 的 changeSessionId()（保留 session 数据，只换 ID）
@PostMapping("/login")
public String login(@RequestParam String username, @RequestParam String password,
                    HttpServletRequest request) {
    User user = userService.authenticate(username, password);
    if (user != null) {
        request.changeSessionId();  // 只更换 Session ID，保留数据
        request.getSession().setAttribute("user", user);
        return "redirect:/dashboard";
    }
    return "redirect:/login?error";
}
```

### PHP

```php
// ❌ 登录成功后未重建 session
function login($username, $password) {
    session_start();
    $user = authenticate($username, $password);
    if ($user) {
        $_SESSION['user_id'] = $user['id'];
        $_SESSION['role'] = $user['role'];
        header("Location: /dashboard");
        exit;
    }
}
```

```php
// ✅ 登录成功后重建 session
function login($username, $password) {
    session_start();
    $user = authenticate($username, $password);
    if ($user) {
        session_regenerate_id(true); // true = 删除旧 session 文件
        $_SESSION['user_id'] = $user['id'];
        $_SESSION['role'] = $user['role'];
        header("Location: /dashboard");
        exit;
    }
}
```

### Python Flask

```python
# ❌ 登录成功后直接设置 session，未重建
@app.route('/login', methods=['POST'])
def login():
    user = authenticate(request.form['username'], request.form['password'])
    if user:
        session['user_id'] = user.id
        session['authenticated'] = True
        return redirect(url_for('dashboard'))
    return redirect(url_for('login', error=1))
```

```python
# ✅ 登录成功后清空并重建 session
@app.route('/login', methods=['POST'])
def login():
    user = authenticate(request.form['username'], request.form['password'])
    if user:
        session.clear()  # 清空旧 session 数据
        session['user_id'] = user.id
        session['authenticated'] = True
        # Flask 的 session 基于签名 cookie，clear() + 重新赋值会生成新签名
        return redirect(url_for('dashboard'))
    return redirect(url_for('login', error=1))
```

### Go — gin-contrib/sessions

```go
// ❌ 登录成功后未重建 session
func loginHandler(c *gin.Context) {
    session := sessions.Default(c)
    user := authenticate(c.PostForm("username"), c.PostForm("password"))
    if user != nil {
        session.Set("userId", user.ID)
        session.Save()
        c.Redirect(302, "/dashboard")
        return
    }
    c.Redirect(302, "/login?error=1")
}
```

```go
// ✅ 登录成功后清空并重建 session
func loginHandler(c *gin.Context) {
    session := sessions.Default(c)
    user := authenticate(c.PostForm("username"), c.PostForm("password"))
    if user != nil {
        session.Clear()  // 清空旧数据
        session.Options(sessions.Options{MaxAge: -1}) // 删除旧 cookie
        session.Save()
        // 创建新 session
        session.Options(sessions.Options{
            MaxAge:   3600,
            HttpOnly: true,
            Secure:   true,
        })
        session.Set("userId", user.ID)
        session.Save()
        c.Redirect(302, "/dashboard")
        return
    }
    c.Redirect(302, "/login?error=1")
}
```

## 检测方法

1. **定位登录接口**：搜索 `login` / `signin` / `authenticate` 方法
2. **检查 session 操作顺序**：在认证成功分支中，是否在设置用户信息**之前**调用了 session 重建
3. **搜索关键函数**：
   - Java：`session.invalidate()` / `request.changeSessionId()`
   - PHP：`session_regenerate_id()`
   - Python：`session.clear()` / `session.regenerate()`
   - Go：`session.Clear()` + 重建
4. **如果无上述调用**：该登录流程存在 Session 固定风险

## 利用场景

- 攻击者通过 URL 参数注入 Session ID（`http://target.com/login;jsessionid=attacker_session`）
- 攻击者通过子域 Cookie 注入 Session ID
- 攻击者通过同站 XSS 设置 Cookie
- 受害者使用该 Session ID 登录后，攻击者即获得已认证会话
