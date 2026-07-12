---
name: vertical-privilege-escalation
description: 垂直越权检测 — 低权限账号可访问高权限功能或管理接口的风险；适用于角色权限模型、后台功能与操作审计场景。
when-to-use: 当需要验证低权限账号是否可访问高权限功能或管理接口时
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# 垂直越权检测

## 成因引用

垂直越权成因：source（用户角色字段——JWT claim 中的 role、cookie 中的 role / is_admin、请求体 / query 中的 role 字段、自定义请求头 `X-Role` / `X-Admin`、session 里的角色态）→ sink（权限校验逻辑：高权限端点的角色判定、多角色系统的权限边界、敏感操作的角色前置条件）。判定按 sink 语义"该端点 / 该动作是否仅在高权限主体下放行"——业务命名（admin / manager / operator / supervisor）只是表层。详见同根目录 `pentest/web-security-testing/SKILL.md` 漏洞成因图谱 · 垂直越权行（不在本 skill 重复成因）。

## 触发线索（基线检查项）

以下是已知的常见垂直越权触发线索，作为**基线起点**而非必检硬清单。结合目标代码与上下文动态调整：

- 适用且已完成 → 标注 `[x] done`
- 明确不适用 → 标注 `[-] n/a (原因)`，原因要具体到代码事实
- 基线未列出但实际发现 → 新增条目并标注 `[+] added (来源)`

基线触发线索按"sink 语义"分类（不按业务命名）：

- **管理后台端点**：`/admin/*` / `/manage/*` / `/operator/*` / `/supervisor/*`，应仅高权限可达
- **角色切换 / 提升接口**：`/api/users/me/role` / `/api/roles/switch`，角色变更动作
- **用户管理操作**：创建用户 / 重置密码 / 改角色 / 禁用账号，仅 admin 可执行
- **配置管理 / 系统设置**：`/api/config/*` / `/api/settings/*`，可改全局参数
- **审批 / 审核 / 仲裁**：流程节点的审批人角色判定
- **数据导出 / 全量查询**：跨用户的报表 / 日志 / 审计接口
- **role 字段在请求中**：body / query / header / cookie / JWT claim 内的 `role` / `is_admin` / `is_superuser` / `level` / `scope` / `permissions`
- **多角色 cookie 共存**：session 同时持多个 role cookie，后端优先级判定可能错位
- **前端按角色渲染菜单**：仅前端判角色后端不校验的典型场景（菜单隐藏 ≠ 后端拒绝）
- **代码模式**：handler 入口缺 `auth.RequireRole("admin")` 调用；JWT 解析后直接信任 claim 不二次校验；mass-assignment 把请求体里的 role 直接绑定到模型

## 思考检查点

加载本 skill 时按这些问题思考：

- 这个端点的"高权限"含义是什么？后端在哪一步判定角色？
- role 来源有几处（JWT claim / cookie / session / 请求体 / header）？每个来源都可控吗？
- 请求体里有 role / is_admin / scope 字段吗？是否被 mass-assignment 直接绑定到 DB 模型？
- 同子系统其他 admin 端点的角色校验是逐项配置的吗？需要按端点账本独立测
- 写操作命中时，是否为不可逆动作？需走哨兵自证或非破坏差分

## 前置条件与安全边界

- 准备至少两类账号：`高权限账号` 与 `低权限账号`。
- 仅在授权测试环境执行，写操作优先使用可回滚测试数据。涉及删除/覆盖/不可逆写时，按 `common/closure-verification.md` 的《破坏性 / 不可逆动作的闭环边界》执行——强制哨兵自证或非破坏差分，二者都做不到就停 `suspected`，不得对真实业务数据执行破坏动作来强行闭环。
- 单接口默认最多 8 次请求（高权限基线、低权限对照、复验）。
- 单轮只改变"账号权限级别"，其余请求参数保持一致。

## 检测步骤

### Step 1：基线与端点画像

1. 从端点账本（`recon-methodology` 产出的 `shared/models/site-model.md` 端点总表）筛选"应仅高权限可达"端点（管理后台、配置、用户管理、审批等）
2. 使用高权限账号调用接口建立基线，记录成功条件与关键返回字段（如管理面板列表 / 全量用户表）
3. 识别 role 在请求里的所有载体（JWT claim / cookie / session id / body 字段 / header / query）

### Step 2：凭证替换

保持请求参数不变，仅替换为低权限账号凭证并重放请求，观察是否仍能成功执行高权限操作或读取高权限数据。

### Step 3：role 字段篡改 / 注入

- **JWT claim 篡改**：在低权限账号 JWT 中改 role 为 admin，观察是否未做签名复核 / 未做 claim 二次校验
- **cookie / header 注入**：低权限会话上加 `X-Role: admin` / `X-Admin: 1` / `role=admin` cookie
- **body mass-assignment**：写操作时在 body 加 `"role":"admin"` / `"is_admin":true`，探测后端是否直接绑定
- **多角色共存**：同时携带 `role=user; role=admin` 两个 cookie，探测后端取优先级差异

### Step 4：role 标志置空 / fail-open

将角色或权限标志（`role`、`is_admin`、`level`、`scope`）置空、缺省、置 0 或降级提交，探测后端是否在标志缺失时 fail-open 到高权限或跳过角色校验（典型 mass-assignment / 默认角色缺陷）。对写操作执行"写后读"验证；若为删除/覆盖/不可逆写，按《破坏性 / 不可逆动作的闭环边界》改走哨兵自证或非破坏差分。

> 次级向量：HTTP 方法 / 路径变体（`/admin/users` vs `/api/admin/users` vs `/v2/admin/users`）/ 大小写绕过路由前缀匹配，鉴权严格度可能不一致。

## 示例库

### 正例形态（代码层根因）

- `router.GET("/admin/users", handlers.ListUsers)` — admin 端点漏挂角色校验中间件（`low-role-admin-endpoint-no-check-vpe`）
- `db.Model(&user).Updates(c.Bind(req))` — 请求体 role 字段被 mass-assignment 直接绑定（`body-role-field-mass-assignment-vpe`）
- `claims := jwt.Parse(token); if claims["role"] == "admin" { ... }` — JWT claim 直接信任，未做签名 / 角色二次校验（`jwt-role-claim-tamper-vpe`）
- `if cookies["role"][0] == "user" { reject }` — 多角色 cookie 共存时取首条，可绕过（`multi-role-cookie-low-priority-vpe`）

### 窄化反例（必须避免）

以下是垂直越权维度的典型窄化误判：

- **"前端不显示该入口 → 后端必然校验角色"** — 错。前端按角色渲染菜单不等于后端鉴权，直连 API 即可绕过
- **"已测了 `/api/admin/*` → 其他 admin 接口都有"** — 错。每个 admin 端点的角色校验独立（中间件按路由分组 / handler 内独立判定），需按端点账本逐项测
- **"role 字段不在请求里 → 不能伪造"** — 错。role 可能在 JWT claim / cookie / session / 自定义 header / body 多个位置，需测每个载体；JWT 篡改 / mass-assignment / cookie 注入是常见绕过
- **"已在子系统 A 命中 → 子系统 B 假定同样"** — 错。跨子系统独立结账，不同子系统由不同团队 / 不同时期开发，角色模型可能不同
- **"返回 403 → 已防护"** — 不严谨。需验证是否能用其他方式绕过：底层 API 直调（`/api/v1/admin/*` vs `/v2/...`）、改方法（GET → POST）、改参数（加 role 字段）、改路径编码

## 反例义务（必须遵守）

> **为什么这里是「必须」**：反例义务属于交付契约——"未发现垂直越权"或"角色校验已生效"结论是覆盖完整性的产物声明，缺失反向验证清单会让下游误信"该维度全站安全"。

写"未发现垂直越权"或"角色校验已生效"前，产物必须包含：

- 测过的"应仅高权限可达"端点完整清单（按 sink 语义枚举：管理后台 / 用户管理 / 角色切换 / 配置管理 / 审批 / 数据导出等全部触发线索类别，不按 URL 前缀筛选）
- 每个端点尝试的篡改向量（凭证替换、JWT claim 篡改、cookie / header 注入、body mass-assignment、role 标志置空、多角色共存、版本 / 路径变体）
- 每个端点的对照证据（高权限基线、低权限直访、篡改向量响应、关键管理字段）

清单不完整 → 结论降级为 `partial-coverage` 并显式声明未覆盖范围（例如：仅测了 `/admin/*`、未测配置 / 审批 / 用户管理类）。

## 闭环验证要求（必须遵守）

通用闭环口径见同根目录 `common/closure-verification.md`（技能表 path 列同一抽取根下，需要时 `read_file` 读取）。核心：结论须形成「输入 → 处理 → 真实危害 → 可复核证据」完整证据链；仅凭菜单可见、按钮 200、前端展示等中间信号最多判 `suspected`，证明低权限主体真实读到/操作了高权限数据或能力才判 `confirmed`。

### 实际效果验证方向（至少证明一类）

- **跨权限读取**：低权限主体读取到原本仅管理员/高权限用户可见的数据（如全量用户列表 / 审计日志 / 配置）
- **跨权限执行**：低权限主体执行了原本仅高权限主体可执行的操作（如创建用户 / 改他人角色 / 改配置），并通过回读或业务状态变化证明成功。若该操作为不可逆动作（删除/覆盖/批量改），改用哨兵自证或非破坏差分，不得对真实数据执行（见《破坏性 / 不可逆动作的闭环边界》）
- 若只有菜单可见、按钮返回 200 或前端展示异常，但未证明真实权限突破，不能给 `confirmed`

## 判定标准

| 现象 | 判定 |
|------|------|
| 低权限账号可读取管理数据或执行高权限操作，并通过真实数据/状态变化证明权限突破已生效 | confirmed |
| 响应异常，但尚未证明真实高权限数据暴露或操作已生效 | suspected |
| 低权限账号稳定被拒绝（401/403/权限错误） | not vulnerable |
| 只测了部分高权限端点（如仅 `/admin/*`），未覆盖配置 / 审批 / 用户管理 | `partial-coverage`（不得宣称 safe） |

> **不可逆动作例外**：上表 confirmed 行涉及删除/覆盖/不可逆写时，按 `common/closure-verification.md`《破坏性 / 不可逆动作的闭环边界》——不得对真实业务数据执行，改走哨兵自证或非破坏差分，二者都做不到则降 `suspected`。

## 修复建议

- 服务端按"资源 + 动作"执行强制授权校验，不仅按路由前缀；handler 入口统一调用角色断言
- 敏感操作增加二次确认与审计日志（操作者、角色、来源、参数摘要）
- 角色字段只从服务端 session / 已验证 JWT 取，禁止信任请求体 / cookie / 自定义 header 传入的 role
- mass-assignment 防御：DB 模型绑定白名单字段，禁止把请求体 role / is_admin 直接绑定
- 权限策略集中化管理（策略引擎 / RBAC 中间件），避免散落在控制器中的条件分支遗漏
- JWT 角色 claim 二次校验（签名 + 后端 session 状态对齐）
- 为高权限接口补充"低权限拒绝"自动化测试用例
