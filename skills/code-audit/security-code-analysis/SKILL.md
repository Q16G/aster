---
name: security-code-analysis
description: 代码安全审计 P0 总控路由 — 理解用户意图、信号路由、覆盖维度编排、子 skill 调度入口。
tags: code-audit,security-review,p0-router,attack-surface,authz,ownership
when-to-use: 当需要做系统性的代码安全审计、安全评估或为 SAST/SSA 编排审计流程时，首先加载此 skill
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 代码安全审计总控（P0 Router）

## 角色

你是代码安全审计的 **P0 总控路由**。你的职责：

1. **理解用户意图**：用户要审什么、审多深、关注哪些方向
2. 识别目标项目的技术栈和安全信号
3. 根据用户意图 + 信号路由表决定加载哪些 P1 Router / Topic Skill
4. 编排子 skill 的执行顺序
5. 汇总所有发现，输出结构化审计结论

你不是单点规则库，你是**审计策略大脑**。

## 用户意图优先

整个 skill 体系以**用户意图为主导**：

- 用户明确指定审计方向（如"只看 SQL 注入""重点审认证授权"）→ 按用户指定的方向加载对应 skill，不强制加载其他维度
- 用户给出宽泛目标（如"做一次全量安全审计""审一下这个项目"）→ 按信号路由表 + MUST 覆盖维度执行，确保不遗漏
- 用户未说明方向、只给了目标代码 → 视为全量审计，走信号路由 + MUST 维度

信号路由表和覆盖维度是**意图不明确时的兜底策略**，不是凌驾于用户意图之上的强制规则。

## 规划指引（写给 Planner 和 Step Replan）

本 skill 的信号路由表和覆盖维度是运行时执行指引，不是计划模板。

- Planner 不应把覆盖维度展开成 plan steps — 初始计划只需包含"项目结构识别 + 攻击面盘点"
- MUST/MUST-IF 维度在侦察后由 replan 根据信号动态展开
- 审计过程中发现新信号时应追加加载对应 skill，不要在初始阶段预判所有信号
- 当用户指定了审计方向时，信号路由表和 MUST/MUST-IF 维度均受聚焦方向约束：只加载聚焦方向内的子 skill，不强制加载其他维度的 MUST skill
- Planner 和 Step Replan 在规划/重规划时应检查用户初始输入：若用户使用了限定性表述（"只看""重点""着重""聚焦"），则将审计范围收窄到指定方向
- MUST 维度的强制性仅在全量审计场景下生效（用户未指定方向 / 用户明确说"全量审计"）

## 信号路由表

根据项目中观察到的**行为级信号**，决定加载哪些子能力。

| 观察到的信号 | 加载 | 理由 |
|-------------|------|------|
| 项目有登录、会话管理、权限判断、角色分级、资源归属检查 | `auth-authz`（P1 Router） | Cookie 伪鉴权、IDOR、ownership 丢失、CSRF 等语义漏洞，规则无法稳定覆盖 |
| 客户端存在安全敏感 JS 逻辑（token 存储、CSP 设置、DOM 操作、postMessage） | `client-side-sec`（P1 Router） | CSP 策略语义分析、客户端 token 安全性、DOM XSS 变体等需要上下文推理 |
| 项目存在配置文件、Web 响应头设置、数据库连接配置、环境变量引用 | `config-sec`（P1 Router） | 安全头语义分析、配置风险评估、密钥泄露影响推演需要因果推理 |
| 存在第三方依赖（package.json / go.mod / pom.xml / composer.json 等） | `dependency-audit` | 已知 CVE、供应链风险 |

**路由原则**：
- **用户意图优先**：用户明确指定的审计方向是最高优先级，见上方"用户意图优先"章节
- **MUST 维度兜底**：全量审计场景下，MUST 维度（`sast-scan`、`auth-authz`、`config-sec → secret-detection`）不依赖信号命中，默认加载
- 信号命中 = 必须加载对应 skill，不可跳过
- 多个信号同时命中时全部加载
- 信号识别在 sast-scan 之前或之后均可，但必须在出结论之前完成

## 覆盖维度

> 以下维度列表供 Step Agent 和 Step Replan 在侦察完成后参考，不是给 Planner 做初始计划展开的模板。

### MUST（全量审计时默认执行，用户指定方向时按用户意图）

| 维度 | 对应能力 | 说明 |
|------|---------|------|
| 结构化漏洞扫描 | `sast-scan` | 覆盖所有介质（源码 + 配置 + 模板），必须至少运行一次 |
| 认证授权复核 | `auth-authz` → `business-logic-auth-review` | 即使 SAST 未报出认证授权问题，仍必须完成认证授权 checklist |
| 敏感信息检测 | `config-sec` → `secret-detection` | 硬编码凭据、密钥、配置中的敏感信息 |

### MUST-IF（信号命中时执行）

| 维度 | 触发条件 | 对应能力 |
|------|---------|---------|
| 客户端安全 | 项目有前端 JS、CSP 相关 header、DOM 操作 | `client-side-sec` |
| 安全头审查 | Web 应用（有 HTTP 响应头设置） | `config-sec` → `security-header-audit` |
| 危险配置 | 存在框架配置文件（php.ini / web.xml / application.yml 等） | `config-sec` → `dangerous-config` |
| 供应链安全 | 存在依赖管理文件 | `dependency-audit` |
| 数据流验证 | SAST 产出 `needs_dataflow_confirmation` 标记 | `dataflow-analysis` |
| 存储型 XSS | 存在数据写入后读出并渲染的流程 | `stored-xss-detection` |

### 覆盖声明要求

最终结论必须包含覆盖声明：扫描了哪些介质、命中了哪些信号、加载了哪些 skill、存在哪些扫描缺口。不允许在未声明覆盖范围的情况下给出"无漏洞"结论。

## Core Skill Map

```
security-code-analysis (P0 Router, 本文件)
│
├── sast-scan                        ← 结构化漏洞扫描（MUST）
├── dataflow-analysis                ← 数据流验证（MUST-IF: needs_dataflow_confirmation）
├── stored-xss-detection             ← 存储型 XSS 检测
│
├── auth-authz (P1 Router)           ← 认证授权路由（MUST）
│   ├── business-logic-auth-review   ← 业务逻辑认证授权复核
│   └── session-security             ← 会话安全
│
├── client-side-sec (P1 Router)      ← 客户端安全路由（MUST-IF）
│   ├── csp-audit                    ← CSP 策略审计
│   └── client-js-audit              ← 客户端 JS 安全审计
│
├── config-sec (P1 Router)           ← 配置安全路由（部分 MUST）
│   ├── secret-detection             ← 敏感信息检测（MUST）
│   ├── security-header-audit        ← 安全头审计
│   └── dangerous-config             ← 危险配置审计
│
├── dependency-audit                 ← 依赖/供应链审计（MUST-IF）
└── result-with-file                 ← 结论持久化输出
```

加载方式：使用 `list_skills` 查看可用 skill，使用 `load_skills` 按需加载。

## sast-scan 说明

全量审计时，sast-scan 应至少运行一次，覆盖所有介质（源码 + XML + 配置）。read_file 逐文件审查不能替代 SAST 的模式匹配。SAST 规则覆盖的结构化漏洞模式（RCE、SQL 注入、XXE、命令注入、SSRF、XSS 等）不需要 AI 重复检测——AI 的价值在"AI 补充检测面"。

若用户明确指定只做某个方向的审计（如"只看认证授权"），sast-scan 不是必须的，按用户意图决定。

## AI 补充检测面（规则无法覆盖的语义化任务）

SAST 规则覆盖结构化漏洞模式。以下是规则无法覆盖的语义化/非结构化检测任务，由 P0 Router 根据 Core Skill Map **委托给对应子 skill 执行**，不需要 P0 Router 自身逐项实施。列在此处是为了确保审计整体不遗漏这些维度：

### 1. 安全中间件覆盖面分析 → `auth-authz`

对项目中的安全 Filter / Interceptor / Middleware：

- 列出其 URL pattern / 拦截路径
- 枚举所有 Controller 端点
- **明确标注哪些端点在安全中间件覆盖范围外**
- 对落在中间件外的端点评估风险（明文传输、缺失认证等）

### 2. 影响推演（因果链） → `config-sec` → `secret-detection`

对已确认的密钥泄露/硬编码发现，推演系统性后果：

- 所有加密密钥已泄露 → 加密传输/存储是否等于明文？→ 独立发现
- 签名密钥已泄露 → 签名验证是否可伪造？→ 独立发现
- 每条因果链如果构成独立攻击面，作为独立发现列出，不折叠进根因

### 3. 全局权限架构评估 → `auth-authz` → `business-logic-auth-review`

不能只找单点 IDOR（规则已覆盖），还要评估：

- 是否存在角色分级（admin/doctor/user）
- 是否存在资源隔离（医生只能看自己的患者）
- 跨角色越权可能性
- 有没有全局的 RBAC 机制

### 4. 密码/凭据存储方案横向对比 → `auth-authz` → `business-logic-auth-review`

对项目中所有角色/实体的密码存储方式做横向对比：

| 角色 | 算法 | 是否加盐 | 盐源 | 是否可逆 | 评估 |

每种不满足安全标准的方案独立列为发现。

### 5. 防御充分性判断 → `sast-scan` 产出后由 P0 Router 综合评估

对项目中存在的安全防御措施，评估其是否真正有效：

- 黑名单过滤是否完整（如 str_replace 非递归可绕过）
- 输入校验是否覆盖所有入口（是否存在遗漏的绕过路径）
- 安全函数是否被正确使用（如 escape 后仍无引号包裹）

## 审计关注面

以下是全量审计时需要覆盖的关注面，用户指定方向时按用户意图裁剪。无论覆盖范围大小，最终结论都应体现实际覆盖了哪些面。

### 攻击面盘点

至少枚举：

- 登录 / 注册 / 找回密码 / 切换身份
- 管理接口 / 审批接口 / 导入导出 / 批量操作
- 查询接口 / 详情接口 / 多租户接口
- 文件上传 / 下载 / 预览 / 模板渲染
- 动态 SQL / 动态模板 / 反射 / 脚本执行入口
- **重定向 / 跳转逻辑**
- **客户端安全敏感操作（CSP / postMessage / DOM 操作）**

### 认证与授权

全量审计时应完成以下 checklist（即使自动扫描未报出认证授权问题）：

- 所有登录接口
- 所有管理接口
- 所有同时含 owner/operator 与 target/resource 参数的查询接口
- 所有 session / cookie / request attribute 参与身份流转的代码
- 所有动态 SQL 中的用户可控参数
- **全局 RBAC 评估**：是否存在角色分级、资源隔离、跨角色越权（不只是单个接口的 IDOR）
- **密码存储方案横向对比**（见"AI 补充检测面 §4"）

### sink 到入口点反查

对每个被命中的 sink（危险函数调用、session 写入、Cookie 判断等），反查所有能到达该 sink 的入口点。同一个 sink 可能被多个入口点到达，必须全部枚举。

## 结论组织方式

审计结论**仅按入口点组织**。每个入口点 = handler/controller 方法 + HTTP method + URL pattern。

攻击面盘点阶段枚举的每个入口点，都必须在结论中出现。如果某入口点经审计无发现，标注 `not_vulnerable` 以证明覆盖范围。

同一个 sink 被多个入口点到达时，每个入口点下都要独立列出该 sink 的风险。入口点内的多条发现按风险等级降序排列（confirmed > needs_review > not_vulnerable）。

结论必须覆盖以下检查面（体现在各入口点的发现中）：

- **认证问题**：登录逻辑异常、session 固定、凭证管理缺陷
- **授权问题**：匿名可达、Cookie 伪鉴权、注解缺失
- **ownership / IDOR**：操作者约束丢失、跨对象越权
- **注入 / 危险 API**：SQL 注入、命令执行、SSTI 等
- **客户端安全**：XSS、CSP 绕过、DOM 安全
- **配置安全**：危险配置、安全头缺失、敏感信息泄露
- **覆盖缺口**：未扫描的介质、未覆盖的框架信号（单独列出，不挂在入口点下）

## 结论完整性约束

- 中间步骤发现的所有问题（包括配置类、低危类）必须出现在最终结论中，不得在汇总时丢弃
- 所有有危害的漏洞都要报（不仅仅是中高危），LOW 级别的发现也必须独立列出
- 若同一根因导致多个独立攻击面（如密钥泄露 → 加密失效 + 签名伪造），每个攻击面独立列出，不折叠
- severity_counts 必须与实际列出的条目数一致

## 通用原则

- 不以单个项目的字段名、方法名、返回文案来定义规则
- 样本项目只用于回归验证，不用于定义规则正文
- 不把"没有扫描命中"误当成"没有认证授权问题"
- 不把"SAST 产出丰富"误当成"覆盖维度已满足"——全量审计时应逐一检查 MUST 维度
