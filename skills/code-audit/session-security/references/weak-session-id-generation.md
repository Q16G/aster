# 可预测的 Session ID 生成（Weak Session ID Generation）

## 漏洞模式

Session ID 使用非密码学安全的方式生成，导致攻击者可预测或枚举有效的 Session ID，从而劫持其他用户的会话。

**核心原则：Session ID 必须由密码学安全的随机数生成器（CSPRNG）生成，熵至少 128 bit。任何基于自增、时间戳、弱随机数、用户属性哈希的方案都是不安全的。**

常见不安全模式：
- 自增整数：`sessionId = ++counter`
- 时间戳：`sessionId = System.currentTimeMillis()`
- 弱随机数：`sessionId = Math.random()` / `rand()`
- 可预测哈希：`sessionId = MD5(userId + timestamp)`
- 用户属性组合：`sessionId = Base64(username + role)`

## 通用代码示例

### Java — 自定义 Session ID 生成器

```java
// ❌ 使用时间戳 + 用户名的 MD5 作为 Session ID
public String generateSessionId(String username) {
    String raw = username + System.currentTimeMillis();
    return DigestUtils.md5Hex(raw);
}

// ❌ 使用 java.util.Random（非密码学安全）
public String generateSessionId() {
    Random random = new Random();
    return Long.toHexString(random.nextLong());
}
```

```java
// ✅ 使用 SecureRandom（CSPRNG）
public String generateSessionId() {
    SecureRandom secureRandom = new SecureRandom();
    byte[] bytes = new byte[32]; // 256 bit
    secureRandom.nextBytes(bytes);
    return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
}

// ✅ 最佳实践：使用框架默认的 Session ID 生成（Tomcat/Jetty 默认使用 SecureRandom）
// 不要自定义 session ID 生成逻辑
```

### PHP — 弱 Session ID 配置

```php
// ❌ 自定义弱 session ID
session_id(md5(time() . $_SERVER['REMOTE_ADDR']));
session_start();

// ❌ php.ini 使用弱哈希
// session.hash_function = 0  (MD5, 128 bit — 勉强可接受但不推荐)
// session.entropy_length = 0  (无额外熵源)
```

```php
// ✅ PHP 7.1+ 默认配置已足够安全
// session.sid_length = 48（默认 32，推荐 48+）
// session.sid_bits_per_character = 6（默认 4，推荐 6）
// 不要自定义 session_id()，让 PHP 使用内置 CSPRNG 生成

ini_set('session.sid_length', '48');
ini_set('session.sid_bits_per_character', '6');
session_start();
```

### Python Flask — 自定义 Session 后端

```python
# ❌ 使用 random 模块（非 CSPRNG）生成 session token
import random
import string

def generate_session_token():
    chars = string.ascii_letters + string.digits
    return ''.join(random.choice(chars) for _ in range(32))

# ❌ 使用时间戳
import hashlib, time
def generate_session_token():
    return hashlib.md5(str(time.time()).encode()).hexdigest()
```

```python
# ✅ 使用 secrets 模块（CSPRNG）
import secrets

def generate_session_token():
    return secrets.token_urlsafe(32)  # 256 bit

# ✅ 最佳实践：使用 Flask 内置 session（基于 itsdangerous 签名）或 Flask-Session 扩展
# 不要自定义 session token 生成
```

### Go — 自定义 Session ID

```go
// ❌ 使用 math/rand（非 CSPRNG）
import "math/rand"

func generateSessionID() string {
    return fmt.Sprintf("%x", rand.Int63())
}

// ❌ 使用时间戳的哈希
func generateSessionID() string {
    h := md5.Sum([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
    return hex.EncodeToString(h[:])
}
```

```go
// ✅ 使用 crypto/rand（CSPRNG）
import "crypto/rand"

func generateSessionID() string {
    b := make([]byte, 32) // 256 bit
    if _, err := rand.Read(b); err != nil {
        panic(err)
    }
    return base64.URLEncoding.EncodeToString(b)
}
```

## 检测方法

1. **搜索自定义 session ID 生成**：`rg "session.?[Ii]d|sessionId|session_id|generateSession|newSession"` 找自定义生成逻辑
2. **检查随机数来源**：确认使用的是 `SecureRandom` / `crypto/rand` / `secrets` / `random_bytes` 而非 `Random` / `math/rand` / `random` / `rand()`
3. **检查哈希输入**：如果用 MD5/SHA 生成 session ID，检查输入是否可预测（时间戳、用户名、IP）
4. **检查框架配置**：确认没有覆盖框架默认的安全 session ID 生成器

## 利用场景

攻击者通过以下方式利用弱 Session ID：
- **枚举**：当 Session ID 为自增或小范围数值时，暴力枚举有效 session
- **预测**：当 Session ID 基于时间戳或已知信息时，计算目标用户的 session
- **碰撞**：当 Session ID 熵不足时，通过大量请求碰撞到有效 session
