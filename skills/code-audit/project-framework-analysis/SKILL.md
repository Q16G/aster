---
name: project-framework-analysis
description: >-
  白盒建模——一次深度建模产出五张共享模型：项目架构（技术栈/分层/框架自有封装/闭源依赖位置）、
  入口点（路由/中间件信任边界）、认证与权限（会话/角色/多租户/归属字段）、业务（实体/流程状态机/
  业务不变量/高价值资产）、全局威胁（攻击面×漏洞类别的适用性映射，供下游逐一全测）。本能力不做漏洞判定，为后续
  漏洞维度分析建立共享底图。
when-to-use: 当开始一次全量代码安全审计、需要先建立项目架构/入口点/认证权限/业务/全局威胁五张共享模型、为后续分析建立入口时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 白盒建模（五张模型）

> 本能力在审计早期做**一次深度建模**，产出五张共享模型作为后续多维度审计的入口——项目架构 / 入口点 / 认证与权限 / 业务 / 全局威胁。白盒源码全可见，整张攻击面图一次性静态建起并复用，仅项目结构重大变更时重建。**不做漏洞判定**——建模阶段产生的可疑点交对应审计能力消费。

## 1. 触发线索 / 适用信号

按"半径 + 项目体量 + 已有产物 + 信息完备度"四维识别命中场景。

**半径维度**：
- 用户给的目标路径是**项目根目录**（含 `pom.xml` / `go.mod` / `package.json` / `composer.json` / `requirements.txt` / `Cargo.toml` 任一特征文件），不是单文件
- 至少存在 controller 层目录（`controllers/` / `handlers/` / 含 `@RequestMapping` 注解的 Java 文件 / `routes/` / `views/`），项目体量大于单文件

**已有产物维度**：
- `shared/models/` 五张模型 markdown **尚不存在**
- 已有产物但目标项目栈 / 入口点近期发生重大变更（新增模块 / 新增框架 / 切换 ORM）

**信息完备度维度**：
- 多语言混合（前后端同仓 / 微服务异构）
- 多模块（monorepo / 多 maven module / lerna 工作区）
- 用户未在初始描述里给出明确技术栈或入口点

**反向信号**（不命中本能力）：
- 用户已显式给出技术栈 + 已知入口点（直接跳到对应漏洞维度 skill）
- 小修改半径——单文件审计、PR diff、特定函数复核
- 已有同 commit 范围内的 framework 产物（增量审计直接消费现有产物即可）

---

## 2. 造成原因

**本能力 n/a**（原因：本能力是前置侦察，不针对单一漏洞，无成因可写。各漏洞成因由对应 audit skill 在 §2 中独立讲清。）

---

## 3. 领域 source-sink 数据流模型

**本能力 n/a**（原因：侦察阶段不做 source-sink 追踪。本能力只产出"哪里可能是 source、哪里可能是 sink 的封装"这一**位置盘点**，跨函数可达性证明由下游 [dataflow-analysis](../dataflow-analysis/SKILL.md) 接力。）

---

## 4. 常见类型

**本能力 n/a**（原因：本能力不针对漏洞分类，攻击变体由各漏洞维度 skill 自行枚举。）

---

## 5. 入口点定位

> 本能力的"入口点"实际是"侦察起点"——告诉模型从项目里哪些特征文件 / 声明位置进入，把侦察面建立起来。

按项目根的特征文件 + 路由声明 + 中间件声明 + ORM 配置定位起点。

> 下列框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准。

### 项目根特征文件（识别技术栈 + 依赖）

| 语言 | 特征文件 | 同时看什么 |
|---|---|---|
| Java | `pom.xml` / `build.gradle` / `settings.gradle` | dependencies 节、`spring-boot-starter-*` / `mybatis-spring` / `shiro-*` 信号 |
| Go | `go.mod` / `go.sum` | require 节、`gin` / `echo` / `gorm` / `database/sql` 信号 |
| Python | `requirements*.txt` / `pyproject.toml` / `Pipfile` | `flask` / `django` / `fastapi` / `sqlalchemy` / `jinja2` 信号 |
| JS / TS | `package.json` / `pnpm-workspace.yaml` / `lerna.json` | dependencies 节、`express` / `koa` / `@nestjs/*` / `next` / `sequelize` / `typeorm` 信号 |
| PHP | `composer.json` | `laravel/framework` / `topthink/think` / `symfony/*` 信号 |
| C / C++ | `Makefile` / `CMakeLists.txt` / `meson.build` | 链接的库（OpenSSL / libcurl / 框架库） |
| Rust | `Cargo.toml` | `actix-web` / `axum` / `rocket` / `tokio` 信号 |

### 路由声明文件（找入口点）

按框架 grep 路由声明位置：

- Spring：`rg "@RequestMapping|@GetMapping|@PostMapping|@PutMapping|@DeleteMapping|@PatchMapping"`
- Gin / Echo：`rg "\.(GET|POST|PUT|DELETE|PATCH|Any)\(|router\.|engine\.Group"`
- Flask / FastAPI：`rg "@app\.(route|get|post|put|delete)|@router\.(get|post|put|delete)"`
- Django：读 `urls.py` 找 `path(...)` / `re_path(...)` 声明
- Express / Koa：`rg "app\.(get|post|put|delete|use)|router\.(get|post|put|delete)"`
- NestJS：`rg "@(Get|Post|Put|Delete|Patch|Controller)\("`
- Laravel：读 `routes/web.php` / `routes/api.php`
- ThinkPHP：约定式路由按控制器目录

### 中间件 / 拦截器 / 过滤器声明位置

- Spring：`@WebFilter` / `@Component` 实现 `Filter` / `HandlerInterceptor` / `@ControllerAdvice` / Spring Security 配置类
- Gin：`engine.Use(...)` / `group.Use(...)` 注册的 handler
- Django：`settings.py` 的 `MIDDLEWARE` 列表
- Express：`app.use(...)` / `router.use(...)`
- Laravel：`app/Http/Kernel.php` 的 `$middleware` / `$routeMiddleware`

### ORM / 数据访问层配置

- MyBatis：`*Mapper.xml` 位置、`@Mapper` 注解扫描包配置
- Gorm：`*.go` 里的 `db.AutoMigrate(...)` 调用
- SQLAlchemy / Django ORM：`models.py` 位置
- Sequelize / TypeORM：`models/` 目录、`ormconfig.json`

---

## 6. 跨框架代码变体

> 本能力在不同框架下的侦察起点对照——每语言 × 框架的特征文件 + 框架信号 + 关注介质。**目的是让本能力在任意项目栈都能用**。

| 语言 | 框架 | 特征文件信号 | 必入侦察面的介质 |
|---|---|---|---|
| Java | Spring Boot | `pom.xml` 含 `spring-boot-starter-web` | `*Controller.java`、`application*.yml`、`templates/` |
| Java | Spring MVC | `web.xml` 配 `DispatcherServlet` | `*Controller.java`、`WEB-INF/`、`spring-*.xml` |
| Java | MyBatis | `pom.xml` 含 `mybatis-spring` 或 `mybatis-plus` | `**/*Mapper.xml`、`@Mapper` 接口、`@Select` / `@Update` 注解 |
| Java | Shiro / Sa-Token | `pom.xml` 含 `shiro-spring` / `sa-token-*` | Shiro 配置类、`@RequiresPermissions` / `@SaCheckLogin` 注解位置 |
| Java | Thymeleaf / Freemarker | `pom.xml` 含 `thymeleaf` / `freemarker` | `templates/*.html` / `*.ftl` 模板目录 |
| Go | Gin | `go.mod` 含 `gin-gonic/gin` | `router*.go`、`main.go`、middleware 注册位置 |
| Go | Echo | `go.mod` 含 `labstack/echo` | 路由注册文件、middleware |
| Go | Beego | `go.mod` 含 `beego/beego` | `routers/router.go`、controllers/ |
| Go | Gorm | `go.mod` 含 `gorm.io/gorm` | 含 `db.Raw` / `db.Exec` 的文件、模型定义 |
| Go | 自定义 middleware | 项目内 middleware 包 | middleware 注册顺序与覆盖范围 |
| Python | Flask | `requirements.txt` 含 `flask` | `app.py` / `views.py`、`templates/`（Jinja2） |
| Python | Django | `manage.py` + `settings.py` | `urls.py`、`views.py`、`models.py`、`MIDDLEWARE` 配置 |
| Python | FastAPI | `requirements.txt` 含 `fastapi` | router 文件、`main.py`、Pydantic 模型 |
| Python | SQLAlchemy | `requirements.txt` 含 `sqlalchemy` | 模型定义、含 `session.execute` / `text()` 的文件 |
| Python | Jinja2 | 模板文件 `.html` / `.j2` | `templates/` 目录 |
| JS / TS | Express | `package.json` 含 `express` | `routes/*.js`、`app.js`、`middlewares/` |
| JS / TS | Koa | `package.json` 含 `koa` | router 文件、`koa-*` 中间件 |
| JS / TS | NestJS | `package.json` 含 `@nestjs/core` | `*.controller.ts`、`*.module.ts`、`*.guard.ts` |
| JS / TS | Next.js SSR | `next.config.js` 存在 | `pages/api/*` / `app/api/*` 路由、SSR getServerSideProps |
| PHP | Laravel | `composer.json` 含 `laravel/framework` | `routes/web.php`、`routes/api.php`、`app/Http/` |
| PHP | ThinkPHP | `composer.json` 含 `topthink/think` | `application/` 或 `app/` 目录、约定式路由 |
| PHP | Blade / Twig | 模板文件 `.blade.php` / `.twig` | `resources/views/` |
| C / C++ | 通用 | `Makefile` / `CMakeLists.txt` | 命令执行点（`system` / `popen`）、内存安全点、链接的第三方库 |

> 详细的侦察维度展开（每维度的识别细则、grep pattern、易漏点）见 [references/architecture-model.md](references/architecture-model.md)。

---

## 7. 思考检查点

加载本 skill 时按这些问题思考——按"待回答的核心问题"驱动侦察展开：

- 这个项目是什么技术栈、什么分层结构？（MVC / 充血贫血 / 微服务边界 / monorepo）
- 攻击面入口点有哪些？路由声明在哪个文件里？有没有动态注册的路由 / 反射加载？
- request 上下文里**哪些字段是服务端注入的可信值**、**哪些是客户端可控**？中间件向上下文写入了什么？
- 框架把 source / sink / 跨过程分发 / 认证 / 过滤封装成了哪些自有类与方法？（通用 SAST 工具按标准类型匹配会看不见的盲区）
- 依赖里哪些是闭源 / 可反编译 / 混淆？落在攻击面的什么位置（是否承载鉴权 / 是否在污点必经路径上）？
- 项目里的核心业务实体的归属字段（`user_id` / `tenant_id` / `owner_id`）在哪些表 / 哪些字段？

---

## 8. 建模方法论

> 本能力**只到建模产物**——跨函数追踪 / sink 可达性证明走 [dataflow-analysis](../dataflow-analysis/SKILL.md) 与对应单漏洞维度 skill。本节是五张模型的建模骨架；每张模型的判据、易漏点、产物字段见对应 reference。

### 五张模型总览

一次深度建模，按下列顺序建五张模型（前四张是事实盘点，第五张综合前四张）：

| 模型 | 承载维度 | 细则 |
|------|---------|------|
| 项目架构模型 | 技术栈 / 分层 / 框架自有封装 / 闭源依赖 | [architecture-model.md](references/architecture-model.md) |
| 入口点模型 | 路由枚举 / 中间件信任边界 / 业务语义标注 | [entry-point-model.md](references/entry-point-model.md) |
| 认证与权限模型 | 会话 / 角色分级 / 多租户 / 归属字段 | [auth-model.md](references/auth-model.md) |
| 业务模型 | 实体关系 / 流程状态机 / 业务不变量 / 高价值资产 | [business-model.md](references/business-model.md) |
| 全局威胁模型 | 攻击面 × 漏洞类别的全量适用性映射（供下游逐一全测） | [threat-model.md](references/threat-model.md) |

### 建模维度（非编号——按"待回答问题"组织，结合代码事实展开顺序）

每维度按"识别信号 → 为什么这维度重要 → 须产出什么"组织；下列维度按上表归入五张模型。

**技术栈与框架识别**

- 识别信号：§5 项目根特征文件 + dependencies 节信号
- why：后续所有维度都依赖准确的栈识别——把 Spring 项目当 Django 审计、漏掉 MyBatis XML 介质，后续整条链路失真
- 须产出：语言 + 框架信号 + 版本线索

**架构分层**

- 识别信号：目录命名（`controller/` / `service/` / `mapper/` / `dao/` / `repository/`）、注解模式（`@Service` / `@Repository`）、模块边界
- why：分层决定 source 入口位置与 sink 集中位置——MVC 项目 source 在 Controller，sink 在 Mapper；微服务项目要识别服务边界以确定本服务范围
- 须产出：分层模式 + 模块/微服务边界

**路由与入口点枚举**

- 识别信号：§5 路由声明文件 grep 结果
- why：入口点是后续所有漏洞维度 source-to-sink 追踪的起点——缺一个入口点 = 那一类漏洞维度在该端点失去 source 锚点
- 须产出：端点骨架表（Controller / 方法 / HTTP method / URL pattern / 命中的中间件 / 参数列表）

**中间件与信任边界**

- 识别信号：§5 中间件声明位置 + 拦截器配置 + `@ControllerAdvice` / Filter / `app.use` 注册
- why：判断 source 是否可控、鉴权是否真实，都依赖"哪些 request 属性是 server-derived（可信）vs client-controlled（不可信）"这一事实；中间件向上下文注入的字段直接决定信任边界
- 须产出：过滤器/中间件映射表（名称 / 拦截路径 / 类型 / 注入字段 / 信任边界标注）

**认证与会话架构**

- 识别信号：登录端点位置、Token 类（JWT / Cookie / 自定义）、`@PreAuthorize` / `@RequiresRoles` 注解、Spring Security 配置
- why：认证逻辑决定哪些端点匿名可达、哪些端点需要角色校验——这是越权类漏洞维度的前提
- 须产出：会话策略 + 鉴权机制 + 角色权限模型 + 多租户标识

**数据模型归属字段**

- 识别信号：实体类字段命名（`userId` / `owner_id` / `tenant_id` / `org_id` / `principalId`）、SQL 表 schema
- why：IDOR / ownership / 越权类审计依赖 owner ↔ resource 字段对照——侦察阶段沉淀这套对照表，下游能力按字段名直接追踪
- 须产出：owner/operator 字段集合 + resource/target 字段集合 + 多租户字段

**框架封装的 source / sink / 自定义工具类**

- 识别信号：自定义 Controller 基类的取参方法、`StringBuffer` 拼接后 `prepareStatement` 的伪安全预编译、包装 `Runtime` / `ProcessBuilder` 的工具类、自研 `ObjectInputStream` 子类、自研模板渲染封装、自研登录 Filter 与恒真校验、自研输入过滤器/编解码器
- why：通用 SAST 规则只认标准 API——自研封装把标准 source / sink 包了一层，按标准类型匹配会整段看不见；这是系统性盲区。详细分类（① Source 封装 / ② Sink 封装（SQL / 命令 / 文件 / 反序列化 / 模板）/ ③ 分发 / RPC 边界封装 / ④ 认证 / 会话封装 / ⑤ 过滤 / 校验封装）见 [references/architecture-model.md](references/architecture-model.md)
- 须产出：框架封装映射表（封装类别 / 功能 / 类#方法位置）

**依赖结构与闭源 / 混淆识别**

- 识别信号：依赖清单（`pom.xml` / `go.mod` / `package.json` / `requirements.txt` / `composer.json`）+ 命名空间（公共 vs 私有）+ MANIFEST/Vendor/groupId/LICENSE 归属信号 + 类名混淆特征（`a.a.a` / `o0Oo`）
- why：闭源依赖如果承载鉴权 / 过滤 / sink 逻辑，标"未审"会让结论失真——需要标注其在攻击面的位置以决定是否反编译。详细分类判据（公共库 / 无源码可反编译 / 混淆 / unknown）与归属（同厂商自有 / 第三方异厂商商业 / unknown）规则见 [references/architecture-model.md](references/architecture-model.md)
- 须产出：依赖图谱表（坐标 / 分类 / 归属 / 有无可读源码 / 相对入口/边界位置 / 是否关键路径 / 处置建议）

**业务建模（业务模型）**

- 识别信号：领域实体类 / 表 schema、跨多方法的业务流程、实体 status 字段与流转代码、数值 / 配额 / 归属约束的校验位置
- why：语义型漏洞（越权 / 逻辑 / 状态机 / RBAC / 影响放大）的判据基准——没有业务不变量，逻辑漏洞只能靠临场直觉
- 须产出：核心实体与关系 + 关键流程与状态机合法迁移 + 业务不变量清单（含"在哪校验 / 是否校验"）+ 高价值资产与影响放大链路。细则见 [business-model.md](references/business-model.md)

**威胁建模（全局威胁模型 = 攻击面映射）**

- 识别信号：前四张模型的汇聚——入口点 × 业务语义 × sink 位置 × 可达身份
- why：把前四张模型的事实汇总为统一的攻击面映射，供下游各维度**按接口逐一取用、逐一全测**；本模型只供信息，不排优先级、不做维度取舍、不指挥先测后测
- 须产出：攻击面清单 + 攻击面 × 漏洞类别的**全量适用性映射**（每个接口的每个结构上可能成立的维度都登记，`rationale` 陈述该维度在此接口成立的事实依据）。某维度在某接口结构上不可能成立可作事实记录，但不表述为 out-of-scope、不作为下游省略其他维度的依据。细则见 [threat-model.md](references/threat-model.md)

**产物落库**

- 详见下方"产物结构"段——建模的最终产物形式

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标项目动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 项目根特征文件已识别，技术栈 + 框架信号 + 版本线索完整
- [ ] 路由声明文件已 grep 覆盖，端点骨架表完整列出（不省略）
- [ ] 中间件声明位置已识别，注入字段与信任边界标注到位
- [ ] 认证 / 会话架构已识别，恒真校验 / 空实现已标注
- [ ] 核心实体的 owner 字段 / resource 字段已列出
- [ ] 框架封装映射表覆盖 ①-⑤ 五个类别
- [ ] 依赖图谱覆盖所有依赖，每条标注分类 + 归属 + 关键路径位置
- [ ] 关键路径上的闭源依赖（含 `unknown`）已写入 `task_context.md` 作为前提变化
- [ ] 侦察未能覆盖的部分（动态注册路由 / 反射加载）显式标为缺口，不假装枚举完整

---

## 9. 闭环要求（必须遵守）

**本能力 n/a**（原因：本能力不做漏洞判定，无 `confirmed` / `suspected` / `not_vulnerable` 概念。但**产物契约**仍是交付契约——侦察缺漏会让下游所有维度失去锚点，因此独立成"产物结构"段写在本节末尾。）

### 产物结构（必须遵守）

> **为什么这里是「必须」**：产物结构是下游审计能力按入口点对账的接口。下游 [dataflow-analysis](../dataflow-analysis/SKILL.md) / [sast-scan](../sast-scan/SKILL.md) / 各单漏洞维度 skill 都按本产物的入口点清单展开攻击面覆盖；**省略一个入口点 = 一类漏洞维度在该端点失去 source 锚点**，整条链路失真。产物落库规范见 [common/result-with-file](../../common/result-with-file/SKILL.md)。

**产物形态**：五张模型各落一个 markdown，无 JSONL 并存：

- `shared/models/architecture-model.md` — 技术栈 / 分层 / 框架封装（source/sink/分发/过滤/认证 wrapper）/ 依赖与闭源识别
- `shared/models/entry-point-model.md` — 入口点骨架表 + 中间件信任边界表
- `shared/models/auth-model.md` — 认证机制 + 归属字段对照表（owner/resource/tenant）
- `shared/models/business-model.md` — 业务实体 / 流程状态机 / 业务不变量表
- `shared/models/threat-model.md` — 攻击面 × 漏洞维度全量适用性映射表（`attack_surface / vuln_class / rationale`，不含 priority / in_scope）

各模型内的信息用 markdown 表承载（每张表的列见对应 `references/<model>.md` 的「产物落库」段）；入口点 `id` 带 `ep-` 前缀全局唯一、覆盖态初始 `pending`。

> 注意：`shared/models/*.md` 是**本次审计的运行时产物**；skill 目录下 `references/*.md` 是**静态方法论细则**，二者同名不同物。

**落盘硬约束**：五张模型必须各自落 `shared/models/` 对应文件，**禁止只写在 task_context.md 或仅口头描述**；任一模型文件缺失 = 该模型未完成，结论降级 `partial-coverage`。

表约束：

- 一入口 / 一实体 / 一不变量 / 一攻击面映射独占**一行**——**禁止**用"等" / "..." / "（其余 N 条略）"省略
- 入口点穷举不省略——why：下游各漏洞维度按入口点对账，省略一个入口点会让该入口点对应的整类漏洞维度失去 source 锚点
- 威胁映射表只含 `attack_surface / vuln_class / rationale`，**不含** priority / in_scope——本能力不排优先级、不做取舍

**就地更新（必须遵守）**：

> why：重跑建模不应重置已有的扫描进度——下游能力可能已将部分入口点覆盖态更新为 `in_progress` / `completed`，整文件覆盖写会破坏跨批次状态追踪。

模型文件已存在时，按表行主键（入口点 `id` / 实体字段 / 攻击面单元）就地更新或追加新行；已有行的覆盖态原样保留、不重置，不删旧行。

**反例义务**（必须遵守）：

> why：写"已完整盘点框架"前必须有反向覆盖证据——缺失会让下游误信"该范围内攻击面已穷举"。

写"建模已完成"结论前，五张模型的产物必须包含：

- 所有路由声明文件已 grep 覆盖的证据（grep 命令 + 命中数）
- 所有中间件注册位置已识别（middleware 数 + 拦截范围 + 注入字段信任边界）
- 所有依赖已分类（公共库 / 无源码 / 混淆 / `unknown` 数量分布）
- 业务模型：核心流程 / 状态机 / 业务不变量已枚举，每条不变量标注"在哪校验 / 是否校验"
- 威胁模型：攻击面 × 漏洞类别的全量适用性映射已产出，每个接口 × 每个可能维度都登记，不省略、不排除、不排优先级
- 任一未覆盖的范围（动态注册路由 / 反射加载 / 闭源依赖内部）在对应模型的「缺口」段显式列出原因

清单不完整 → 结论降级为 `partial-coverage`，且必须在对应模型的 `shared/models/<model>.md` "缺口"段显式列出未覆盖范围。

---

## 10. 具象化反例库

**本能力 n/a**（原因：本能力不做漏洞判定，无 FP / FN 概念。各漏洞维度的反例库由对应 audit skill 在 §10 中独立写。）

---

## 11. 静态分析边界

**本能力 n/a**（原因：本能力是侦察阶段，不涉及静态分析的可达性证明边界。反射调用 / 闭源依赖 / 动态字符串构造等静态分析边界由 [dataflow-analysis](../dataflow-analysis/SKILL.md) 与下游单漏洞维度 skill 在 §11 中独立写。本能力对这些情形的处置是**侦察阶段标注其位置**——反射点 / 闭源依赖 / 动态注册路由进 `coverage_gap` 记录，供下游能力按位置接力。）

---

## 12. 修复建议

**本能力 n/a**（原因：本能力不做漏洞判定，无修复对象。各漏洞维度的修复建议由对应 audit skill 在 §12 中独立写。）
