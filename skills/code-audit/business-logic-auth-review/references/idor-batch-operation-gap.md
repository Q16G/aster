# IDOR：批量操作鉴权缺口（Batch Operation Authorization Gap）

## 漏洞模式

单条资源操作端点（查看/编辑/删除）有完整的 ownership 校验——从 session 获取操作者、在数据层加入 owner 过滤条件——但同一模块的批量/bulk 端点在处理 ID 列表时，**跳过了逐项归属验证**，直接对传入的所有 ID 执行操作。

**核心原则：批量操作 = N 次单项操作 × 相同的授权标准。如果单项操作需要验证 ownership，批量操作必须对列表中的每一项执行同等验证。** 开发者常见的错误思路是"单项已经有校验了，批量只是循环调用"——但批量端点往往为了性能直接用 `WHERE id IN (...)` 一次性操作，绕过了单项端点的校验逻辑。

更隐蔽的变体是"部分校验"：批量操作只校验列表中的第一项或随机一项，通过后对所有 ID 执行操作。攻击者只需在列表中混入一个自己有权限的 ID，即可对其余无权限的 ID 执行操作。

## 通用代码示例

### Java Spring + MyBatis

```java
// ✅ 单项删除 — 有 ownership 校验
@PostMapping("/order/delete")
public Result deleteOrder(@RequestParam Long id, HttpSession session) {
    String account = (String) session.getAttribute("account");
    Order order = orderService.selectByIdAndAccount(id, account);
    if (order == null) {
        return Result.error("无权限");
    }
    orderService.deleteById(id);
    return Result.ok();
}

// ❌ 批量删除 — 跳过 ownership 校验
@PostMapping("/order/batchDelete")
public Result batchDelete(@RequestBody List<Long> ids) {
    // 无 session 取值，无逐项归属验证
    orderService.deleteByIds(ids);
    return Result.ok();
}
```

```xml
<!-- MyBatis: 批量删除直接 IN 查询，无 account 条件 -->
<!-- ❌ 危险 -->
<delete id="deleteByIds">
    DELETE FROM orders WHERE id IN
    <foreach collection="ids" item="id" open="(" close=")" separator=",">
        #{id}
    </foreach>
</delete>

<!-- ✅ 安全：加入 account 条件 -->
<delete id="deleteByIdsAndAccount">
    DELETE FROM orders WHERE account = #{account} AND id IN
    <foreach collection="ids" item="id" open="(" close=")" separator=",">
        #{id}
    </foreach>
</delete>
```

### PHP

```php
// ✅ 单项查看 — 有 user_id 校验
function viewTicket($id) {
    $userId = $_SESSION['user_id'];
    $stmt = $pdo->prepare("SELECT * FROM tickets WHERE id = ? AND user_id = ?");
    $stmt->execute([$id, $userId]);
    $ticket = $stmt->fetch();
    if (!$ticket) { http_response_code(403); exit; }
    return $ticket;
}

// ❌ 批量导出 — 无 user_id 校验
function exportTickets() {
    $ids = json_decode($_POST['ids'], true);
    $placeholders = implode(',', array_fill(0, count($ids), '?'));
    $stmt = $pdo->prepare("SELECT * FROM tickets WHERE id IN ($placeholders)");
    $stmt->execute($ids);
    // 导出所有匹配记录，不校验是否属于当前用户
    header('Content-Type: text/csv');
    while ($row = $stmt->fetch()) {
        echo implode(',', $row) . "\n";
    }
}

// ✅ 安全写法：批量导出加入 user_id 过滤
function exportTicketsSafe() {
    $ids = json_decode($_POST['ids'], true);
    $userId = $_SESSION['user_id'];
    $placeholders = implode(',', array_fill(0, count($ids), '?'));
    $params = array_merge($ids, [$userId]);
    $stmt = $pdo->prepare(
        "SELECT * FROM tickets WHERE id IN ($placeholders) AND user_id = ?"
    );
    $stmt->execute($params);
    // ...
}
```

### Python Flask

```python
# ✅ 单项更新 — 有 owner 校验
@app.route('/resources/<int:rid>', methods=['PUT'])
@login_required
def update_resource(rid):
    resource = Resource.query.filter_by(
        id=rid, owner_id=current_user.id
    ).first_or_404()
    resource.name = request.json.get('name', resource.name)
    db.session.commit()
    return jsonify({"ok": True})

# ❌ 批量更新 — 跳过 owner 校验
@app.route('/resources/batch-update', methods=['PUT'])
@login_required
def batch_update_resources():
    items = request.get_json()  # [{"id": 1, "name": "new"}, {"id": 2, "name": "new2"}]
    for item in items:
        resource = Resource.query.get(item['id'])  # 无 owner_id 过滤
        if resource:
            resource.name = item.get('name', resource.name)
    db.session.commit()
    return jsonify({"ok": True})

# ✅ 安全写法：逐项校验 ownership
@app.route('/resources/batch-update', methods=['PUT'])
@login_required
def batch_update_resources_safe():
    items = request.get_json()
    for item in items:
        resource = Resource.query.filter_by(
            id=item['id'], owner_id=current_user.id
        ).first()
        if resource:
            resource.name = item.get('name', resource.name)
    db.session.commit()
    return jsonify({"ok": True})
```

### Go Gin

```go
// ✅ 单项删除 — 有 ownership 校验
func DeleteDevice(c *gin.Context) {
    deviceID := c.Param("id")
    userID := c.GetString("userID") // auth middleware 注入
    device, _ := service.GetDeviceByIDAndOwner(deviceID, userID)
    if device == nil {
        c.JSON(403, gin.H{"error": "forbidden"})
        return
    }
    service.DeleteDevice(deviceID)
    c.JSON(200, gin.H{"ok": true})
}

// ❌ 批量删除 — 无逐项 ownership 校验
func BatchDeleteDevices(c *gin.Context) {
    var req struct {
        IDs []string `json:"ids"`
    }
    c.BindJSON(&req)
    for _, id := range req.IDs {
        service.DeleteDevice(id) // 直接删除，无 ownership 检查
    }
    c.JSON(200, gin.H{"ok": true})
}

// ✅ 安全写法 1：逐项校验
func BatchDeleteDevicesSafe(c *gin.Context) {
    var req struct {
        IDs []string `json:"ids"`
    }
    c.BindJSON(&req)
    userID := c.GetString("userID")
    for _, id := range req.IDs {
        device, _ := service.GetDeviceByIDAndOwner(id, userID)
        if device == nil {
            c.JSON(403, gin.H{"error": "forbidden", "id": id})
            return
        }
        service.DeleteDevice(id)
    }
    c.JSON(200, gin.H{"ok": true})
}

// ✅ 安全写法 2：数据层批量过滤
func BatchDeleteDevicesSafe2(c *gin.Context) {
    var req struct {
        IDs []string `json:"ids"`
    }
    c.BindJSON(&req)
    userID := c.GetString("userID")
    // 在数据层一次性加入 owner 过滤
    affected := service.DeleteDevicesByIDsAndOwner(req.IDs, userID)
    c.JSON(200, gin.H{"deleted": affected})
}
```

## 识别信号

| 信号 | 说明 |
|------|------|
| 同一 Controller 存在单项 + 批量两套端点 | 如 `DELETE /order/{id}` 与 `POST /order/batchDelete`，对比两者的鉴权粒度 |
| 批量端点方法签名只有 ID 列表参数 | 如 `@RequestBody List<Long> ids`，无 session 取值，无 operator 参数 |
| 批量 SQL 使用 `WHERE id IN (...)` 无 owner 条件 | MyBatis `<foreach>` / ORM `query.filter(Model.id.in_(...))` 只有主键，无归属过滤 |
| 批量端点直接循环调用 `deleteById` / `updateById` | 调用的是无 ownership 参数的通用方法，而非单项端点的完整逻辑 |
| export / download / archive 类批量接口 | 这些接口常被视为"辅助功能"而遗漏权限校验 |
| 批量操作只校验第一项或最后一项 | 部分校验的变体，攻击者混入一个合法 ID 即可绕过 |

## 常见出现场景

- **批量删除**：订单/工单/设备的批量删除接口，勾选多个 ID 一次性删除，但不校验每项是否属于当前用户
- **批量导出**：选择多条记录导出 CSV/Excel，SQL 只用 `WHERE id IN (...)` 无 owner 条件，可导出他人数据
- **批量状态变更**：批量审批/驳回/启用/停用，循环执行但不逐项校验操作权限
- **批量转移/分配**：将多个资源从一个归属者转移到另一个，但不校验当前用户是否有权转移每一项
- **批量下载/打包**：选择多个文件/附件打包下载，不校验每个文件的访问权限
- **购物车/收藏夹批量操作**：批量移除/移动收藏项，但使用的是不带 user_id 过滤的通用删除方法

## 审计方法

1. **枚举批量端点**：搜索 `batch` / `bulk` / `batchDelete` / `batchUpdate` / `export` / `import` / `List<Long> ids` / `[]string` 等批量操作特征
2. **对比单项端点**：找到同一模块的单项操作端点，对比两者的鉴权差异——单项有 session 取值 + ownership 校验，批量是否也有
3. **检查数据层查询**：批量操作的 SQL/ORM 查询是否包含 owner/account/tenant 条件，还是只有 `WHERE id IN (...)`
4. **追踪批量循环体**：如果批量端点是循环调用单项方法，确认调用的是有 ownership 参数的方法（如 `deleteByIdAndAccount`）还是通用方法（如 `deleteById`）
5. **验证全量校验**：如果批量端点有校验逻辑，确认是对列表中的**每一项**都校验，还是只校验第一项/随机一项。部分校验 = 无校验
