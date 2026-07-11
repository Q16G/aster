# 入口点模型细则（entry-point-model）

> 五张模型之一「入口点模型」的细则。入口点是后续所有漏洞维度 source→sink 追踪的**起点**——缺一个入口点 = 那一类漏洞维度在该端点失去 source 锚点。本模型枚举攻击面入口并标注信任边界，**不做漏洞判定**。

## 1. 路由与入口点枚举

按框架 grep 路由声明位置（框架 × 声明位置见 `SKILL.md` §5「路由声明文件」表），穷举端点：

- 每个端点产出：Controller / 方法、HTTP method、URL pattern、命中的中间件、参数列表（每参数标 `source: body|query|form|header|cookie|path`）、是否含资源 ID（`has_resource_id`）。
- **动态注册 / 反射分发路由**：`app.dispatch` 按 action 名反射、注解扫描动态注册等静态枚举不到的，标 `coverage_gap` 记录原因，交下游按位置接力——不假装枚举完整。
- **穷举不省略**：每个入口点独占一行，禁止用"等 / ..."省略。

## 2. 中间件与信任边界

判断 source 是否可控、鉴权是否真实，都依赖"哪些 request 属性是 server-derived（可信）vs client-controlled（不可信）"这一事实。

- **识别信号**：中间件 / 拦截器 / 过滤器注册位置（框架对照见 `SKILL.md` §5「中间件声明位置」）、`@ControllerAdvice`、`app.use`、Filter 链。
- **须产出**：每个中间件的 `名称 / 拦截路径 / 排除路径 / 类型（auth|filter|ratelimit|...）/ 向上下文注入的字段 / 信任边界标注`。
  - 中间件向上下文注入的 `userId` / `roles` 等字段 = server-derived 可信值；请求里同名字段 = client-controlled 不可信值。这条对照直接决定越权 / 认证类维度的 source 判定。

## 3. 逐入口业务语义标注

每个入口点标注其业务语义（登录 / 文件下载 / 订单创建 / ...）与鉴权要求（none|guest|user|admin），供业务模型串流程、供威胁模型汇总攻击面映射。

## 产物落库

写入机器读 `project-framework-analysis.jsonl`（`kind: route` / `kind: middleware`，schema 见 `SKILL.md` §9）与人读 `shared/models/entry-point-model.md`。route 类 `id` 带 `ep-` 前缀全局唯一，`scan_status` 初始 `pending`。

## 反例义务

写"入口点已穷举"前，产物须含：所有路由声明文件已 grep 覆盖的证据（命令 + 命中数）、所有中间件注册位置已识别、动态注册 / 反射加载路径已进 `coverage_gap`。清单不完整 → 结论降级 `partial-coverage`。
