# 授权伪校验：操作者来自客户端可控输入（Client-Derived Operator）

## 漏洞模式

端点在执行权限校验时**确实用到了 operator/tenantId/userId 等操作者标识**，表面上看起来有归属检查，但该操作者值来自请求参数（body / query / cookie / header）而非服务端安全上下文（session / SecurityContext / JWT claims / auth middleware）。攻击者只需修改请求中的 operator 字段即可绕过全部权限校验。

**与 idor-ownership-absence.md 的关键区别**：后者描述的是"根本没有 operator"——方法签名中只有资源 ID，完全不获取操作者身份。本模式描述的是"operator 存在但来源不可信"——代码里有 operator 参数、有 WHERE 条件、甚至有比对逻辑，但整条链路从入口就已被污染。**这类漏洞更具欺骗性，因为代码审查时很容易误判为"有鉴权"。**

识别核心：在 controller/handler 层追踪 operator 的来源。**如果 operator 来自 `@RequestParam` / `request.args` / `$_GET` / `$_POST` / `$_COOKIE` / `c.Query()` / `c.PostForm()` / request body，而不是来自 `session.getAttribute()` / `SecurityContextHolder` / `current_user` / `c.GetString("userID")`（auth middleware 注入），则该鉴权等同于无鉴权。**

## 通用代码示例

### Java Spring

```java
// ❌ 危险：userId 来自请求参数，攻击者可任意篡改
@GetMapping("/order/list")
public Result listOrders(@RequestParam Long userId) {
    // 看起来有 operator 过滤，实际上 userId 由客户端控制
    return Result.ok(orderService.selectByUserId(userId));
}

// ❌ 危险：tenantId 从 Cookie 读取，客户端可伪造
@GetMapping("/resource/list")
public Result listResources(@CookieValue("tenantId") String tenantId) {
    return Result.ok(resourceService.selectByTenant(tenantId));
}

// ✅ 安全：从 session 获取 userId
@GetMapping("/order/list")
public Result listOrders(HttpSession session) {
    Long userId = (Long) session.getAttribute("userId");
    return Result.ok(orderService.selectByUserId(userId));
}

// ✅ 安全：从 SecurityContext 获取身份
@GetMapping("/order/list")
public Result listOrders() {
    Authentication auth = SecurityContextHolder.getContext().getAuthentication();
    String username = auth.getName();
    return Result.ok(orderService.selectByUsername(username));
}
```

### PHP

```php
// ❌ 危险：tenant_id 来自 GET 参数
function listDevices() {
    $tenantId = $_GET['tenant_id'];
    $stmt = $pdo->prepare("SELECT * FROM devices WHERE tenant_id = ?");
    $stmt->execute([$tenantId]);
    return $stmt->fetchAll();
}

// ❌ 危险：user_id 来自 Cookie（客户端可伪造）
function viewProfile() {
    $userId = $_COOKIE['user_id'];
    $stmt = $pdo->prepare("SELECT * FROM users WHERE id = ?");
    $stmt->execute([$userId]);
    return $stmt->fetch();
}

// ✅ 安全：从 session 获取 tenant_id
function listDevices() {
    $tenantId = $_SESSION['tenant_id'];
    $stmt = $pdo->prepare("SELECT * FROM devices WHERE tenant_id = ?");
    $stmt->execute([$tenantId]);
    return $stmt->fetchAll();
}
```

### Python Flask

```python
# ❌ 危险：user_id 从请求参数获取
@app.route('/tickets')
@login_required
def list_tickets():
    user_id = request.args.get('user_id')
    tickets = Ticket.query.filter_by(owner_id=user_id).all()
    return jsonify([t.to_dict() for t in tickets])

# ❌ 危险：operator 从请求 body 获取
@app.route('/orders/<int:order_id>/cancel', methods=['POST'])
@login_required
def cancel_order(order_id):
    data = request.get_json()
    operator_id = data.get('operator_id')
    order = Order.query.filter_by(id=order_id, owner_id=operator_id).first_or_404()
    order.status = 'cancelled'
    db.session.commit()
    return jsonify({"ok": True})

# ✅ 安全：使用 Flask-Login 的 current_user
@app.route('/tickets')
@login_required
def list_tickets():
    tickets = Ticket.query.filter_by(owner_id=current_user.id).all()
    return jsonify([t.to_dict() for t in tickets])
```

### Go Gin

```go
// ❌ 危险：userId 从 query 参数获取，攻击者可任意指定
func ListOrders(c *gin.Context) {
    userID := c.Query("userId") // 客户端可控
    orders, _ := service.GetOrdersByUser(userID)
    c.JSON(200, orders)
}

// ❌ 危险：tenantId 从 POST form 获取
func CreateResource(c *gin.Context) {
    tenantID := c.PostForm("tenantId")
    resource := &model.Resource{TenantID: tenantID}
    // ... 创建资源，tenant 由攻击者指定
    service.CreateResource(resource)
    c.JSON(200, gin.H{"ok": true})
}

// ✅ 安全：从 auth middleware 注入的 context 获取
func ListOrders(c *gin.Context) {
    userID := c.GetString("userID") // auth middleware 从 JWT/session 解析后写入
    orders, _ := service.GetOrdersByUser(userID)
    c.JSON(200, orders)
}
```

## 识别信号

| 信号 | 说明 |
|------|------|
| `@RequestParam` / `@PathVariable` 传入 userId / account / tenantId | operator 来自请求 URL 或表单，攻击者可任意篡改 |
| `$_GET['user_id']` / `$_POST['tenant_id']` / `$_COOKIE['role']` | PHP 中从超全局变量获取操作者身份，完全由客户端控制 |
| `request.args.get('user_id')` / `request.json.get('operator_id')` | Python 中从请求参数或 body 提取操作者 |
| `c.Query("userId")` / `c.PostForm("tenantId")` | Go Gin 中从 query/form 获取操作者，而非 `c.GetString()` 从 middleware context |
| SQL/ORM 查询有 operator 条件但值可追溯至请求参数 | WHERE 子句看起来完整，但 operator 变量来源不可信 |
| 存在权限比对逻辑但比对双方均可被攻击者控制 | 如 `if (requestUserId == requestOwnerId)` —— 两边都来自请求 |

## 常见出现场景

- **多租户系统**：tenantId 通过请求头/Cookie/query 传递而非从登录态解析，导致跨租户访问
- **订单/工单查询**：列表接口用 `?userId=xxx` 过滤，攻击者替换为其他用户 ID 即可查看他人数据
- **操作确认接口**：取消/修改/审批操作中 operator_id 从 body 传入，绕过归属验证
- **API 网关透传**：上游网关将用户身份放入请求头但未签名，下游服务直接信任 `X-User-Id` header
- **前后端分离架构**：前端 localStorage 中的 userId 随请求发送，后端直接使用而不从 token 解析
- **Cookie 伪鉴权**：将 userId/role 写入普通 Cookie（非 HttpOnly signed session），后端读取后当作可信身份

## 审计方法

1. **追踪 operator 来源**：对每个端点，找到 operator/userId/tenantId/account 变量的首次赋值点。如果赋值来源是 `@RequestParam` / `request.args` / `$_GET` / `$_POST` / `$_COOKIE` / `c.Query()` / `c.PostForm()` / request header，标记为 client-derived
2. **对比安全来源**：确认项目中是否存在从 session / SecurityContext / auth middleware context 获取操作者的标准模式。如果同一项目中有的端点用 session、有的端点用 request param，后者是高风险点
3. **检查中间层传递**：即使 controller 层从 session 获取了 operator，追踪到 service / mapper 层确认该值未被请求参数覆盖。特别注意 DTO/Entity 中同时存在 operator 字段且会被 request body 自动绑定的情况
4. **审查 Cookie 可信度**：区分 server-side session cookie（如 JSESSIONID / connect.sid）和 application-level cookie（如 `userId=123` / `role=admin`）。后者由客户端完全控制
5. **验证 API 网关/中间件链**：如果 operator 来自请求头（如 `X-User-Id`），确认上游是否有签名/验证机制。无签名的透传等同于客户端直接控制
