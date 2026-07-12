# 认证与权限模型细则（auth-model）

> 五张模型之一「认证与权限模型」的细则。认证逻辑决定哪些端点匿名可达、哪些需角色校验；归属字段决定越权判定的基准。本模型是越权 / 认证 / IDOR / 多租户类维度的前提，**不做漏洞判定**。

## 1. 认证与会话架构

- **识别信号**：登录端点位置、Token 类型（JWT / Cookie-Session / 自定义 header）、`@PreAuthorize` / `@RequiresRoles` / `@SaCheckLogin` 注解、Spring Security / Shiro / Sa-Token 配置类。
- **须产出**：会话策略（Token 载体 / 有效期 / 刷新机制）+ 鉴权机制（在哪一层校验、注解还是过滤器）+ 恒真 / 空实现的校验位置（形如 `validateXxx` 直接 `return true`，见 architecture-model.md ④ 认证封装）。

## 2. 角色与权限分级

- **识别信号**：角色枚举 / 权限表 / RBAC 配置；元素或端点上的角色注解。
- **须产出**：角色模型形态——单层 vs 分级、RBAC vs ABAC、是否有第三方接入身份（OAuth / API Key / Bot Token）。这是垂直越权维度的基准。

## 3. 多租户标识

- **识别信号**：`tenant_id` / `org_id` / `workspace_id` 字段贯穿实体与查询条件；租户从会话注入还是请求携带。
- **须产出**：多租户隔离字段 + 隔离在哪一层强制（查询条件自动拼租户 vs 依赖调用方传）——多租户穿透维度的基准。

## 4. 数据归属字段对照表

IDOR / ownership / 越权类审计依赖 owner ↔ resource 字段对照。

- **识别信号**：实体类字段命名（`userId` / `owner_id` / `tenant_id` / `principalId`）、表 schema。
- **须产出**：`owner/operator 字段集合` + `resource/target 字段集合` + `多租户字段`，逐实体列出。下游按字段名直接追踪"用户可控的 resource_id 是否经过归属判定"。与业务模型「归属一致性不变量」交叉引用。

## 产物落库

写入 `shared/models/auth-model.md`：认证机制描述 + 归属字段对照表。

```markdown
## 认证机制
- 会话：JWT（TokenValidator.validateToken 方法体恒真 → 失效校验，TokenValidator.java:15）
- 角色分级：RBAC（user / admin）；多租户 tenant_id 隔离

## 归属字段对照表
| 实体 | 字段 | 角色(owner/resource/tenant) | file_location |
|------|------|----------------------------|---------------|
| Order | userId | owner | Order.java:12 |
| Order | orderId | resource | Order.java:8 |
```

就地更新不删旧行。

> 注意：本文件 `references/auth-model.md` 是**建模方法论细则**（怎么建）；运行时产物落 `shared/models/auth-model.md`（建出来的模型）。二者同名不同物。
