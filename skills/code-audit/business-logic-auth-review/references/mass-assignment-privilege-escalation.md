# 批量赋值提权：自动绑定覆盖敏感字段（Mass Assignment Privilege Escalation）

## 漏洞模式

框架提供的自动对象绑定机制（`@ModelAttribute` / `BindJSON` / `setattr` / `$_POST` 遍历赋值）将请求参数直接映射到实体对象的所有字段，攻击者通过在请求中附加额外字段（如 `role=admin` / `isAdmin=true` / `status=active` / `balance=999999`）即可覆盖本不应由用户控制的敏感属性。

**核心原则：自动绑定默认是"全量映射"——框架不区分"用户应该提交的字段"和"系统内部字段"。** 开发者如果不显式限制可绑定字段（白名单）或排除敏感字段（黑名单），攻击者就可以通过构造请求修改任意实体属性。

此漏洞的危害取决于被覆盖字段的语义：覆盖 `role` / `isAdmin` 是提权；覆盖 `price` / `balance` 是业务欺诈；覆盖 `status` / `verified` 是流程绕过。**审计时应关注实体中所有非用户输入字段，而不仅仅是权限相关字段。**

## 通用代码示例

### Java Spring

```java
// ❌ 危险：@ModelAttribute 绑定整个 UserEntity，包括 role 字段
// 攻击者发送 POST /user/register?username=test&password=123&role=admin
@PostMapping("/user/register")
public Result register(@ModelAttribute UserEntity user) {
    userService.save(user);  // user.role 被攻击者设为 "admin"
    return Result.ok();
}

// UserEntity 定义
public class UserEntity {
    private String username;
    private String password;
    private String role;      // 攻击者可通过请求参数覆盖
    private Boolean isAdmin;  // 攻击者可通过请求参数覆盖
    private Integer status;   // 攻击者可通过请求参数覆盖
    // getters & setters ...
}

// ❌ 危险：@RequestBody 绑定 JSON 到实体
// 攻击者发送 {"username":"test","password":"123","role":"admin"}
@PostMapping("/user/update")
public Result update(@RequestBody UserEntity user) {
    userService.updateById(user);
    return Result.ok();
}

// ✅ 安全写法 1：使用 DTO 只接受允许的字段
@PostMapping("/user/register")
public Result register(@RequestBody RegisterDTO dto) {
    UserEntity user = new UserEntity();
    user.setUsername(dto.getUsername());
    user.setPassword(dto.getPassword());
    user.setRole("user");  // 服务端强制设置默认角色
    userService.save(user);
    return Result.ok();
}

// ✅ 安全写法 2：使用 @InitBinder 限制可绑定字段
@InitBinder
public void initBinder(WebDataBinder binder) {
    binder.setDisallowedFields("role", "isAdmin", "status");
}
```

### PHP

```php
// ❌ 危险：遍历 $_POST 赋值所有字段到对象
// 攻击者 POST: username=test&password=123&role=admin&is_admin=1
function createUser() {
    $user = new User();
    foreach ($_POST as $key => $value) {
        $user->$key = $value;  // role、is_admin 均被覆盖
    }
    $user->save();
}

// ❌ 危险：array_merge 合并请求数据
function updateProfile() {
    $user = User::find($_SESSION['user_id']);
    $data = array_merge($user->toArray(), $_POST);
    // 攻击者在 POST 中加入 role=admin 即可提权
    $user->fill($data);
    $user->save();
}

// ✅ 安全写法：只提取允许的字段
function createUser() {
    $allowed = ['username', 'password', 'email'];
    $data = array_intersect_key($_POST, array_flip($allowed));
    $user = new User();
    foreach ($data as $key => $value) {
        $user->$key = $value;
    }
    $user->role = 'user';  // 服务端强制设置
    $user->save();
}
```

### Python Flask

```python
# ❌ 危险：遍历 request JSON 设置所有属性
# 攻击者发送 {"username":"test","email":"t@t.com","role":"admin","is_active":true}
@app.route('/user/register', methods=['POST'])
def register():
    user = User()
    for k, v in request.json.items():
        setattr(user, k, v)  # role、is_active 均被覆盖
    db.session.add(user)
    db.session.commit()
    return jsonify({"ok": True})

# ❌ 危险：dict 解包创建对象
@app.route('/user/register', methods=['POST'])
def register():
    user = User(**request.json)  # 所有 JSON 字段映射到 Model
    db.session.add(user)
    db.session.commit()
    return jsonify({"ok": True})

# ✅ 安全写法：显式提取允许字段
@app.route('/user/register', methods=['POST'])
def register():
    data = request.get_json()
    user = User(
        username=data.get('username'),
        email=data.get('email'),
        password=generate_hash(data.get('password')),
        role='user'  # 服务端强制设置
    )
    db.session.add(user)
    db.session.commit()
    return jsonify({"ok": True})
```

### Go Gin

```go
// ❌ 危险：BindJSON 绑定整个 User 结构体，包括 Role 字段
// 攻击者发送 {"username":"test","password":"123","role":"admin"}
func Register(c *gin.Context) {
    var user model.User
    if err := c.BindJSON(&user); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    // user.Role 已被攻击者设为 "admin"
    db.Create(&user)
    c.JSON(200, gin.H{"ok": true})
}

// User 结构体定义
type User struct {
    ID       uint   `json:"id"`
    Username string `json:"username"`
    Password string `json:"password"`
    Role     string `json:"role"`     // 攻击者可通过 JSON 覆盖
    IsAdmin  bool   `json:"is_admin"` // 攻击者可通过 JSON 覆盖
    Status   int    `json:"status"`   // 攻击者可通过 JSON 覆盖
}

// ✅ 安全写法 1：使用专用请求结构体（DTO）
type RegisterRequest struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required"`
    Email    string `json:"email"    binding:"required,email"`
}

func Register(c *gin.Context) {
    var req RegisterRequest
    if err := c.BindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    user := model.User{
        Username: req.Username,
        Password: hashPassword(req.Password),
        Email:    req.Email,
        Role:     "user", // 服务端强制设置
    }
    db.Create(&user)
    c.JSON(200, gin.H{"ok": true})
}

// ✅ 安全写法 2：绑定后覆盖敏感字段
func Register(c *gin.Context) {
    var user model.User
    if err := c.BindJSON(&user); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    user.Role = "user"       // 强制覆盖
    user.IsAdmin = false     // 强制覆盖
    user.Status = 0          // 强制覆盖
    db.Create(&user)
    c.JSON(200, gin.H{"ok": true})
}
```

## 识别信号

| 信号 | 说明 |
|------|------|
| `@ModelAttribute` / `@RequestBody` 直接绑定到 Entity 而非 DTO | 实体中的所有字段均可被请求覆盖，包括 role / status / isAdmin |
| `c.BindJSON(&entity)` / `c.ShouldBind(&entity)` 目标为完整模型 | Go 中将请求直接绑定到数据库模型结构体 |
| `setattr(obj, k, v)` / `User(**request.json)` 遍历赋值 | Python 中动态属性设置，无字段白名单限制 |
| `foreach $_POST as $key => $value` 赋值到对象 | PHP 中遍历请求参数批量赋值 |
| Entity/Model 中含 `role` / `isAdmin` / `status` / `balance` / `verified` 字段 | 这些字段不应由用户请求直接控制 |
| 缺少独立的 DTO/Request 结构体，Controller 层直接操作 Entity | 无输入层与持久层的隔离，绑定即入库 |

## 常见出现场景

- **用户注册**：注册表单只展示 username/password/email，但后端绑定整个 UserEntity，攻击者添加 `role=admin` 即可注册管理员
- **个人资料更新**：更新昵称、头像等，但绑定到完整用户对象，攻击者可同时修改 `status` / `verified` / `balance`
- **订单创建/修改**：前端只提交商品和数量，但后端绑定整个 OrderEntity，攻击者可修改 `totalPrice` / `discount` / `status`
- **工单/任务提交**：提交工单内容，但绑定到 TicketEntity，攻击者可修改 `priority` / `assignee` / `status`
- **设备/资源注册**：IoT 设备注册时绑定整个 DeviceEntity，攻击者可修改 `tenantId` 将设备注册到其他租户下
- **批量导入**：CSV/JSON 批量导入用户或数据时，解析逻辑未过滤敏感字段，每条记录都可能携带提权字段

## 审计方法

1. **定位自动绑定入口**：搜索 `@ModelAttribute` / `@RequestBody` / `BindJSON` / `ShouldBind` / `setattr` / `$_POST` 遍历赋值 / `**kwargs` 解包等自动绑定模式
2. **检查绑定目标类型**：确认绑定目标是 Entity/Model（含敏感字段）还是 DTO/Request（只含允许字段）。如果直接绑定到 Entity，列出该 Entity 的所有字段，标记哪些不应由用户控制
3. **检查字段保护机制**：确认是否有 `@InitBinder` / `setDisallowedFields` / `json:"-"` tag / `$fillable` / `$guarded` / Schema 白名单等机制限制可绑定字段
4. **检查绑定后覆盖**：即使绑定到了完整 Entity，是否在 save 之前强制覆盖了敏感字段（如 `user.setRole("user")`）。注意：覆盖必须在所有代码路径上都生效
5. **验证更新场景**：更新操作（update/patch）比创建操作更危险——创建时可能有默认值保护，但更新时 `updateById` 会将攻击者设置的字段写入数据库
