---
name: business-logic-auth-review
description: >-
  业务逻辑越权与权限边界白盒审计——追用户可控 resource_id / owner_id / batch_ids / amount /
  state 等字段是否经归属判定、角色判定、状态前提与数量上限，覆盖水平 IDOR / 垂直越权 / 状态机
  绕过 / 批量越权 / 金额数量篡改 / 多租户穿透。
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
argument-hint: "[target_path] [--focus idor|vertical|state-machine|batch|amount]"
arguments:
  - target_path
  - focus
---

# 业务逻辑越权 / 权限边界 / IDOR 白盒审计

## 1. 触发线索 / 适用信号

按 **代码 pattern + 项目结构 + 依赖 / 注解**三维识别本能力命中场景。

**代码 pattern 维度**（grep 命中模式）：

- Controller 参数中接收用户可控的资源标识：`@PathVariable Long id` / `@RequestParam Long resourceId` / `@RequestParam Long targetUserId` / 路径中 `/users/{id}` / `/orders/{orderId}` / 批量入口 `List<Long> ids`
- Service 层做归属判定：`if (currentUser.getId() != resource.getOwnerId()) throw new AccessDeniedException()` / `assert resource.user_id == request.user.id`
- Mapper / Repository 层按用户过滤的 WHERE：`WHERE id = #{id} AND owner_id = #{currentUserId}`（安全形态）vs `WHERE id = #{id}`（候选）
- 状态机字段与转换：`if (order.status == PAID) order.status = SHIPPED` / `enum OrderStatus { CREATED, PAID, SHIPPED, COMPLETED }`
- 业务金额 / 数量从请求取值：`order.setAmount(req.getAmount())` / `quantity = request.json["quantity"]`
- 角色分层：admin / operator / owner / guest 等多角色端点共存

**文件位置 / 命名约定维度**：

- Spring：`*Controller.java` 接收资源 ID 但 Service 无归属判定；`*Mapper.xml` 含 `WHERE id = #{id}` 缺 `AND owner_id`
- Django：`views.py` 用 generic ListView / DetailView 但缺 `LoginRequiredMixin` / `UserPassesTestMixin`
- Express：`routes/*.js` 接收 `req.params.id` 但 controller 直接 `Model.findByPk(id)`
- Laravel：`app/Http/Controllers/` 直接 `Model::find($id)->delete()`，缺 `$this->authorize('delete', $model)`
- GraphQL：resolver 函数签名 `(parent, args, context)` 但未消费 `context.user` 做角色校验

**依赖 / 注解维度**：

- `pom.xml` 含 `spring-security` / `shiro-spring` / `sa-token`，识别注解使用率
- `requirements.txt` 含 `django.contrib.auth` / `django-guardian`
- `composer.json` 含 `laravel/framework`（Policy / Gate）
- `package.json` 含 `casbin` / `accesscontrol` / `passport`
- 注解出现：`@PreAuthorize("@perm.canAccess(#id)")` / `@RequiresPermissions` / `@SaCheckPermission` / `@permission_required` 但**端点覆盖不全**

业务命名（`getUserOrder` / `deleteUser` / `updateProfile`）仅作粗筛——sink 语义相同（按 ID 操作他人资源 / 角色断言 / 状态转换）就是审计候选。

---

## 2. 造成原因（共享章节）

本类漏洞的核心是「source = 用户可控的身份 / 资源 / 业务字段」流到「sink = CRUD / 状态转换 / 金额计算 / 批量操作」时**缺少必要的服务端校验**，攻击者直接构造请求改写 source 即可越权或篡改业务逻辑。常见成因有五类：

1. **IDOR（水平越权）**：用户提交资源 ID 但服务端未校验该用户对该资源的归属，直接 CRUD 他人资源（`UPDATE orders SET ... WHERE id = ?` 缺 `AND owner_id = ?`）。
2. **垂直越权**：普通用户调用管理端接口，端点无角色断言或仅依赖 UI 隐藏（前端按钮藏起来不等于后端拒绝）。
3. **状态机绕过**：业务状态字段（status / state / phase）转换缺少前置状态判定，直接调中间状态接口跳过前置步骤（未支付直接发货 / 未审核直接上架）。
4. **批量操作滥用**：单条端点权限校验生效，但批量入口（`List<Long> ids`）循环内未逐条校验，攻击者把他人 ID 混入数组。
5. **业务字段篡改**：金额 / 数量 / 折扣率 / 库存等业务字段从前端取值后直接进入计算 / 持久化，服务端未按 source-of-truth（数据库 / 配置 / 商品表）重新计算。

更深一层的根因是**信任边界设置错误**——前端 / Cookie / 请求参数被信任为身份 / 业务事实来源，而服务端 session / SecurityContext / 数据库才是真正的 source-of-truth。

---

## 3. 领域 source-sink 数据流模型

**代码层 source 集合**（按来源分类）：

- 资源标识：`@PathVariable Long id` / `@RequestParam Long resourceId` / `req.params.id` / `request.GET['id']` / `$_GET['id']`
- 目标用户：`@RequestParam Long targetUserId` / `req.body.userId` / `request.json['user_id']`
- 批量 ID 数组：`List<Long> ids` / `[]int64` / `req.body.ids: number[]` / `request.POST.getlist('ids')`
- 业务字段（受信任度低）：`amount` / `quantity` / `discount` / `state` / `status` / `role` / `tenant_id`
- 客户端身份旁路（伪 source）：`Cookie["userId"]` / `req.headers['x-user-id']` / `request param "currentUser"`——任何来自客户端的"身份"声明都属潜在 source

**代码层 sink 集合**（按操作类型分类）：

- CRUD 操作：`Mapper.updateById` / `Mapper.deleteById` / `repo.save(entity)` / `Model.objects.filter(id=x).delete()` / `DB::table('x')->where('id',$id)->update(...)`
- 状态转换调用：`order.setStatus(SHIPPED)` / `workflow.transition(state)` / `entity.status = 'approved'`
- 金额 / 数量 / 折扣计算：`total = price * quantity` / `order.amount = req.amount` / `cart.discount = req.discount`
- 批量操作循环体：`for (Long id : ids) mapper.deleteById(id)` / `ids.forEach(id => Model.delete(id))`
- 跨租户 / 跨用户读取：`SELECT * FROM orders WHERE id = ?` 不带 `tenant_id` / `owner_id` 条件

**数据流追踪规则**（本能力专属——多 source 多 sink 交叉）：

本类漏洞 source 与 sink 不是 1:1 配对，必须**交叉验证**以下四种约束至少其一在数据流中生效：

- **归属判定**：`current_user.id == resource.owner_id` 或 SQL 内 `AND owner_id = #{currentUserId}` 出现
- **角色判定**：`@PreAuthorize("hasRole('ADMIN')")` / `if (user.role != 'admin')` / `subject.checkRole('admin')` 等显式角色断言
- **状态前提**：状态转换前判断当前状态（`if (order.status == PAID)` 才允许转 SHIPPED）
- **数量 / 范围上限**：`if (quantity > limit) reject` / 金额从服务端 source-of-truth 重新计算后比对

跨函数 / 跨文件追踪边界：

- Controller → Service → Mapper / Repository 调用链都要追，**中间任何一层做了对应约束都算安全**
- AOP / Interceptor / Middleware 改写 request 上下文的情形：直接 grep 看不见，必须独立读拦截器实现（见 §11）
- 跨服务 RPC 透传 currentUser 上下文：边界处停手，标 `static-unknown`

---

## 4. 常见类型（共享章节）

| 类型 | 静态识别特征 | 白盒识别难点 |
|---|---|---|
| **水平越权 IDOR** | 同角色 / 同租户用户互读 / 互改资源；Mapper SQL 缺 `AND owner_id = ?` | AOP 拦截器统一加 owner 条件（看不到）/ ORM scope 自动加（typeorm scope / Django manager `for_user`） |
| **垂直越权** | 普通用户访问管理端接口；admin 端点缺角色断言或仅靠 `session != null` | admin / 普通端共用 Service 方法，单看 Service 看不出端点角色差异 |
| **状态机绕过** | 状态字段直接被 setter 改写 / 转换前无前置状态判定 | 状态枚举多分支时逻辑分散；用 Spring StateMachine 时跨配置文件 |
| **批量越权** | `List<ID>` 入参，循环体内未逐条校验归属 | 单项端点安全 → 误以为批量也安全；批量接口可能复用单项校验但漏点不同 |
| **金额 / 数量篡改** | `setAmount(req.getAmount())` / 折扣率 / 库存从请求取值后直接计算 | 前端"展示价"传后端被信任；负数 / 整数溢出 / 浮点精度边界 |
| **数据归属字段被信任前端** | 实体绑定（`@RequestBody` / `BindJSON`）把 `ownerId` / `tenantId` / `role` 字段直接覆盖 | 自动绑定无 DTO 白名单，看 Controller 表面正常 |
| **操作者 vs 操作对象混淆** | 同一字段名（`userId`）在不同上下文（操作者 / 被操作对象）含义不同 | Controller 用 `userId` 当操作者，Service 把它当被操作对象 |
| **跨租户数据穿透** | 多租户系统查询缺 `tenant_id` 约束 / `tenant_id` 从 request 取值 | tenant 上下文从 SecurityContext 拿（看不到 SQL 显式拼接） |

---

## 5. 入口点定位

按项目结构定位 source / sink 候选位置——**Controller 找 source / Service 找校验层 / Mapper / Repository 找 sink**。

> 下列框架 / 项目类型仅作类似项目示例，不限于此；以目标实际栈为准。

### Java / Spring 项目

- `*Controller.java`：参数中接收资源 ID（`@PathVariable` / `@RequestParam`）；注解层 `@PreAuthorize` / `@Secured` 是否齐全；`@RequestBody` 接收实体时看 DTO 是否含 `ownerId` / `role` 等敏感字段
- `*Service.java`：是否有 `findByXxxAndCurrentUser` 模式（安全）vs `findByXxx`（候选）；状态转换方法是否检查前置状态
- `*Mapper.xml` / `@Mapper`：`WHERE id = #{id}` 缺 `AND owner_id = #{currentUserId}`；批量 `<foreach>` 内是否含归属约束
- `pom.xml`：识别 Spring Security / Shiro / Sa-Token 引入，确认是否全局生效

### Python / Django 项目

- `views.py`：generic CBV（ListView / DetailView）缺 `LoginRequiredMixin` / `UserPassesTestMixin` / `PermissionRequiredMixin`；`get_queryset` 是否按 `request.user` 过滤
- `models.py` Manager：自定义 `for_user(user)` Manager 方法（安全）vs 直接 `objects.all()`
- DRF：`viewsets.ModelViewSet` 的 `permission_classes` / `get_queryset` 是否覆盖归属
- `settings.py`：`AUTHENTICATION_BACKENDS` / `DEFAULT_PERMISSION_CLASSES` 是否设置

### Node.js / Express 项目

- `routes/*.js` / `controllers/*.js`：`req.params.id` 直接 `Model.findByPk(id)` 缺归属过滤
- middleware 层：`requireAuth` / `requireRole` 中间件挂载是否覆盖目标路由
- ORM（Sequelize / TypeORM）：scope / hooks 是否注入 `where: {ownerId: req.user.id}`
- `package.json`：识别 passport / casbin / accesscontrol 引入

### PHP / Laravel 项目

- `app/Http/Controllers/`：`Model::find($id)->delete()` 缺 `$this->authorize('delete', $model)`
- `app/Policies/`：Policy 类实现 + Controller / route 是否真调用 `authorize` / `can`
- route 中间件：`auth` / `can:xxx` 中间件挂载

### Go / Gin 项目

- `router.go` / `main.go`：找 handler 函数，追到 service / repository
- handler：`c.Param("id")` / `c.Query("user_id")` 直接传到 DAO 缺 currentUser 拼接
- 自定义 middleware：JWT 解析 currentUser 后是否注入 context，handler 是否消费

### GraphQL

- resolver：`(parent, args, context) => Model.findById(args.id)` 是否消费 `context.user` 做角色 / 归属校验
- schema directive：`@auth(requires: ADMIN)` / `@hasPermission` 注解是否覆盖

### 通用建议

- 先把所有"按 ID 操作他人资源"的端点列全（LIST / VIEW / EDIT / DELETE 各独立看）
- 用 `sast-scan` 输出的 `business-logic-auth-review` 候选清单加速定位（如有）
- 闭源依赖 / 跨服务 RPC 见 §11 静态分析边界

---

## 6. 跨框架代码变体

主流框架的"安全形态 vs 危险形态"对照（精选）：

| 框架 | 安全形态（含校验） | 危险形态（缺校验） |
|---|---|---|
| **Spring Security** | `@PreAuthorize("@perm.canAccess(#id, principal)")` 全覆盖 | 端点缺注解 / `permitAll()` 过宽 / SpEL 绕过 |
| **Spring + MyBatis** | `WHERE id = #{id} AND owner_id = #{currentUserId}` | 仅 `WHERE id = #{id}`；批量 `<foreach>` 内无 owner |
| **Shiro** | `Subject.checkPermission("order:edit:" + id)` / `@RequiresPermissions` | 仅 `Subject.isAuthenticated()`；`anon` 过宽 |
| **Sa-Token** | `StpUtil.checkPermission` / `@SaCheckPermission` | 仅 `StpUtil.isLogin()` |
| **Django auth** | `UserPassesTestMixin` + `test_func`；`get_queryset` filter by `request.user` | 普通 ListView 无 Mixin；queryset 全表 |
| **Laravel Policy** | `$this->authorize('update', $model)` / Gate `Gate::allows` | 直接 `Model::find($id)->update(...)` |
| **Express + Casbin** | `enforcer.enforce(sub, obj, act)` | 仅 JWT 有效性检查 |
| **NestJS** | `@UseGuards(RolesGuard)` + custom `OwnerGuard` | 仅 `@UseGuards(AuthGuard)` |
| **GraphQL（graphql-shield）** | `permissions.compose({Query: {getUser: isOwner}})` | resolver 内裸 `Model.findById(args.id)` |

**业务字段被信任的危险点**（任何框架适用）：实体自动绑定（mass assignment）/ Cookie / Header 注入身份 / 金额 / 数量 / 折扣率从前端取值 / 状态字段直接 setter。

完整框架对照（Shiro / Sa-Token / DRF / NestJS / Flask / FastAPI 等）、ORM scope / Manager / hook 等"看不见的校验"识别位置、mass assignment 修复模板见 [references/framework-patterns.md](references/framework-patterns.md)。

---

## 7. 思考检查点（共享章节）

加载本 skill 时按这些问题按 sink 语义思考（不按业务命名）：

- 这个 ID / 字段从 HTTP 入口到 sink（CRUD / 状态转换 / 计算）之间，是否做了归属校验 / 角色判定 / 状态前提 / 数量上限**至少其一**？
- 这个角色判断是否同时覆盖**批量入口**？（单项 vs 批量端点常用不同 mapper 方法，校验逻辑容易漏一边）
- 状态转换是否检查前置状态？枚举有 N 个值，转换路径有 M 条，每条路径都覆盖了吗？
- 金额 / 数量 / 折扣是否从**服务端 source-of-truth** 重新计算？还是直接信前端传值？
- admin 端是否复用了普通端 Service 代码？同 Service 方法在 admin / 普通两个端点上看到的 source 角色不同，校验是否对齐了被请求者的角色？
- 跨子系统是否有同 pattern_id 端点？（admin 端常复用普通端代码且漏校验）

---

## 8. 检测方法论 / 数据流追踪

> 本能力**只到 `static-confirmed`（静态可达 + 中间无校验）**——动态确认走黑盒越权请求验证（见 §9）。本节方法论描述白盒追踪，不规定 plan / step 编排。

### Step 0：基线侦察

- 加载 `project-framework-analysis` 输出（如有）：项目所有 Controller + 角色清单 + 数据模型归属字段 + 多租户标识
- 识别框架栈（Spring Security / Shiro / Sa-Token / Django auth / Laravel Policy / Casbin 等），确认注解 / 中间件 / Policy 的实际覆盖面
- 列出所有"按 ID 操作他人资源"的端点候选（含 LIST / VIEW / EDIT / DELETE / 批量入口）

### Step 1：端点枚举与角色矩阵

按框架 grep 路由声明（Spring `@*Mapping` / Flask `@app.route` / Express `router.get|post|...` / Laravel `Route::*` 等）输出 `(端点, HTTP方法, source 字段, 可见角色断言)` 四元组；建 admin / operator / owner / guest 四态 × 端点矩阵，每格标预期可访问性。

### Step 2：source-to-sink 追踪

对每个端点四层逐一查校验：

1. **sink 层（Mapper / Repository / DB）**：SQL WHERE 是否含归属字段；ORM 查询是否走 scope / `for_user` 模式
2. **Service 层**：是否调用归属判定 / 角色断言 / 状态前提 / 数量上限校验
3. **Controller 层**：是否有权限注解（`@PreAuthorize` / `@RequiresPermissions` / Policy `authorize` 调用）
4. **AOP / Interceptor / Middleware 层**：是否统一加 owner 过滤 / 角色断言

工具加速：调用 `dataflow-analysis` MCP 工具做跨函数数据流追踪；调用 `sast-scan` 拿粗筛候选。

详细的 IDOR 逐步验证脚本（含 8 步分类协议与案例库索引）见 [references/idor-protocol.md](references/idor-protocol.md)。

### Step 3：状态机审计

找所有状态字段（`status` / `state` / `phase`）和枚举定义；列出所有状态转换路径；每条转换路径检查前置状态判定 / 触发者角色 / 业务前提。

### Step 4：批量入口扫描

grep `List<Long>` / `[]int64` / `ids:` / batch / bulk / multi 命名的端点；循环体内是否对每个 ID 逐条做归属校验；对照同资源的单项端点——单项有归属检查而批量没有 → **高危信号**。

### Step 5：金额 / 数量 source-of-truth 验证

grep `setAmount` / `setPrice` / `setQuantity` / `setDiscount` 等业务字段 setter；跟踪赋值来源是 request 还是 DB / 配置 source-of-truth；最终计算 / 持久化位置应按服务端 source-of-truth 重算。

### Step 6：信任边界审计

grep `Cookie.*getValue` / `request.getHeader("X-User-Id")` / `req.headers['x-user-id']` 等"客户端注入身份"通道；currentUser 上下文应唯一来自服务端 session / JWT 签名验证后的 claims；任何旁路 → 高危。

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 所有"按 ID 操作他人资源"端点已列全（LIST / VIEW / EDIT / DELETE 各独立看）
- [ ] 角色矩阵（admin / operator / owner / guest × 端点）已填完
- [ ] 每个端点的 sink 层 SQL / ORM 查询都查了归属约束
- [ ] 状态机枚举与所有转换路径都覆盖了前置状态判定
- [ ] 批量入口（含 `List<ID>` / batch / bulk / multi）独立扫描
- [ ] 金额 / 数量 / 折扣类字段从服务端 source-of-truth 验证
- [ ] AOP / Interceptor / Middleware / ORM scope 等"隐式校验层"独立读取确认
- [ ] 闭源依赖范围内的可达性已标 `static-unknown` 而非默认 not_vulnerable

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本漏洞特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 `result-with-file` 机器消费与计数闸门；判定上限错位会让"白盒已审"被误读为"动态确认"，导致整条链路失真。

### 白盒判定上限

本能力作为白盒原子能力，判定上限为 `static-confirmed`（静态可达性已证），**不等于动态 confirmed**。

**static-confirmed（落 `status=needs_review`）**：

- source 是用户可控 ID / 字段（resource_id / target_user_id / batch_ids / amount / state）
- sink 是 CRUD / 状态转换 / 金额计算 / 批量操作
- 中间无归属判定、无角色判定、无状态前提、无数量上限校验（四种约束**均缺**）

**static-unknown（落 `status=needs_review` + 标注 unknown）**：

- AOP / `@Aspect` / Interceptor / Middleware 改写 request 上下文（看不到）
- 注解处理器编译期注入校验（如 Lombok 风格的权限检查器）
- 跨服务 RPC 透传 currentUser，本服务边界处停手
- 动态权限策略（OPA / Casbin 动态规则 / 自定义 SPI）
- Cookie / Header 注入 currentUser 的旁路（走 [session-security](../session-security/SKILL.md)）

**not_vulnerable（落 `status=not_vulnerable`）**：

- 数据流分析证明 source 经归属判定 / 角色判定 / 状态前提 / 数量上限**至少其一**生效
- 端点不接触业务对象（静态资源 / 健康检查 / 公开列表）

**升级路径**（白盒不能独立给 `confirmed`）：

- 走 graybox 流程：用白盒候选指导黑盒越权请求构造
- 黑盒构造 A 用户登录 → 请求 B 用户的资源 → 观测到 B 资源被读 / 改 / 删 / 状态转换 → 结论从 `static-confirmed` 升为 `confirmed`
- 状态机绕过：构造跨步请求观测状态字段直接转到目标值
- 金额篡改：构造负数 / 极大值 / 折扣 0 等请求观测持久化结果

**禁止**白盒独立判 `confirmed`——无可观测效果证据，仅静态可达不构成动态利用。

### 产物契约（必须遵守）

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/business-logic-auth-review.jsonl`，不等汇总阶段回头整理：

```json
{
  "id": "bla-001",
  "title": "订单详情接口缺少归属校验",
  "severity": "high",
  "cwe": "CWE-639",
  "source": "@PathVariable Long orderId",
  "sink": "OrderMapper.selectById",
  "entry_point": "GET /api/orders/{orderId}",
  "victim_role": "owner",
  "actor_role": "owner",
  "vuln_type": "idor-horizontal",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "file_location": "OrderController.java:42",
  "source_report": "business-logic-auth-review",
  "description": "..."
}
```

字段约束：

- `id` 带 `bla-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒默认 `needs_review`）
- `confidence ∈ static-confirmed | static-unknown | not-applicable`
- `vuln_type ∈ idor-horizontal | idor-vertical | state-machine-bypass | batch-auth-gap | amount-tampering | mass-assignment | tenant-isolation`
- `(entry_point, actor_role, victim_role, vuln_type)` **四元组任一不同即各自独立成行**——禁止合并折叠
- `actor_role` / `victim_role` 取 `admin | operator | owner | guest | anonymous`
- `file_location` 填 `file:line`，不留空、不写区间

### 反例义务（必须遵守）

> **why**：白盒"权限边界完整"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统该维度安全"。

写"权限边界完整"或"未发现越权"前，产物必须包含：

- 全角色 × 端点矩阵（admin / operator / owner / guest × 所有端点），每格标注实际可访问性
- 所有 sink 候选位置完整清单（Mapper / Repository / 状态转换 / 批量入口 / 金额计算 grep 覆盖证据）
- 所有 source 候选位置完整清单（每个 Controller 都有追踪结论）
- 每个 (source, sink, vuln_type) 三元组的判定结果（归属 / 角色 / 状态 / 数量约束哪个生效）
- `static-unknown` 单元格的具体原因（AOP / 闭源 / RPC / 动态策略）

清单不完整 → 结论降级为 `partial-coverage`。

参考案例库（执行验证协议前可读）：

- [idor-ownership-absence.md](references/idor-ownership-absence.md) — operator 根本不存在型 IDOR
- [authz-client-derived-operator.md](references/authz-client-derived-operator.md) — operator 存在但来自客户端可控输入（伪校验）
- [authz-independent-endpoint-verification.md](references/authz-independent-endpoint-verification.md) — 不能从一个接口的安全性推断其他接口
- [vertical-privilege-missing-role-check.md](references/vertical-privilege-missing-role-check.md) — 管理操作缺少角色校验
- [mass-assignment-privilege-escalation.md](references/mass-assignment-privilege-escalation.md) — 自动绑定覆盖敏感字段导致提权
- [idor-batch-operation-gap.md](references/idor-batch-operation-gap.md) — 批量操作跳过单项 ownership 校验
- [tenant-isolation-failure.md](references/tenant-isolation-failure.md) — 多租户查询缺少 tenant 作用域隔离

---

## 10. 具象化反例库（共享章节）

### FP（看似命中实际不构成）

**反例 1：AOP 拦截器统一做归属校验**

- 抽象规则：Spring `@Aspect` / Around advice 在 sink 调用前注入 owner 判定，Controller / Service 单看都没校验
- 具体场景：`@Aspect class AuthAspect { @Around("@annotation(AuthCheck)") ... }` 配合 `@AuthCheck` 注解
- 关键识别特征：项目有 `@Aspect` 类 / Spring AOP 配置 / 自定义注解；端点上有特定注解（如 `@AuthCheck` / `@OwnerOnly`）
- 排除方法：独立读 Aspect 实现确认拦截逻辑，确认覆盖端点；标 `static-unknown` 而非直接判 not_vulnerable

**反例 2：中间件层做了角色判定，Service 单看是裸的**

- 抽象规则：Express / Koa middleware 在路由前判角色，controller 内 `Model.findByPk` 看似裸调
- 具体场景：`app.use('/admin', requireRole('admin'), adminRouter)`
- 关键识别特征：路由挂载链可见 middleware 层；中间件实现做角色断言
- 排除方法：读路由挂载顺序确认 middleware 覆盖目标 handler

**反例 3：ORM 自动加 tenant_id / owner_id 过滤**

- 抽象规则：typeorm scope / Django Manager `for_user` / Sequelize hook `beforeFind` 自动注入 WHERE 条件
- 具体场景：`@Scope(["active", "current-tenant"])` 自动加 tenant_id；Django 自定义 Manager 重写 `get_queryset`
- 关键识别特征：Model 定义里有 scope / hook / Manager 重写；查询不显式加 WHERE 但 ORM 自动注入
- 排除方法：读 Model / Manager 实现确认 hook 触发条件，是否所有查询路径都触发

### FN（看似不命中实际是真洞）

**反例 4：批量接口循环里漏校验**

- 抽象规则：单项端点 Service 安全，批量端点走不同 Service 方法（`selectByIds` vs `selectByIdAndOwner`），SQL WHERE 条件差异
- 关键识别特征：批量端点参数是 ID 数组；批量 Service 方法名与单项不同
- 详见 [references/idor-batch-operation-gap.md](references/idor-batch-operation-gap.md)

**反例 5：状态机用枚举但缺前置状态判定**

- 抽象规则：状态字段是枚举类型不能注入字符串，但 `setStatus(OrderStatus s)` 无前置状态判定可被跨步调用
- 关键识别特征：状态枚举存在；状态转换 setter 接口未校验当前状态
- 确认方法：grep 所有 `setStatus` / `transition` 调用，看是否有 `if (status == X)` 前置判

**反例 6：数据归属字段从 Cookie 取值**

- 抽象规则：currentUser 上下文从 Cookie 取（`Cookie["userId"]`），用户可篡改 Cookie 注入任意身份
- 关键识别特征：grep `Cookie.*getValue.*[uU]ser` / `request.getCookie` + 身份字段名
- 确认方法：读 Filter 实现确认 currentUser 来源是否 JWT 签名 / session 而非裸 Cookie

**反例 7：admin 端复用普通端代码且共用 Service 无角色分支**

- 抽象规则：admin / 普通端共用 Service 方法且 Service 无角色判定分支——可能 admin 越权读不到他人，也可能普通用户绕到 admin 端复用同方法
- 关键识别特征：admin / 普通端点调用同一个 Service 方法；Service 内仅做单一归属判定
- 确认方法：列角色 × 端点矩阵，看 admin 端点的预期能力与 Service 内归属判定是否对齐

### 易混淆案例

**反例 8：闭源依赖里的权限校验默默被忽略**

- 抽象规则：项目调用 `external-auth-lib.checkOwner(user, resource)` 看似有校验，但闭源库内部可能仅 log 不抛异常
- 关键识别特征：调用点看起来安全，但下游闭源（依赖图谱里的 `unknown` 标注）
- 排除方法：标 `static-unknown` 推 dependency-decompile 做反编译；不能默认为 not_vulnerable

更多场景化反例见 `references/` 案例库（client-derived operator / 跨端点独立性 / mass assignment / tenant isolation 各有专题）。

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到代码层 pattern + 跨函数追踪为止。

下面这些情形数据流分析无法继续追踪，**必须标 `static-unknown`**，不允许默认为 not_vulnerable：

1. **AOP / `@Aspect` / Spring Interceptor / Django middleware 改写 request 上下文**——独立读拦截器实现确认覆盖端点，不能仅看 Controller 判 not_vulnerable
2. **注解处理器编译期注入校验**（APT / Lombok 风格的权限检查器）——读 generated 目录或反编译 .class 确认
3. **动态权限策略（OPA / Casbin 动态规则 / 自定义 SPI）**——权限规则从配置 / DB / 远程加载，读实际规则确认覆盖逻辑
4. **跨服务 RPC 鉴权（JWT 透传 / SSO / OAuth2 token introspect）**——本服务范围内追到出站 RPC 调用即停，下游服务单独审计
5. **Cookie / Header 注入 currentUser 的旁路**——走 [session-security](../session-security/SKILL.md) 专项确认 session / token 完整性
6. **ORM scope / Manager / hook 自动注入 WHERE 条件**（typeorm `@Scope` / Django Manager `for_user` / Sequelize `beforeFind` / GORM scope）——读实现确认所有查询路径都触发，任一旁路未触发标 `static-unknown`
7. **反射 / 动态方法分派**（Java `Method.invoke` / Python `getattr` / JS `obj[methodName]()`）——记录反射点行号

**底线**：本能力写"权限边界完整"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

协作工具：

- `sast-scan`：粗筛"按 ID 操作他人资源"端点候选
- `dataflow-analysis`：跨函数追 source-to-sink 可达性
- `session-security`：身份上下文完整性专项
- `dependency-decompile`：闭源依赖反编译追踪

---

## 12. 修复建议（共享章节）

### 源头治理（首选）

- **所有按 ID 操作他人资源的 Mapper SQL 加归属约束**：

  ```xml
  <!-- 危险 -->
  <select id="selectById">SELECT * FROM orders WHERE id = #{id}</select>
  <!-- 安全 -->
  <select id="selectByIdAndOwner">
    SELECT * FROM orders WHERE id = #{id} AND owner_id = #{currentUserId}
  </select>
  ```

- **权限判定在 Service 入口统一做**：取出资源后比对 `resource.ownerId == currentUser.id || currentUser.isAdmin()`，否则抛 AccessDeniedException。不依赖前端隐藏 / 不依赖路由路径。

- **批量接口循环内逐条校验**：取出资源集合后 `forEach` 内逐条比对 `ownerId`，任一不匹配抛异常。批量大小加上限。

- **状态转换显式检查前置状态**：`shipOrder` / `completeOrder` 等业务方法内先校验 `order.status == PAID` 才允许转 SHIPPED，状态字段不允许直接 setter 暴露。

- **金额 / 数量从服务端 source-of-truth 重新计算**：订单总价 = `SELECT price FROM goods WHERE id = ? × quantity`；折扣率从配置 / 优惠券表查；数量加范围检查；不信前端传值。

- **实体绑定时显式排除敏感字段**：用 DTO 白名单 / `@JsonIgnore` / `fillable` 配置，禁止 `@RequestBody UserEntity` 直接覆盖 role / ownerId / tenantId。详细模板见 [references/framework-patterns.md](references/framework-patterns.md) 业务字段被信任的危险点段。

### 深度防御

- Spring Security 注解层（`@PreAuthorize`）+ Service 业务校验层双层
- 多租户场景在 ORM scope / Manager 层注入 tenant_id 过滤作为兜底
- 角色 × 端点矩阵作为自动化测试用例覆盖（每个角色访问每个端点的预期结果）

### 兜底拒绝

- API 网关 / WAF 层做粗粒度路径级 ACL（仅作辅助，不替代业务层校验）
- 数据库账号最小权限：业务账号禁止跨用户 / 跨租户全表 SELECT

### 参考

- [OWASP Broken Object Level Authorization (API1:2023)](https://owasp.org/API-Security/editions/2023/en/0xa1-broken-object-level-authorization/)
- [OWASP Broken Function Level Authorization (API5:2023)](https://owasp.org/API-Security/editions/2023/en/0xa5-broken-function-level-authorization/)
- [OWASP Mass Assignment Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Mass_Assignment_Cheat_Sheet.html)
