# 多租户隔离失效：跨租户数据泄露（Tenant Isolation Failure）

## 漏洞模式

多租户（multi-tenant）系统中，数据查询缺少 tenant_id 作用域限制，导致一个租户的用户可以查看、修改甚至删除其他租户的数据。与单用户 IDOR 不同，**租户隔离失效的影响范围是整个组织/企业的全部数据**，危害等级通常更高。

租户隔离失效有三种典型形态：
1. **查询无 tenant 过滤**：SQL/ORM 查询完全没有 tenant_id 条件，返回全部租户数据
2. **tenant_id 来自客户端**：查询有 tenant_id 条件，但该值从请求参数/Cookie/Header 获取而非从登录态解析（与 authz-client-derived-operator.md 模式叠加）
3. **缓存/索引无 tenant 前缀**：数据查询有租户隔离，但缓存 key 未包含 tenant_id，导致跨租户缓存命中

**核心原则：多租户系统中，每一条数据访问路径——包括数据库查询、缓存读写、文件存储、消息队列——都必须携带并校验 tenant 作用域。遗漏任何一层都会导致隔离失效。**

## 通用代码示例

### Java Spring + MyBatis — 查询缺少 tenant 过滤

```java
// ❌ 危险：查询无 tenant_id 条件，返回所有租户的订单
@GetMapping("/order/list")
public Result listOrders(@RequestParam(required = false) String status) {
    return Result.ok(orderService.selectByStatus(status));
}
```

```xml
<!-- ❌ 危险：MyBatis 查询无 tenant 过滤 -->
<select id="selectByStatus" resultType="Order">
    SELECT * FROM orders
    <where>
        <if test="status != null">
            AND status = #{status}
        </if>
    </where>
</select>

<!-- ✅ 安全：加入 tenant_id 条件 -->
<select id="selectByStatusAndTenant" resultType="Order">
    SELECT * FROM orders
    <where>
        AND tenant_id = #{tenantId}
        <if test="status != null">
            AND status = #{status}
        </if>
    </where>
</select>
```

```java
// ✅ 安全：从 SecurityContext 获取 tenantId
@GetMapping("/order/list")
public Result listOrders(@RequestParam(required = false) String status) {
    String tenantId = SecurityContextHolder.getContext()
        .getAuthentication().getDetails().getTenantId();
    return Result.ok(orderService.selectByStatusAndTenant(status, tenantId));
}
```

### Java — tenant_id 来自请求参数

```java
// ❌ 危险：tenantId 从请求参数获取，攻击者可篡改
@GetMapping("/device/list")
public Result listDevices(@RequestParam String tenantId) {
    return Result.ok(deviceService.selectByTenant(tenantId));
}

// ✅ 安全：从登录态获取
@GetMapping("/device/list")
public Result listDevices(HttpSession session) {
    String tenantId = (String) session.getAttribute("tenantId");
    return Result.ok(deviceService.selectByTenant(tenantId));
}
```

### PHP — 查询缺少 tenant 过滤

```php
// ❌ 危险：查询无 tenant_id，返回所有租户数据
function listResources() {
    $stmt = $pdo->prepare("SELECT * FROM resources WHERE status = ?");
    $stmt->execute([$_GET['status']]);
    return $stmt->fetchAll();
}

// ❌ 危险：tenant_id 来自 Cookie
function listResources() {
    $tenantId = $_COOKIE['tenant_id']; // 客户端可伪造
    $stmt = $pdo->prepare("SELECT * FROM resources WHERE tenant_id = ? AND status = ?");
    $stmt->execute([$tenantId, $_GET['status']]);
    return $stmt->fetchAll();
}

// ✅ 安全：tenant_id 来自 session
function listResources() {
    $tenantId = $_SESSION['tenant_id'];
    $stmt = $pdo->prepare("SELECT * FROM resources WHERE tenant_id = ? AND status = ?");
    $stmt->execute([$tenantId, $_GET['status']]);
    return $stmt->fetchAll();
}
```

### Python Flask / SQLAlchemy — 查询缺少 tenant 过滤

```python
# ❌ 危险：查询无 tenant 过滤，返回所有租户的工单
@app.route('/tickets')
@login_required
def list_tickets():
    status = request.args.get('status')
    tickets = Ticket.query.filter_by(status=status).all()
    return jsonify([t.to_dict() for t in tickets])

# ❌ 危险：tenant_id 从请求参数获取
@app.route('/tickets')
@login_required
def list_tickets():
    tenant_id = request.args.get('tenant_id')  # 客户端可控
    tickets = Ticket.query.filter_by(tenant_id=tenant_id).all()
    return jsonify([t.to_dict() for t in tickets])

# ✅ 安全：从 current_user 获取 tenant
@app.route('/tickets')
@login_required
def list_tickets():
    status = request.args.get('status')
    tickets = Ticket.query.filter_by(
        tenant_id=current_user.tenant_id,
        status=status
    ).all()
    return jsonify([t.to_dict() for t in tickets])
```

### Go GORM — 查询缺少 tenant 过滤

```go
// ❌ 危险：查询无 tenant 条件
func ListOrders(c *gin.Context) {
    var orders []model.Order
    status := c.Query("status")
    db.Where("status = ?", status).Find(&orders)
    c.JSON(200, orders)
}

// ❌ 危险：tenantID 从请求参数获取
func ListOrders(c *gin.Context) {
    var orders []model.Order
    tenantID := c.Query("tenantId") // 客户端可控
    db.Where("tenant_id = ? AND status = ?", tenantID, c.Query("status")).Find(&orders)
    c.JSON(200, orders)
}

// ✅ 安全：从 auth middleware context 获取 tenantID
func ListOrders(c *gin.Context) {
    var orders []model.Order
    tenantID := c.GetString("tenantID") // auth middleware 注入
    status := c.Query("status")
    db.Where("tenant_id = ? AND status = ?", tenantID, status).Find(&orders)
    c.JSON(200, orders)
}

// ✅ 更安全：使用 GORM Scope 全局注入 tenant 过滤
func TenantScope(tenantID string) func(db *gorm.DB) *gorm.DB {
    return func(db *gorm.DB) *gorm.DB {
        return db.Where("tenant_id = ?", tenantID)
    }
}

func ListOrders(c *gin.Context) {
    var orders []model.Order
    tenantID := c.GetString("tenantID")
    db.Scopes(TenantScope(tenantID)).Where("status = ?", c.Query("status")).Find(&orders)
    c.JSON(200, orders)
}
```

### 缓存层隔离失效（跨语言通用模式）

```java
// ❌ 危险：缓存 key 无 tenant 前缀，租户 A 写入的缓存被租户 B 命中
public Order getOrder(Long orderId) {
    String cacheKey = "order:" + orderId;  // 缺少 tenant 前缀
    Order cached = redis.get(cacheKey);
    if (cached != null) return cached;
    Order order = orderMapper.selectById(orderId);
    redis.set(cacheKey, order);
    return order;
}

// ✅ 安全：缓存 key 包含 tenant 前缀
public Order getOrder(Long orderId, String tenantId) {
    String cacheKey = "tenant:" + tenantId + ":order:" + orderId;
    Order cached = redis.get(cacheKey);
    if (cached != null) return cached;
    Order order = orderMapper.selectByIdAndTenant(orderId, tenantId);
    redis.set(cacheKey, order);
    return order;
}
```

```python
# ❌ 危险：缓存 key 无 tenant 前缀
def get_user_config(user_id):
    cache_key = f"config:{user_id}"  # 不同租户同 user_id 冲突
    cached = redis_client.get(cache_key)
    if cached:
        return json.loads(cached)
    config = UserConfig.query.get(user_id)
    redis_client.set(cache_key, json.dumps(config.to_dict()))
    return config.to_dict()

# ✅ 安全：缓存 key 包含 tenant
def get_user_config(user_id, tenant_id):
    cache_key = f"tenant:{tenant_id}:config:{user_id}"
    # ...
```

## 识别信号

| 信号 | 说明 |
|------|------|
| 数据库表有 `tenant_id` 字段但查询未使用 | 表结构支持多租户，但 SQL/ORM 查询未加 tenant_id 条件 |
| 全表查询 `SELECT * FROM table` 或 `.query.all()` / `db.Find(&list)` | 无任何 WHERE 条件，返回所有租户数据 |
| tenant_id 从 `@RequestParam` / `$_GET` / `request.args` / `c.Query()` 获取 | 租户标识来自客户端可控输入，而非 session/token |
| 缓存 key 格式为 `entity:{id}` 无 tenant 前缀 | 不同租户的相同 ID 会命中同一缓存条目 |
| 项目有 `tenant` / `organization` / `company` / `workspace` 概念但无全局过滤器 | 缺少 MyBatis Interceptor / GORM Scope / SQLAlchemy event / middleware 等全局 tenant 注入 |
| 文件存储路径无 tenant 隔离 | 如 `/uploads/{filename}` 而非 `/uploads/{tenantId}/{filename}`，可遍历访问其他租户文件 |

## 常见出现场景

- **SaaS 平台数据查询**：列表/搜索/统计接口缺少 tenant 过滤，一个企业可看到其他企业的全部数据
- **管理后台**：系统管理界面的数据查询未加 tenant 条件，管理员看到跨组织数据
- **报表与导出**：报表生成/数据导出 SQL 遗漏 tenant_id 条件，导出全量跨租户数据
- **缓存层穿透**：数据库查询有 tenant 过滤，但 Redis/Memcached 缓存 key 无 tenant 前缀，命中其他租户缓存
- **文件与附件**：文件上传/下载路径未包含 tenant 隔离，通过遍历文件名访问其他租户文件
- **消息队列/通知**：消息发送/通知推送未校验 tenant 归属，租户 A 的操作触发租户 B 的通知

## 审计方法

1. **识别多租户模型**：确认项目是否为多租户架构——搜索 `tenant` / `tenantId` / `organization` / `orgId` / `company` / `workspace` 等字段在数据库表和实体中的存在
2. **枚举所有数据查询**：对每个 Mapper/Repository/ORM 查询，检查是否包含 tenant_id 作为 WHERE 条件。特别关注 `selectAll` / `findAll` / `query.all()` / `db.Find()` 等全量查询
3. **追踪 tenant_id 来源**：对有 tenant_id 条件的查询，追踪其值来源。来自 session/SecurityContext/auth middleware 是安全的；来自 request param/cookie/header 是不安全的
4. **检查全局过滤机制**：确认项目是否有全局 tenant 注入机制（如 MyBatis Interceptor 自动追加 tenant_id、GORM Scope、SQLAlchemy event listener、中间件自动注入）。如有，确认所有查询是否都经过该机制，是否有绕过路径
5. **审查缓存与存储层**：检查 Redis/Memcached 缓存 key 是否包含 tenant 前缀；检查文件存储路径是否有 tenant 隔离；检查消息队列 topic/channel 是否按 tenant 分隔
