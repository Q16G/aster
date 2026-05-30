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

产出要回答四个问题：

1. 这是个什么技术栈、什么分层结构的项目？
2. 攻击面有哪些入口点 / 路由？
3. 哪些 request 上下文字段是**服务端注入的可信值**、哪些是**客户端可控**的（信任边界）？
4. 这些产出**对后续分析各有什么影响**（移交清单）？

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

## 输出（Markdown）

按 a–g 分节输出。其中两张关键表必须给出：

### 入口点 / 路由清单

| Controller/Handler | 方法 | HTTP Method | URL Pattern | 命中的中间件 | 备注 |
|---|---|---|---|---|---|

枚举到的端点必须完整列出，不得用"等""..."省略。

### 过滤器 / 中间件映射

| 过滤器名 | 拦截路径 | 类型 | 注入字段 | 信任边界(server-derived/client-controlled) |
|---|---|---|---|---|

## 对后续分析的影响 / 移交清单（必须输出）

本节显式声明侦察产出如何被后续分析维度消费，是本 skill 的核心交付物：

| 后续分析维度 | 复用本侦察的哪些产出 | 影响 |
|---|---|---|
| 结构化漏洞扫描 | 语言/框架信号、扫描面清单 | 选规则、构建多介质扫描面、覆盖声明 |
| 数据流 / 污点分析 | 过滤器注入字段、入口点 | 区分 server-derived vs client-derived source，定 taint 起点 |
| 认证授权与 ownership 复核 | 入口点骨架、中间件 URL 覆盖、归属字段、多租户标识 | 喂入端点枚举、授权矩阵、中间件覆盖面分析 |
| 配置与敏感信息检查 | 配置/密钥文件位置 | 定位扫描目标 |
| 依赖 / 供应链检查 | 依赖清单文件 | 定位 SCA 目标 |

## 边界与通用原则

- 本 skill **只做侦察，不做漏洞判定**——发现的可疑点交由后续对应分析维度确认
- 不以单个项目的字段名、方法名、返回文案来定义通用结论
- 不在报告中泄露审计目标的真实项目名，用匿名化描述
- 侦察未能覆盖的部分（如动态注册路由、反射加载）显式标注为缺口，不能假装已枚举完整
- 全量审计时作为侦察前哨先运行；用户指定聚焦方向时可跳过
