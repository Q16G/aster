# PHP 运行时危险配置（PHP Dangerous Runtime Configuration）

## 漏洞模式

PHP 的运行时配置（`php.ini`、`.htaccess`、`ini_set()`）直接影响应用的安全边界。某些配置项在危险值下会将低危漏洞升级为高危，或直接开启攻击面。

**核心原则：危险配置本身可能不构成漏洞，但它是漏洞升级的前提条件。`allow_url_include=On` 单独不是漏洞，但 LFI + `allow_url_include=On` = RCE。**

## 危险配置清单与利用链

| 配置项 | 危险值 | 安全值 | 风险 | 利用链 |
|--------|-------|--------|------|--------|
| `allow_url_include` | `On` | `Off` | LFI → RCE | `include($_GET['page'])` + `php://input` POST 注入 PHP 代码 |
| `allow_url_fopen` | `On` | `Off`（除非业务需要） | SSRF / 远程读取 | `file_get_contents($_GET['url'])` → 读取内网服务 |
| `display_errors` | `On`（生产） | `Off` | 信息泄露 | 错误消息暴露文件路径、SQL 语句、数据库结构 |
| `expose_php` | `On` | `Off` | 版本泄露 | `X-Powered-By: PHP/7.4.3` → 攻击者针对已知 CVE |
| `open_basedir` | 未设置 | 设置为应用根目录 | 任意读取 | `include('/etc/passwd')` 无目录限制 |
| `disable_functions` | 空 | 禁用 `exec,system,passthru,shell_exec,popen,proc_open` | 命令执行 | 即使注入 PHP 代码，无可用命令执行函数则无法 RCE |
| `session.cookie_httponly` | `0` | `1` | XSS 窃取 Session | `document.cookie` 可读取 PHPSESSID |
| `session.cookie_secure` | `0` | `1` | 明文传输 Session | HTTP 请求中 Session Cookie 被中间人截获 |
| `session.use_strict_mode` | `0` | `1` | Session 固定 | 攻击者预设 Session ID 被服务端接受 |
| `session.use_only_cookies` | `0` | `1` | Session 固定 | 通过 URL 参数传递 Session ID |
| `register_globals` | `On` | `Off`（PHP 5.4+ 已移除） | 变量覆盖 | `?admin=1` 覆盖 `$admin` 变量 |
| `magic_quotes_gpc` | `Off`（PHP 5.4+ 已移除） | — | 无自动转义 | 旧代码依赖 magic_quotes 防注入，关闭后无防护 |

## 利用链示例

### allow_url_include=On + LFI → RCE

```php
// 应用代码（LFI 漏洞）
include($_GET['page'] . '.php');

// 正常使用
// http://target.com/?page=about  → include('about.php')

// 攻击（需要 allow_url_include=On）
// POST http://target.com/?page=php://input
// Body: <?php system('whoami'); ?>
// → include('php://input.php') → 执行 POST body 中的 PHP 代码

// 攻击方法2（data:// 协议）
// http://target.com/?page=data://text/plain;base64,PD9waHAgc3lzdGVtKCd3aG9hbWknKTsgPz4=
```

### display_errors=On + SQL 错误 → 信息泄露

```php
// 应用代码
$result = mysqli_query($conn, "SELECT * FROM users WHERE id = " . $_GET['id']);
// 攻击: ?id=1' 
// 响应（display_errors=On）:
// Warning: mysqli_query(): ... You have an error in your SQL syntax near ''' 
// at line 1 in /var/www/app/includes/db.php on line 42
// → 泄露：数据库类型、文件路径、SQL 结构
```

### disable_functions 为空 + 代码注入 → RCE

```php
// 即使攻击者能注入 PHP 代码（eval 注入、模板注入、反序列化）
// 如果 disable_functions 禁用了所有命令执行函数：
// disable_functions = exec,system,passthru,shell_exec,popen,proc_open,pcntl_exec
// 攻击者的 system('whoami') 会被阻止

// 但如果 disable_functions 为空，任何代码注入都能直接 RCE
```

## 配置文件位置

| 位置 | 优先级 | 说明 |
|------|--------|------|
| `/etc/php.ini` 或 `/etc/php/X.Y/apache2/php.ini` | 全局 | 系统级配置 |
| `/etc/php/X.Y/fpm/php.ini` | 全局（FPM） | PHP-FPM 配置 |
| `.htaccess` | 目录级 | Apache 下可覆盖部分 `php.ini` 设置 |
| `ini_set()` | 运行时 | 代码中动态修改（仅部分配置可改） |
| `.user.ini` | 目录级 | CGI/FastCGI 模式下的用户级配置 |

## 其他语言/框架的等价配置

PHP 的运行时配置风险模式在其他语言中有等价形态：

| PHP 配置 | Python (Django/Flask) | Go | Node.js |
|---------|----------------------|----|---------|
| `display_errors=On` | `DEBUG=True`（Django） / `app.debug=True`（Flask） | 无框架级开关，但 `gin.SetMode(gin.DebugMode)` 类似 | `NODE_ENV=development` |
| `allow_url_include=On` | 无直接等价（Python 无 include 机制） | 无直接等价 | 无直接等价 |
| `disable_functions=空` | 无等价（Python 无函数禁用机制） | 无等价 | `--disallow-code-generation-from-strings`（仅 eval 相关） |
| `open_basedir=未设置` | `ALLOWED_HOSTS` 未限制（不完全等价） | 无等价 | 无等价 |
| `session.cookie_httponly=0` | `SESSION_COOKIE_HTTPONLY=False` | `http.Cookie{HttpOnly: false}` | `cookie: { httpOnly: false }`（express-session） |
| `session.cookie_secure=0` | `SESSION_COOKIE_SECURE=False` | `http.Cookie{Secure: false}` | `cookie: { secure: false }` |

> 注意：PHP 的配置风险模式（运行时 ini 控制安全边界）较为特殊。其他语言的等价风险通常分散在框架配置、环境变量和代码中，由 `java-spring-exposure-config.md` 和各框架的文档覆盖。

## 审计方法

1. **搜索 PHP 配置文件**：`rg -g "php.ini" -g ".htaccess" -g ".user.ini" "allow_url_include|display_errors|expose_php|open_basedir|disable_functions|register_globals|session\."` 
2. **搜索运行时配置修改**：`rg "ini_set\(|ini_get\(" *.php` 检查代码中的动态配置变更
3. **逐项对照危险值**：将实际配置值与上方清单对比
4. **评估利用链**：某个危险配置是否与代码中的其他漏洞构成组合攻击（如 LFI + allow_url_include）
5. **检查生产 vs 开发**：确认 `display_errors` / `debug` 等配置在生产环境是否关闭
