# 业务逻辑授权审计 — 跨框架代码变体（详表）

> 主 SKILL.md §6 的扩展版——主流框架的"安全形态 vs 危险形态"对照、ORM 自动注入旁路、业务字段被信任的危险点。读完后再回主 §6 / §8 选择审计路径。

## 一、按框架对照表

| 框架 | 安全形态（含校验） | 危险形态（缺校验） | 主要漏点 |
|---|---|---|---|
| **Spring Security** | `@PreAuthorize("@perm.canAccess(#id, principal)")` + 注解全覆盖；`SecurityContextHolder` 取 currentUser | 端点缺注解；`permitAll()` 过宽；SpEL 表达式被绕过 | 注解漏挂、SpEL 绕过、`SecurityFilterChain` 遗漏 |
| **Spring + MyBatis** | Mapper SQL 含 `AND owner_id = #{currentUserId}`；`@Param` 显式传 currentUser | 仅 `WHERE id = #{id}`；批量 `<foreach>` 内无 owner | XML 模板缺 owner 条件 |
| **Shiro** | `Subject.checkPermission("order:edit:" + id)` / `@RequiresPermissions("order:edit")` | 仅 `Subject.isAuthenticated()`；`anon` filter 过宽；URL 路径绕过 | remember-me 误当 authenticated、anon 过宽 |
| **Sa-Token** | `StpUtil.checkPermission("order:edit")` / `@SaCheckPermission` | 仅 `StpUtil.isLogin()`；注解缺失 | 注解漏挂、checkPermission 漏调 |
| **Django auth** | `UserPassesTestMixin` + `test_func` 校验归属；`@permission_required('app.edit')`；`get_queryset` filter by `request.user` | 普通 ListView 无 Mixin；`get_object` 直接 `pk` 不 filter user | CBV 缺 Mixin、`get_queryset` 全表 |
| **Django REST Framework** | `permission_classes = [IsOwnerOrReadOnly]` + 自定义 `has_object_permission`；`get_queryset` 按 user 过滤 | 仅 `IsAuthenticated`；queryset 全表返回 | `has_object_permission` 漏写 |
| **Laravel Policy** | `$this->authorize('update', $model)` / Gate `Gate::allows('edit-post', $post)` | 直接 `Model::find($id)->update(...)` 缺 authorize | Policy 注册了但 Controller 没调用 |
| **Express + Casbin** | `enforcer.enforce(sub, obj, act)` 在 middleware 或 handler 内 | 仅检查 JWT 有效 / 登录态，未做对象级判定 | 仅 route 级 ACL，对象级缺失 |
| **Express + ACL middleware** | `app.use('/admin', requireRole('admin'))` 路由级 + handler 内对象级双层 | 仅路由级 ACL，handler 内可被同角色越权读他人 | 同角色横向越权 |
| **NestJS** | `@UseGuards(RolesGuard)` + `@Roles('admin')` + custom `OwnerGuard` | 仅 `@UseGuards(AuthGuard)`；缺角色 / 归属 Guard | 自定义 OwnerGuard 缺失 |
| **GraphQL（graphql-shield）** | `permissions.compose({Query: {getUser: isOwner}})` | resolver 内裸 `Model.findById(args.id)` | resolver 直接消费 args.id |
| **Flask + flask-login** | `@login_required` + 自定义 `current_user.id == resource.owner_id` 判定 | 仅 `@login_required` 缺归属判定 | 仅登录态 |
| **FastAPI + Depends** | `Depends(get_current_user)` + 自定义 `Depends(require_owner)` | 仅 `Depends(get_current_user)` 缺归属 | 仅获取 currentUser 不做对象级 |

## 二、ORM 自动注入旁路（"看不见的校验"）

这些机制让 SQL / ORM 查询自动加 WHERE 条件，Controller / Service 表面看不见——审计时**必须独立读 Model / Manager / hook 实现**。

| ORM | 注入机制 | 识别位置 |
|---|---|---|
| **TypeORM** | `@Scope` 装饰器 / `addWhere` Subscriber | Model 定义、Subscriber 类 |
| **Sequelize** | `defaultScope` / `beforeFind` hook | Model 定义、`addHook` 调用 |
| **Django ORM** | 自定义 Manager `get_queryset()` 重写 | `models.py` Manager 子类 |
| **GORM** | Scope 函数 `func ForUser(user *User) func(*gorm.DB) *gorm.DB` | scope 文件、`db.Scopes(ForUser(u))` 调用链 |
| **SQLAlchemy** | session event listener / `with_loader_criteria` | session 配置、event 注册 |
| **Spring Data JPA** | `@Where(clause="owner_id = ?#{principal.id}")` / `@FilterDef` | Entity 类注解 |

**审计要点**：

- ORM scope / Manager 不是"完全免疫"——必须确认**所有查询路径**都触发，包括 raw query、批量删除（`Model.objects.bulk_delete`）、跨表 JOIN
- 任一路径绕过 scope → 标 `static-unknown` 或视为高危候选

## 三、业务字段被信任的危险点

任何框架适用——**source 不仅仅是 ID，业务字段也是 source**。

### 1. 实体自动绑定（mass assignment）

`@RequestBody UserDTO` / `BindJSON(&user)` / `request.form → model` 把请求体直接映射到实体类：

```java
// 危险：UserEntity 含 role / isAdmin / status 字段
@PostMapping("/profile")
public void updateProfile(@RequestBody UserEntity user) {
    userRepo.save(user); // 攻击者构造请求带 role=admin 直接提权
}
```

**修复**：

- 用 DTO 白名单（`UserUpdateDTO` 只含 name / email / phone）
- `@JsonIgnore` 注解敏感字段
- Laravel：`$fillable` / `$guarded` 配置
- Django REST Framework：Serializer `fields` 显式列举

### 2. Cookie / Header 注入身份

```java
// 危险：从 Cookie 取 userId
String userId = request.getCookies()[0].getValue();
SecurityContextHolder.getContext().setAuthentication(new UserToken(userId));
```

攻击者直接修改 Cookie 值伪造任意身份。

**修复**：

- currentUser 上下文唯一来自 JWT 签名验证后的 claims 或 server-side session lookup
- Cookie / Header 中的 "身份" 字段必须经过签名 / 加密 / 服务端二次查询验证

### 3. 金额 / 数量 / 折扣率从前端取值

```java
// 危险：信任前端传 amount
@PostMapping("/order/create")
public void createOrder(@RequestBody OrderDTO dto) {
    Order order = new Order();
    order.setAmount(dto.getAmount()); // 攻击者传 amount=0.01
    order.setQuantity(dto.getQuantity()); // 攻击者传负数 / 极大值
    order.setDiscount(dto.getDiscount()); // 攻击者传 discount=1.0（100% 折扣）
    orderRepo.save(order);
}
```

**修复**：金额从 `goods.price × quantity` 重算；折扣从配置表 / 优惠券表查；数量加范围检查。

### 4. 状态字段直接 setter

```java
// 危险：状态字段从请求取值
public void updateOrder(@RequestBody OrderDTO dto) {
    Order order = orderRepo.findById(dto.getId()).get();
    order.setStatus(dto.getStatus()); // 攻击者传 status=COMPLETED 跳过支付
    orderRepo.save(order);
}
```

**修复**：状态转换走专门方法（`shipOrder` / `completeOrder`）+ 前置状态校验；状态字段不允许直接 setter 暴露。

## 四、ORM 通用危险点

任何 ORM 都适用：

- **字段名 / 表名 / 排序方向**：ORM 一般只参数化「值」，不参数化「字段名」/「表名」/「排序方向」——`ORDER BY ${userInput}` 是越权 + SQL 注入双重风险位
- **Raw / Native 通道**：所有 ORM 都有 raw 通道（`raw()` / `query()` / `Raw()` / `nativeQuery()`），是权限边界 + SQL 注入的常见漏点
- **批量删除 / 批量更新**：`Model.objects.filter(...).delete()` / `bulk_update` 容易绕过 Manager scope，必须独立验证
