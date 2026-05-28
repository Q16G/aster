# 授权独立验证原则：不能从一个接口的安全性推断其他接口

## 漏洞模式

同一模块/Controller 中的多个端点，其授权检查的力度可能完全不同。**某个端点有权限校验，不能推断同模块的其他端点也有同等校验。**

这不是一个特定的漏洞类型，而是一条审计方法论原则。违反它会导致审计结论出现假阴性——"检查了接口 A 有权限控制 → 判定整个模块安全"，而实际上接口 B/C/D 可能完全没有权限控制。

**核心原则：每个端点的授权必须独立验证，不可从相邻端点推断。**

## 常见推断谬误

| 推断方式 | 为什么是错的 |
|---------|------------|
| LIST 有 account 过滤 → VIEW/EDIT 也安全 | VIEW/EDIT 可能走不同的 mapper 查询（`selectById` 只有主键，无 account 条件） |
| WRITE 有 ownership 检查 → READ 也安全 | 开发者常认为"只读无害"，只在写操作加归属校验，读操作直接按 ID 返回 |
| 主接口有 @PreAuthorize → 导出/预览/下载也安全 | 辅助接口容易被遗漏，尤其是后期新增的 export/preview/download |
| 接口 A 有角色校验 → 同 Controller 的接口 B 也有 | 注解是逐方法加的，漏加一个就漏一个 |
| Web 页面有权限 → 对应 API 也有权限 | 前端路由拦截不等于后端 API 鉴权 |
| 单项操作有归属校验 → 批量操作也有 | 批量端点可能只循环调 `deleteById` 而不做逐项 ownership 检查 |

## 通用代码示例

### 示例 1：LIST 有过滤，VIEW 没有（Java）

```java
@RestController
@RequestMapping("/resource")
public class ResourceController {

    // ✅ 有权限控制
    @GetMapping("/list")
    public Result list(HttpSession session) {
        String account = (String) session.getAttribute("account");
        return Result.ok(resourceService.selectByAccount(account));
    }

    // ❌ 同 Controller，但无权限控制
    @GetMapping("/view")
    public Result view(@RequestParam Integer id) {
        return Result.ok(resourceService.selectById(id));
    }
}
```

### 示例 2：WRITE 有 ownership，READ 没有（Python）

```python
# ✅ WRITE — 检查了归属
@app.route('/tickets/<int:tid>/close', methods=['POST'])
@login_required
def close_ticket(tid):
    ticket = Ticket.query.filter_by(id=tid, owner_id=current_user.id).first_or_404()
    ticket.status = 'closed'
    db.session.commit()
    return jsonify({"ok": True})

# ❌ READ — 同模块，但未检查归属
@app.route('/tickets/<int:tid>')
@login_required
def view_ticket(tid):
    ticket = Ticket.query.get_or_404(tid)
    return jsonify(ticket.to_dict())
```

### 示例 3：主接口有鉴权，辅助接口没有（PHP）

```php
// ✅ 主接口 — 有角色校验
function manageUsers() {
    if ($_SESSION['role'] !== 'admin') {
        http_response_code(403); exit;
    }
    // ... 用户管理逻辑
}

// ❌ 导出接口 — 后期新增，遗漏了角色校验
function exportUsers() {
    // 无角色检查，任何登录用户可导出全部用户数据
    $stmt = $pdo->query("SELECT * FROM users");
    header('Content-Type: text/csv');
    // ... 输出 CSV
}
```

### 示例 4：接口 A 有注解，接口 B 漏加（Java Spring）

```java
@RestController
@RequestMapping("/admin/config")
public class ConfigController {

    @PreAuthorize("hasRole('ADMIN')")  // ✅ 有注解
    @PostMapping("/update")
    public Result updateConfig(@RequestBody ConfigDTO dto) {
        return Result.ok(configService.update(dto));
    }

    // ❌ 同 Controller，漏加 @PreAuthorize
    @GetMapping("/export")
    public ResponseEntity<byte[]> exportConfig() {
        byte[] csv = configService.exportAll();
        return ResponseEntity.ok(csv);
    }
}
```

### 示例 5：单项有校验，批量没有（Go）

```go
// ✅ 单项删除 — 校验归属
func DeleteOrder(c *gin.Context) {
    orderID := c.Param("id")
    userID := c.GetString("userID")
    order, _ := service.GetOrderByIDAndUser(orderID, userID)
    if order == nil {
        c.JSON(403, gin.H{"error": "forbidden"})
        return
    }
    service.DeleteOrder(orderID)
    c.JSON(200, gin.H{"ok": true})
}

// ❌ 批量删除 — 没有逐项校验
func BatchDeleteOrders(c *gin.Context) {
    var ids []string
    c.BindJSON(&ids)
    for _, id := range ids {
        service.DeleteOrder(id) // 无 ownership 检查
    }
    c.JSON(200, gin.H{"ok": true})
}
```

## 识别信号

| 信号 | 说明 |
|------|------|
| 同 Controller 中不同方法的参数签名差异大 | 一个从 session 取 account，另一个只接受 id —— 授权粒度不一致 |
| Mapper/Repository 中存在 `selectById` 和 `selectByParams` 两套查询 | 前者只有主键，后者有 account/tenant 条件 |
| 注解（@PreAuthorize/@RequiresRoles）不是加在类级别而是方法级别 | 方法级注解容易漏加，每个方法都要检查 |
| 有 export/download/preview 等辅助端点 | 这些端点常在主功能之后添加，容易遗漏鉴权 |
| 存在 batch/bulk 端点 | 批量操作可能跳过单项操作中的逐条校验 |
| 同一资源有多个访问路径（页面 + API / v1 + v2） | 不同路径的鉴权可能不一致 |

## 常见出现场景

- **CRUD 模块**：LIST 有过滤、CREATE 有校验，但 VIEW/EDIT/DELETE 直接按 ID 操作
- **管理后台**：主功能有角色注解，但导出/统计/预览接口遗漏
- **工单/订单系统**：提交和处理有归属检查，但查看详情没有
- **文件管理**：上传有权限控制，但下载/预览接口直接返回
- **批量操作**：单条删除/修改有 ownership 检查，批量接口跳过
- **API 多版本**：v1 有鉴权，v2 重构时遗漏

## 审计方法

1. **逐端点独立检查**：对每个 Controller，列出所有方法，逐个检查授权——不能因为检查了一个就跳过其他
2. **同模块对比**：同一 Controller/模块内，比较不同方法的授权粒度是否一致。不一致时逐个标记
3. **辅助接口排查**：对每个主功能，搜索是否存在 export/download/preview/batch/bulk 等辅助端点，这些是高概率遗漏点
4. **注解覆盖确认**：如果项目用注解做鉴权（@PreAuthorize / @RequiresRoles），检查是类级别还是方法级别。方法级别时必须逐方法确认
5. **不做推断、只看事实**：审计结论中，每个端点的安全判定必须基于该端点自身的代码，不能写"同模块其他接口有校验，因此本接口安全"
