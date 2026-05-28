# 垂直越权：管理操作缺少角色校验（Missing Role Check）

## 漏洞模式

执行管理操作（账号停用/启用、系统配置修改、日志查看、批量操作）的端点，只校验了用户是否已登录（session 存在），但**未校验用户是否具有管理员角色**。任何普通用户只要已登录即可执行管理操作。

**核心原则：认证（已登录）≠ 授权（有权限）。每个涉及"管理语义"的端点必须独立检查是否存在角色/权限校验。**

## 通用代码示例

### Java Spring — 账号状态管理

```java
// ❌ 只检查了登录态，未检查 admin 角色
@PostMapping("/account/updateState")
public Result updateState(@RequestParam Long id,
                          @RequestParam Integer state,
                          HttpSession session) {
    if (session.getAttribute("userId") == null) {
        return Result.error("未登录");
    }
    // 任何已登录用户都可以停用/启用其他账号
    accountService.updateState(id, state);
    return Result.ok();
}

// ✅ 安全写法：同时校验角色
@PostMapping("/account/updateState")
@RequiresRoles("admin")  // 或 @PreAuthorize("hasRole('ADMIN')")
public Result updateState(@RequestParam Long id,
                          @RequestParam Integer state) {
    accountService.updateState(id, state);
    return Result.ok();
}
```

### Java — 日志查看接口

```java
// ❌ 系统日志应该只有管理员可查看
@GetMapping("/log/system")
public String systemLog(Model model) {
    // 无任何角色检查，只要能访问到此 URL 就能看日志
    model.addAttribute("logs", logService.getSystemLogs());
    return "log/system";
}

// ❌ Nginx 日志查看，同样无角色限制
@GetMapping("/log/nginx")
public String nginxLog(Model model) {
    model.addAttribute("logs", logService.getNginxLogs());
    return "log/nginx";
}
```

### PHP — 配置管理

```php
// ❌ 只检查登录，未检查角色
function updateSystemConfig() {
    if (!isset($_SESSION['user_id'])) {
        header("Location: /login");
        exit;
    }
    // 任何已登录用户都可修改系统配置
    $key = $_POST['config_key'];
    $value = $_POST['config_value'];
    $pdo->prepare("UPDATE system_config SET value = ? WHERE key = ?")
        ->execute([$value, $key]);
}

// ✅ 安全写法
function updateSystemConfigSafe() {
    if (!isset($_SESSION['user_id'])) {
        header("Location: /login"); exit;
    }
    if ($_SESSION['role'] !== 'admin') {
        http_response_code(403); exit;
    }
    // ... 执行配置更新
}
```

### Python Flask — 资源标签管理

```python
# ❌ 资源标签增删改——应为管理操作，但无角色校验
@app.route('/tags/delete/<int:tag_id>', methods=['POST'])
@login_required  # 只保证已登录
def delete_tag(tag_id):
    tag = Tag.query.get_or_404(tag_id)
    db.session.delete(tag)
    db.session.commit()
    return jsonify({"msg": "deleted"})

# ✅ 安全写法
@app.route('/tags/delete/<int:tag_id>', methods=['POST'])
@login_required
@admin_required  # 自定义装饰器检查角色
def delete_tag_safe(tag_id):
    tag = Tag.query.get_or_404(tag_id)
    db.session.delete(tag)
    db.session.commit()
    return jsonify({"msg": "deleted"})
```

## 识别信号

| 信号 | 说明 |
|------|------|
| URL 含 `/admin/` `/config/` `/log/` `/system/` 等管理语义路径 | 但 Controller 方法无 `@RequiresRoles` / `@PreAuthorize` / `isAdmin()` 校验 |
| 操作语义为"停用""启用""删除""配置修改" | 这些操作改变系统或他人状态，应仅管理员可执行 |
| Filter/Interceptor 的 exclude 列表 | 管理端点被排除在权限拦截之外，或拦截器只检查登录不检查角色 |
| 只检查 `session != null` 不检查 `session.role` | 典型的"认证不等于授权"——已登录 ≠ 有权限 |
| 日志/监控/审计类接口 | 常被视为"只读所以安全"，但系统日志可能包含敏感信息 |

## 常见出现场景

- **账号管理**：账号停用/启用/合停——改变其他用户的可用性，普通用户不应有此权限
- **系统配置**：全局参数修改、通知模板编辑、邮件/短信配置——影响全系统行为
- **日志查看**：系统日志、Nginx 日志、操作审计日志——可能泄露用户行为、IP、请求参数
- **资源标签/分类管理**：全局标签的创建、编辑、删除——影响所有用户的资源分类
- **批量操作**：批量导出、批量删除、批量状态变更——影响范围大，需要更高权限

## 审计方法

1. **枚举管理语义端点**：搜索含 `admin` / `config` / `log` / `system` / `manage` / `batch` / `import` / `export` 等关键词的 URL 和方法名
2. **枚举状态变更操作**：搜索 `updateState` / `disable` / `enable` / `delete` / `remove` / `modify` 等语义的方法
3. **逐端点检查角色校验**：确认每个管理端点是否有 `@RequiresRoles` / `@PreAuthorize` / `hasRole` / `isAdmin` / 自定义角色检查
4. **检查拦截器覆盖**：确认安全 Filter/Interceptor 的拦截路径是否覆盖了所有管理端点，exclude 列表是否过宽
5. **区分读操作和写操作**：即使是"只读"管理接口（如日志查看），如果数据敏感，同样需要角色限制
