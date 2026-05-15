---
name: business-logic-auth-review
description: 业务逻辑与认证授权专项复核 skill，用 checklist 和半自动链路分析补足 Semgrep/SSA 对登录语义、Cookie 鉴权、ownership/IDOR 的不足。
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

当识别到以下任一信号时，应考虑加载本 skill：

- Java Web / Spring MVC / MyBatis
- `session` / `cookie` / `request attribute`
- 登录相关方法或接口
- `controller -> service -> mapper` 结构
- 参数同时含 account/principal/tenant 与 resource/userId/reportId

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

### 自定义 Filter / Interceptor

- 鉴权模式：`HttpSession.getAttribute("userId")` / Cookie / `request.getAttribute()` from filter
- 常见缺口：session 固定、Cookie 可伪造、filter 顺序错误、拦截器 exclude 路径配置不当

## IDOR 验证协议

对疑似 IDOR 的链路，按以下步骤验证：

1. 枚举同时含 operator（身份）和 resource（目标）参数的端点
2. 追踪 operator 是否到达数据层（SQL / ORM / Repository）
3. **检查 mapper/XML 层**：对 MyBatis 项目，用 `rg` 检查对应 mapper XML 的 `<select>`/`<update>`/`<delete>` 中 WHERE 条件是否同时包含操作者型和资源型参数。只含资源型参数（如 `#{orderId}`）而无操作者约束（如 `#{tenantId}`/`#{operatorId}`）的查询是高危候选
4. 分类：`operator-constraint-present` / `absent` / `partial`
5. 交叉验证：operator 是 server-derived（session / SecurityContext）还是 client-derived（request param / cookie）

当 `dataflow-analysis` 可用时，使用 SSA 模板 D 和模板 F 做跨层 ownership 确认。

## 和其他 skill 的关系

- `sast-scan` 负责给出候选线索
- `dataflow-analysis` 负责给出流向事实
- 本 skill 负责补业务语义、权限边界与 ownership 审查

若没有明确高风险信号，不建议默认常驻所有 `code-audit` 流程。
