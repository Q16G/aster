# IDOR 验证协议（详细执行步骤）

> 主 SKILL.md §8 给出方法论高层；本文件是 IDOR 维度具体的逐步验证脚本，配合 `references/` 下 7 个案例文件一起用。

## 准备：阅读案例库

执行本协议前，先读取以下案例文件建立漏洞模式认知：

- [idor-ownership-absence.md](idor-ownership-absence.md) — operator 根本不存在型 IDOR
- [authz-client-derived-operator.md](authz-client-derived-operator.md) — operator 存在但来自客户端可控输入（伪校验）
- [authz-independent-endpoint-verification.md](authz-independent-endpoint-verification.md) — 不能从一个接口的安全性推断其他接口
- [vertical-privilege-missing-role-check.md](vertical-privilege-missing-role-check.md) — 管理操作缺少角色校验
- [mass-assignment-privilege-escalation.md](mass-assignment-privilege-escalation.md) — 自动绑定覆盖敏感字段导致提权
- [idor-batch-operation-gap.md](idor-batch-operation-gap.md) — 批量操作跳过单项 ownership 校验
- [tenant-isolation-failure.md](tenant-isolation-failure.md) — 多租户查询缺少 tenant 作用域隔离

## 步骤 1：枚举所有接受资源 ID 的端点

```bash
# Spring
rg "@RequestMapping|@GetMapping|@PostMapping|@PutMapping|@DeleteMapping" --type java
# Flask / FastAPI
rg "@(app|bp|router)\.(route|get|post|put|delete)" --type py
# Express
rg "router\.(get|post|put|delete)|app\.(get|post|put|delete)" --type js
# Laravel
rg "Route::(get|post|put|delete)" --type php
```

重点关注参数中含 `id` / `@PathVariable Long id` / `@RequestParam Long id` / `req.params.id` 的端点。

**关键陷阱**：没有 operator 参数的端点恰恰是最高风险的——不要只关注"同时含 operator 和 resource"的端点。

## 步骤 2：按操作类型独立验证

同一 Controller 的 LIST / VIEW / EDIT / DELETE 必须逐个检查，不可以偏概全。

LIST 有 account 过滤**不能推断** VIEW / EDIT / DELETE 也安全——它们可能走不同的 mapper 查询（如 `selectByParams` vs `selectById`）。

参见 [authz-independent-endpoint-verification.md](authz-independent-endpoint-verification.md)。

## 步骤 3：检查 mapper / 数据层

对每个端点，追踪到 mapper XML / SQL / ORM 查询，确认 WHERE 条件是否包含操作者约束（account / userId / tenantId）。

只含资源型参数（如 `#{id}`）而无操作者约束的查询是高危候选。

```xml
<!-- 危险 -->
<select id="getOrder" resultType="Order">
  SELECT * FROM orders WHERE id = #{id}
</select>

<!-- 安全 -->
<select id="getOrderByUser" resultType="Order">
  SELECT * FROM orders WHERE id = #{id} AND user_id = #{currentUserId}
</select>
```

## 步骤 4：分类落 confidence

按数据层是否包含 operator 约束分类：

- `operator-constraint-present`：数据层查询包含 server-derived 的 operator 约束（落 `not_vulnerable` 或 `static-confirmed-safe`）
- `operator-constraint-absent`：数据层查询只有资源 ID，无任何 operator 约束（落 `static-confirmed` IDOR 候选）
- `operator-constraint-partial`：部分操作有约束、部分无（参见 `authz-independent-endpoint-verification.md`，逐操作独立分类）

## 步骤 5：垂直越权检查

对涉及管理语义的端点（账号停用 / 配置修改 / 日志查看 / 批量操作），检查是否有角色校验：

- Spring Security：`@RequiresRoles` / `@PreAuthorize("hasRole('ADMIN')")` / `@Secured`
- Shiro：`@RequiresRoles` / `Subject.hasRole`
- Sa-Token：`@SaCheckRole`
- Django：`@permission_required` / `user_passes_test`
- Laravel：Policy / Gate

**只检查登录态（session 非空）而不检查角色 = 垂直越权**。参见 [vertical-privilege-missing-role-check.md](vertical-privilege-missing-role-check.md)。

## 步骤 6：交叉验证 operator 来源

operator 上下文是 **server-derived**（session / SecurityContext / JWT signed claims）还是 **client-derived**（request param / cookie / header）？

`client-derived` 的 operator 等于没有 operator——攻击者直接构造请求伪造身份。参见 [authz-client-derived-operator.md](authz-client-derived-operator.md)。

## 步骤 7：批量操作验证

对接受 ID 数组 / 列表的端点（URL 含 batch / bulk / multi，参数为 `List<Long>` / `[]string` / 逗号分隔 ID），检查是否对每个 ID 逐项做归属校验。

对比同资源的单项端点——单项有 ownership 检查而批量没有是高危信号。参见 [idor-batch-operation-gap.md](idor-batch-operation-gap.md)。

## 步骤 8：多租户隔离验证

当项目存在 `tenant_id` / `org_id` 等多租户标识时，检查所有数据层查询是否包含 tenant 约束。

重点关注：

- 查询无 tenant WHERE 条件
- tenant 值从 request 而非 session 获取
- 缓存 key 不含 tenant 前缀

参见 [tenant-isolation-failure.md](tenant-isolation-failure.md)。

## 工具加速

当 `dataflow-analysis` 可用时，使用其 SSA 模板 D（IDOR 跨层 ownership 追踪）和模板 F（垂直越权角色判定追踪）做跨层确认，避免人工逐文件读取。
