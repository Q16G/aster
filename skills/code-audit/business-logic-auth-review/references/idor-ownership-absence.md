# IDOR：Operator 缺失型（Ownership Absence）

## 漏洞模式

端点方法签名中**只有资源 ID 参数，没有任何 operator/owner 参数**，也没有从 session/SecurityContext 中获取当前用户身份来做归属校验。任何已登录用户传入任意资源 ID 即可操作他人资源。

这是 IDOR 最常见也最危险的形态。与 "operator 存在但在传递过程中丢失"（ownership-drop）不同，此模式下 **operator 从一开始就不存在**。

## 通用代码示例

### Java Spring + MyBatis

```java
// ❌ 危险：只接受资源 ID，无 operator
@GetMapping("/order/view")
public Result viewOrder(@RequestParam Integer id) {
    return Result.ok(orderService.selectById(id));
}

// ❌ 危险：entity 中有 id 但无 operator 约束
@PostMapping("/order/edit")
public Result editOrder(@RequestBody OrderEntity entity) {
    orderService.updateById(entity);
    return Result.ok();
}

// ✅ 安全：从 session 取 operator 并传递到数据层
@GetMapping("/order/view")
public Result viewOrder(@RequestParam Integer id, HttpSession session) {
    String account = (String) session.getAttribute("account");
    Order order = orderService.selectByIdAndAccount(id, account);
    if (order == null) return Result.error("无权限");
    return Result.ok(order);
}
```

### PHP

```php
// ❌ 危险：只用 GET 参数中的 id
function viewTicket() {
    $id = $_GET['id'];
    $stmt = $pdo->prepare("SELECT * FROM tickets WHERE id = ?");
    $stmt->execute([$id]);
    return $stmt->fetch();
}

// ✅ 安全：加入 session 中的 user_id 约束
function viewTicket() {
    $id = $_GET['id'];
    $userId = $_SESSION['user_id'];
    $stmt = $pdo->prepare("SELECT * FROM tickets WHERE id = ? AND user_id = ?");
    $stmt->execute([$id, $userId]);
    return $stmt->fetch();
}
```

### Python Flask

```python
# ❌ 危险：直接按 ID 查询，无 owner 校验
@app.route('/records/<int:record_id>')
@login_required
def view_record(record_id):
    record = Record.query.get_or_404(record_id)
    return jsonify(record)

# ✅ 安全：校验资源归属
@app.route('/records/<int:record_id>')
@login_required
def view_record(record_id):
    record = Record.query.filter_by(id=record_id, owner_id=current_user.id).first_or_404()
    return jsonify(record)
```

### Go Gin

```go
// ❌ 危险：只从 URL 取资源 ID
func GetDevice(c *gin.Context) {
    id := c.Param("id")
    device, _ := service.GetDeviceByID(id)
    c.JSON(200, device)
}

// ✅ 安全：从 context 取当前用户并校验归属
func GetDevice(c *gin.Context) {
    id := c.Param("id")
    userID := c.GetString("userID") // 从 auth middleware 设置
    device, _ := service.GetDeviceByIDAndOwner(id, userID)
    if device == nil {
        c.JSON(403, gin.H{"error": "forbidden"})
        return
    }
    c.JSON(200, device)
}
```

## 识别信号

| 信号 | 说明 |
|------|------|
| 方法签名只有 `id` / `@RequestParam Integer id` / `@PathVariable Long id` | 无 operator 参数，也无 session 取值 |
| Mapper/SQL 只有 `WHERE id = #{id}` | 无 account/userId/tenantId 条件 |
| Service 层 `selectById` / `findById` / `get_or_404` | 通用的按主键查询，不含归属过滤 |
| Controller 方法体内无 `session.getAttribute` / `SecurityContext` / `current_user` | 完全不获取当前用户身份 |
| `@login_required` / session 非空检查存在，但无后续 ownership 比对 | 认证 ≠ 授权，已登录不等于有权限 |

## 审计方法

1. **枚举含资源 ID 参数的端点**：`rg "@RequestParam.*[Ii]d|@PathVariable.*[Ii]d|request\.args.*id|c\.Param.*id|c\.Query.*id|\$_GET\['id"` 找所有接受 ID 参数的端点
2. **检查方法体内是否获取当前用户**：在每个端点方法中搜索 `session.getAttribute` / `SecurityContext` / `current_user` / `c.GetString("userId")` / `$_SESSION['user_id']` — 如果不存在，该端点高危
3. **检查数据层查询**：追踪到 mapper XML / ORM 查询，确认 WHERE 条件是否只有 `id = ?` 而无 owner/account/userId 约束
4. **区分 LIST vs DETAIL**：同一资源的 LIST 接口有 account 过滤不能推断 DETAIL/EDIT/DELETE 接口也安全——必须逐端点独立验证

## 常见出现场景

- **工单/订单详情**：列表页按用户过滤，但详情页直接按 ID 查询
- **密码/凭据查看**：密码管理页面按账号过滤，但编辑/查看接口直接按 ID 返回
- **隐私数据**：隐私工单、医疗记录、财务信息等，LIST 有权限控制但 DETAIL 没有
- **设备/资产管理**：设备列表按租户过滤，但设备详情/编辑 API 只用设备 ID
- **文件/附件访问**：文件列表按用户过滤，但下载/预览接口只需文件 ID
