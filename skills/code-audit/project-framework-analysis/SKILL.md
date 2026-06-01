---
name: project-framework-analysis
description: 项目框架与攻击面侦察 — 识别技术栈/分层架构、枚举入口点与路由、盘点过滤器/中间件信任边界、梳理认证会话架构与数据模型归属字段，产出供下游 SAST/数据流/认证授权复用的项目框架图。
tags: code-audit,recon,framework,attack-surface,entry-point
when-to-use: 当开始一次全量代码安全审计、需要先识别项目结构与攻击面、为后续分析建立共享侦察上下文时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 项目框架与攻击面侦察

## 角色与目标

你是代码审计的**侦察前哨**。本 skill 在审计早期运行，**不做漏洞判定**，只负责一次性建立结构化的"项目框架图"作为共享上下文，避免后续各分析维度重复盘点框架、入口点、中间件。

产出要回答五个问题：

1. 这是个什么技术栈、什么分层结构的项目？
2. 攻击面有哪些入口点 / 路由？
3. 哪些 request 上下文字段是**服务端注入的可信值**、哪些是**客户端可控**的（信任边界）？
4. 这套框架把标准的 source / sink / 跨过程分发 / 认证与过滤**封装成了哪些自有类与方法**（通用工具按标准类型匹配会看不见的盲区）？
5. 这些产出**对后续分析各有什么影响**（移交清单）？

## 侦察步骤

### a. 技术栈与框架识别

特征文件优先，而非只看扩展名：

| 语言 | 特征文件 | 框架信号 |
|------|---------|---------|
| Java | `pom.xml`、`build.gradle` | Spring / Spring Boot / MyBatis / Shiro / Spring Security / Thymeleaf / Freemarker |
| Go | `go.mod` | gin / echo / beego / gorm / 自定义 middleware |
| Python | `requirements*.txt`、`pyproject.toml` | Flask / Django / FastAPI / Jinja2 |
| JS/TS | `package.json` | Express / Koa / NestJS / SSR 模板 |
| PHP | `composer.json` | Laravel / ThinkPHP / Blade / Twig / Smarty / 原生 |
| C/C++ | `Makefile`、`CMakeLists.txt` | 命令执行 / 内存安全相关 |

输出：语言 + 框架信号 + 可获取到的版本线索。

### b. 架构与分层

- 分层模式：MVC / controller → service → mapper(repository) / 充血或贫血模型
- 路由机制：注解路由 / 集中注册 / 约定式路由
- ORM / 数据访问层：MyBatis mapper、GORM、SQLAlchemy、Eloquent 等
- 模板引擎与渲染位置
- 模块/包结构（多模块、微服务边界）

### c. 入口点 / 路由枚举

用 `rg` 抓路由声明，输出**端点骨架表**：

- Java：`rg "@RequestMapping|@GetMapping|@PostMapping|@PutMapping|@DeleteMapping"`
- Go gin/echo：`rg "\.(GET|POST|PUT|DELETE|Any)\(|router\.|engine\."`
- Flask/FastAPI：`rg "@app\.(route|get|post|put|delete)|@router\."`
- Express：`rg "app\.(get|post|put|delete)|router\.(get|post)"`

重点标注：参数中含 `id` / `@PathVariable` / `@RequestParam` 的资源型端点，**没有 operator 参数的端点恰恰是最高风险的**。

### d. 过滤器 / 中间件 / 拦截器映射（信任边界）

这是本 skill 最关键、后续分析最依赖的一节。对每个安全 Filter / Interceptor / Middleware：

- 拦截路径 / URL 覆盖范围（含 exclude / anon 配置）
- 类型：认证 / 授权 / 日志 / CORS / XSS / 限流 / 多租户
- **向 request 上下文注入了哪些字段**（如 `userId` / `tenantId` / `roles` 写入 `request.setAttribute` / gin context / session）
- 执行顺序
- **信任边界标注**：被注入的字段是 server-derived（可信）还是仍可被客户端覆盖

为什么重要：后续判断 source 是否可控、鉴权是否真实，都依赖"哪些 request 属性可信"这一事实。

### e. 认证授权架构

- 登录 / 注册 / 找回密码入口位置
- 会话策略：HttpSession / 自定义 token / JWT / Cookie
- 鉴权机制：注解（`@PreAuthorize` / `@RequiresRoles`）/ 中间件 / 手写 if-else
- 角色与权限模型：是否有 RBAC、角色分级
- 多租户标识：`tenant_id` / `org_id` 等是否存在及其来源

### f. 数据模型与归属字段

对核心实体：区分

- **owner/operator 字段**：`userId` / `owner_id` / `tenantId` / `principalId`（操作者约束）
- **resource/target 字段**：`resourceId` / `docId` / `orderId`（被操作对象）

供后续 IDOR / ownership 跨层追踪使用。

### g. 扫描面清单

统计并定位：源码文件、XML mapper、配置文件、模板文件、依赖清单文件的数量与目录位置。

### h. 框架封装映射侦察

本步骤回答一个问题：**标准的 source / sink / 跨过程分发 / 认证与过滤，在本项目里被封装成了哪些自有类与方法？** 通用 SAST 规则与污点分析按"标准类型 / 标准边界"匹配，一旦框架把它们包了一层，就会整段看不见——这是系统性盲区，不止 SQL。

按下列类别逐一排查，**每一处实例都要列出，禁止省略 / 抽样**（沿用本 skill 端点枚举的硬规则）。每类按"识别信号 → 为何通用工具会漏 → 侦察须输出什么"组织。

**① Source 封装**

- 识别信号：自定义 Action/Controller 基类的取参方法、表单反射绑定、自研参数解码器 / 包装请求对象。
- 为何漏：通用规则只认标准请求 API（如 `HttpServletRequest` 取参）作为 taint 源，自研取参方法不在 source 列表。
- 须输出：用户输入经哪些自研基类#方法进入（落到具体类#方法）。

**② Sink 封装（按子类型分别列）**

- **SQL**：自研 `PreparedStatement` 子类 / BaseDAO / 跨库包装；以及 `StringBuffer`/`StringBuilder` 拼接后 `prepareStatement(拼接串)` 的"伪安全预编译"。漏因：标准类型不匹配（非 `java.sql.Statement`）+ 拼接发生在 `prepareStatement` 之前，参数化只对 `?` 占位生效。
- **命令执行**：包装 `Runtime`/`ProcessBuilder` 的工具类。漏因：标准执行 API 被自研类名遮蔽。
- **文件上传 / 下载 / 读写**：包装 `File`/IO 的文件服务类（下载 / 展示 / 写入）。漏因：路径拼接点在自研类内部。
- **反序列化入口**：自研 `ObjectInputStream` 子类（如仅校验魔数）。漏因：未匹配标准反序列化 sink，弱校验被误判为防护。
- **模板 / 表达式执行**：自研模板渲染 / 表达式求值封装。漏因：标准模板引擎 API 被包装。
- 须输出：每个自研 sink 类#方法 + 子类型（落到具体）。

**③ 分发 / RPC 边界封装（数据流桥接关键）**

- 通用模式：客户端桩 / 代理对象通过"服务定位器 / 注册表 / 容器 / JNDI 按名 lookup"拿到接口引用并调用，运行期再路由到服务端实现；以及反射式分发器（按 action / 方法 / 命令名反射调用目标）。
- 为何漏：调用点与真正执行点在静态上断开，跨过程 + 动态分发使污点链中断。
- 须输出：**"客户端桩方法 ↔ 服务端实现方法"的对应关系**（以本项目实际用到的定位 / 分发机制为准去填，而非套某框架的固定 API 名），供数据流当作跨过程边界桥接。

**④ 认证 / 会话封装**

- 识别信号：自研登录 / 上下文 Filter，从 Cookie/Header 设置身份；以及空实现 / 恒真的校验（形如 `validateXxx` 直接 `return true`）。
- 为何漏：通用工具默认存在认证边界即视为可信，空实现 / 恒真校验造成信任边界误判。
- 须输出：身份注入点 + 失效 / 恒真校验位置（落到具体类#方法#边界）。

**⑤ 过滤 / 校验封装**

- 识别信号：自研输入过滤器、编解码器。
- 为何漏：无法区分它是"输入转换点（可能改变可控性）"还是"失效防护（看似防护实则无效）"。
- 须输出：每个过滤 / 编解码点，并标注"输入转换点"还是"失效防护"。

## 输出（Markdown）

按 a–h 分节输出。其中三张关键表必须给出：

### 入口点 / 路由清单

| Controller/Handler | 方法 | HTTP Method | URL Pattern | 命中的中间件 | 备注 |
|---|---|---|---|---|---|

枚举到的端点必须完整列出，不得用"等""..."省略。

### 过滤器 / 中间件映射

| 过滤器名 | 拦截路径 | 类型 | 注入字段 | 信任边界(server-derived/client-controlled) |
|---|---|---|---|---|

### 框架封装映射表

对应步骤 h。穷举所有实例、每实例独占一行、不得用"等""略"省略。

| 封装类别 | 功能 | 位置（类#方法） |
|---|---|---|

## 对后续分析的影响 / 移交清单（必须输出）

本节显式声明侦察产出如何被后续分析维度消费，是本 skill 的核心交付物：

| 后续分析维度 | 复用本侦察的哪些产出 | 影响 |
|---|---|---|
| 结构化漏洞扫描 | 语言/框架信号、扫描面清单 | 选规则、构建多介质扫描面、覆盖声明 |
| 数据流 / 污点分析 | 过滤器注入字段、入口点 | 区分 server-derived vs client-derived source，定 taint 起点 |
| 认证授权与 ownership 复核 | 入口点骨架、中间件 URL 覆盖、归属字段、多租户标识 | 喂入端点枚举、授权矩阵、中间件覆盖面分析 |
| 配置与敏感信息检查 | 配置/密钥文件位置 | 定位扫描目标 |
| 依赖 / 供应链检查 | 依赖清单文件 | 定位 SCA 目标 |
| 结构化漏洞扫描 | 框架封装映射表（sink 封装类型） | 对自研 sink 类型补做定向 rg / 规则匹配，把按标准类型漏掉的封装 sink 也纳入候选集 |
| 数据流 / 污点分析 | 框架封装映射表（分发/RPC 边界封装、认证封装/空实现校验） | 把"客户端桩方法 → 服务端实现方法"当作跨过程边界桥接，使 Web 入口 source 连到底层数据访问/执行层 sink；用认证封装/空实现校验修正信任边界判断 |

## 边界与通用原则

- 本 skill **只做侦察，不做漏洞判定**——发现的可疑点交由后续对应分析维度确认
- 不以单个项目的字段名、方法名、返回文案来定义通用结论
- 不在报告中泄露审计目标的真实项目名，用匿名化描述
- 侦察未能覆盖的部分（如动态注册路由、反射加载）显式标注为缺口，不能假装已枚举完整
- 全量审计时作为侦察前哨先运行；用户指定聚焦方向时可跳过

## 附录：通用框架封装案例参考

驱动步骤 h 排查的清单——"应当查什么"。通用、匿名、可持续扩充；用抽象占位（如 `XxxPreparedStatement` / `XxxClient` + `ServiceLocator.lookup`），不引用任何真实项目。遇到新模式时持续追加。

| 案例 | 模式描述 | 识别信号 | 漏检原因 | 侦察应产出 |
|---|---|---|---|---|
| SQL sink 封装 | 自研 `XxxPreparedStatement` 子类，或 `StringBuffer.append` 拼接后 `prepareStatement(拼接串)` 再无参执行 | 类名后缀 PreparedStatement/Statement/Dao；prepareStatement 入参非字面常量 | 规则要求 `java.sql.Statement` 类型；拼接在 prepareStatement 之前，参数化只对 `?` 生效 | 自研 sink 类#方法、拼接位置 |
| 命令执行封装 | 包装 `Runtime`/`ProcessBuilder` 的 `XxxCmdExecutor` | 类名后缀 Executor/Shell/Cmd；内部调用 exec/start | 标准执行 API 被自研类名遮蔽 | 自研执行类#方法、命令参数来源 |
| 文件上传/下载封装 | 包装 `File`/IO 的 `XxxFileService`（下载/展示/写入） | 类名含 File/Download/Upload；内部 `new File(路径拼接)` | 路径拼接点在自研类内部，未匹配标准 sink | 自研文件类#方法、路径来源 |
| 反序列化入口封装 | 自研 `XxxObjectInputStream` 子类（如仅校验魔数） | 继承 `ObjectInputStream`；`resolveClass` 被覆盖但校验弱 | 未匹配标准反序列化 sink；弱校验被误判为防护 | 自研反序列化类#方法、校验是否有效 |
| RPC/服务定位器分发封装 | 客户端桩 `XxxClient` 经 `ServiceLocator.lookup(name)` 拿接口引用调用，运行期路由到服务端实现；或反射式分发器按名调用 | lookup/getService/getBean 按字符串名取引用；按 action/method 名反射 invoke | 调用点与执行点静态断开，跨过程 + 动态分发使污点链中断 | "客户端桩方法 ↔ 服务端实现方法"对应关系 |
| 认证/上下文 Filter 封装 | 自研 `XxxAuthFilter` 从 Cookie/Header 设身份；或 `validateXxx` 直接 `return true` | Filter/Interceptor 写 request 上下文身份字段；校验方法体恒真/空实现 | 默认认证边界即可信，恒真/空实现造成信任边界误判 | 身份注入点、失效/恒真校验位置 |
| 过滤/校验封装 | 自研输入过滤器/编解码器 `XxxFilter`/`XxxCodec` | 统一入口对参数做转换/编解码 | 无法区分"输入转换点"与"失效防护" | 每个过滤/编解码点 + 标注其性质 |
