---
name: business-logic-auth-review
description: 业务逻辑与认证授权专项复核 — 覆盖登录流程、session/cookie 鉴权、IDOR/ownership 权限边界、CSRF、敏感操作二次验证等规则引擎难以覆盖的语义安全检查。
tags: code-audit,authn,authz,idor,ownership,business-logic
when-to-use: 当项目存在登录、session、cookie、controller/service/mapper 链路，且需要补充业务逻辑与权限边界复核时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 业务逻辑与认证授权专项复核

## 目标

这个 skill 专门补足两类 AST 难点：

- 认证语义：登录逻辑反转、伪成功、伪失败、权限只靠客户端态
- ownership / IDOR：controller 看起来有鉴权，但 service / mapper 丢失操作者约束

它不是规则替代品，而是把这些问题固定成可执行 checklist。

## 适用信号

当识别到以下任一信号时，应加载本 skill：

- 项目有登录、会话管理、权限判断逻辑
- `session` / `cookie` / `request attribute` 参与身份流转或权限决策
- 参数同时含 operator/principal/tenant 与 resource/target ID
- Java Web：Spring MVC / MyBatis / Shiro / Spring Security，controller → service → mapper 结构
- PHP：`$_SESSION` / `$_COOKIE` 用于身份判断，`header()` 做重定向，`mysqli` / PDO 直连
- Python：Flask session / Django auth / FastAPI Depends
- Go：gin context / echo context / 自定义 middleware

## 固定检查项

### 1. 登录接口

逐个检查（8 项）：

- 登录查询的 lookup key 是否正确（用户名、邮箱、手机号等）
- 查询返回非空后，是走成功还是失败分支（反转检测）
- 查询返回空后，是走失败还是成功分支（反转检测）
- 密码/凭证比对是否存在且方向正确
- session 是否在正确分支创建/刷新
- 登录响应码/消息是否与实际认证结果一致
- 是否存在频率限制或锁定机制
- 登录成功后是否重建 session（调用 `session.invalidate()` / `changeSessionId()` 防止 session 固定攻击）

注意识别变体模式：
- Result 对象返回（`Result.error()` / `Result.ok()`）
- 异常抛出（`throw new AuthException()`）
- 集合型判断（`list.isEmpty()` / `list.size() == 0`）

### 2. 管理接口

逐个检查：

- 是否只用 body/query/cookie 里的 account/role/id 做权限判断
- 是否缺少 server-side auth context
- 是否在执行敏感操作前只做了客户端可控字段比对

### 3. 查询与 ownership

逐个检查：

- controller 是否拿到了 operator/owner 信息
- service 是否继续保留该约束
- mapper / repository / SQL 是否最终落到了 where 条件
- 是否只剩 target/resource ID 而丢失 owner/operator

### 4. session / cookie 信任边界

逐个检查：

- request-derived value 是否进入 session
- Cookie value 是否直接进入分支判断
- session 中的身份字段是否可被覆盖、污染或重新绑定

### 5. 对象绑定与权限字段

逐个检查：

- 端点是否使用自动对象绑定（`@ModelAttribute` / `@RequestBody` 映射到实体 / `BindJSON` / `request.form` → model）
- 被绑定的实体类是否包含权限/角色/状态字段（role / isAdmin / status / tenantId）
- 这些字段是否被保护（`@JsonIgnore` / DTO 白名单 / 显式赋值而非自动绑定）

参见 [mass-assignment-privilege-escalation.md](references/mass-assignment-privilege-escalation.md)

## 结论口径

结论**按入口点组织**。每个入口点 = controller 方法 + HTTP method + URL pattern。

IDOR 验证协议第 1 步"枚举端点"的输出直接作为结论的入口点骨架。同一入口点的所有发现（IDOR、Cookie 伪鉴权、登录反转、session 污染等）合并到该入口点下。

同一个 sink 被多个入口点到达时，每个入口点下都要独立列出。

每条发现包含以下维度：

- 问题类别（认证绕过 / 未授权访问 / IDOR / Cookie 伪鉴权 / Session 污染）
- 操作者上下文来源（session / SecurityContext / cookie / request param）
- 目标对象
- 是否存在 server-side 身份校验
- 为何需要复核
- 建议后续动作

判定结果（三个等级均须输出，供人工审核完整审计范围）：
- `confirmed`：漏洞已确认，可直接进入修复
- `needs_review`：高风险但需人工语义确认
- `not_vulnerable`：已排除（误报或存在防护），仍须列出以证明覆盖范围

不允许只输出 confirmed 而省略其他等级。全量输出使审计过程透明、可追溯，审核者可对边界 case 做二次判断。

## 框架模式库

在审计不同框架的项目时，关注以下鉴权模式和常见缺口：

### Spring Security

- 鉴权模式：`@PreAuthorize` / `@Secured` / `@RolesAllowed` / `SecurityContextHolder.getContext().getAuthentication()`
- 常见缺口：注解缺失、SpEL 表达式绕过、`SecurityFilterChain` 配置遗漏、`permitAll()` 过宽

### Shiro

- 鉴权模式：`Subject.isAuthenticated()` / `Subject.hasRole()` / `@RequiresAuthentication` / `@RequiresRoles`
- 常见缺口：`remember-me` 误当 `authenticated`、URL 路径绕过（`/admin/../`）、`anon` 过滤器配置过宽

### PHP 原生 / 框架

- 鉴权模式：`$_SESSION['user_id']` / `$_COOKIE['role']` / `isset($_SESSION[...])` / 自定义 auth 函数
- 常见缺口：Cookie 值直接用于权限判断（客户端可伪造）、`$_SESSION` 未在登录后 `session_regenerate_id()`、`header("Location: ...")` 重定向未 `exit`、角色判断只靠 Cookie 而非数据库查询

### 自定义 Filter / Interceptor

- 鉴权模式：`HttpSession.getAttribute("userId")` / Cookie / `request.getAttribute()` from filter
- 常见缺口：session 固定、Cookie 可伪造、filter 顺序错误、拦截器 exclude 路径配置不当

## IDOR 验证协议

> **参考案例**：执行本协议前，应先阅读 `references/` 下的案例文件以建立漏洞模式认知：
> - [idor-ownership-absence.md](references/idor-ownership-absence.md) — operator 根本不存在型 IDOR
> - [authz-client-derived-operator.md](references/authz-client-derived-operator.md) — operator 存在但来自客户端可控输入（伪校验）
> - [authz-independent-endpoint-verification.md](references/authz-independent-endpoint-verification.md) — 不能从一个接口的安全性推断其他接口
> - [vertical-privilege-missing-role-check.md](references/vertical-privilege-missing-role-check.md) — 管理操作缺少角色校验
> - [mass-assignment-privilege-escalation.md](references/mass-assignment-privilege-escalation.md) — 自动绑定覆盖敏感字段导致提权
> - [idor-batch-operation-gap.md](references/idor-batch-operation-gap.md) — 批量操作跳过单项 ownership 校验
> - [tenant-isolation-failure.md](references/tenant-isolation-failure.md) — 多租户查询缺少 tenant 作用域隔离

按以下步骤验证：

1. **枚举所有接受资源 ID 的端点**：用 `rg "@RequestMapping|@GetMapping|@PostMapping|@PutMapping|@DeleteMapping"` 等方式枚举所有 Controller 方法。重点关注参数中含 `id` / `@RequestParam Integer id` / `@PathVariable Long id` 的端点。**没有 operator 参数的端点恰恰是最高风险的**——不要只关注"同时含 operator 和 resource"的端点
2. **按操作类型独立验证**：同一 Controller 的 LIST / VIEW / EDIT / DELETE 必须逐个检查，不可以偏概全。LIST 有 account 过滤**不能推断** VIEW/EDIT/DELETE 也安全——它们可能走不同的 mapper 查询（如 `selectByParams` vs `selectById`）
3. **检查 mapper/数据层**：对每个端点，追踪到 mapper XML / SQL / ORM 查询，确认 WHERE 条件是否包含操作者约束（account/userId/tenantId）。只含资源型参数（如 `#{id}`）而无操作者约束的查询是高危候选
4. **分类**：
   - `operator-constraint-present`：数据层查询包含 server-derived 的 operator 约束
   - `operator-constraint-absent`：数据层查询只有资源 ID，无任何 operator 约束
   - `operator-constraint-partial`：部分操作有约束、部分无（参见 `references/authz-independent-endpoint-verification.md`）
5. **垂直越权检查**：对涉及管理语义的端点（账号停用/配置修改/日志查看/批量操作），检查是否有角色校验（`@RequiresRoles` / `@PreAuthorize` / `isAdmin()` 等）。只检查登录态（session 非空）而不检查角色 = 垂直越权
6. **交叉验证**：operator 是 server-derived（session / SecurityContext）还是 client-derived（request param / cookie）。client-derived 的 operator 等于没有 operator（参见 [authz-client-derived-operator.md](references/authz-client-derived-operator.md)）
7. **批量操作验证**：对接受 ID 数组/列表的端点（URL 含 batch/bulk/multi，参数为 `List<Long>` / `[]string` / 逗号分隔 ID），检查是否对每个 ID 逐项做归属校验。对比同资源的单项端点——单项有 ownership 检查而批量没有是高危信号（参见 [idor-batch-operation-gap.md](references/idor-batch-operation-gap.md)）
8. **多租户隔离验证**：当项目存在 tenant_id / org_id 等多租户标识时，检查所有数据层查询是否包含 tenant 约束。重点关注：查询无 tenant WHERE 条件、tenant 值从 request 而非 session 获取、缓存 key 不含 tenant 前缀（参见 [tenant-isolation-failure.md](references/tenant-isolation-failure.md)）

当 `dataflow-analysis` 可用时，使用 SSA 模板 D 和模板 F 做跨层 ownership 确认。

## 和其他 skill 的关系

- `sast-scan` 负责给出候选线索
- `dataflow-analysis` 负责给出流向事实
- 本 skill 负责补业务语义、权限边界与 ownership 审查

本 skill 负责认证授权维度的深度复核，由 `security-code-analysis` 审计清单中 B 类任务项触发加载。
