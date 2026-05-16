---
name: security-code-analysis
description: 通用安全代码审计总控 skill，从攻击面、认证授权、ownership、动态 SQL、模板与配置等角度组织静态安全审计，而不只依赖强 sink。
tags: code-audit,security-review,authz,ownership,attack-surface
when-to-use: 当需要做系统性的代码安全审计、认证授权复核、业务逻辑安全排查或为后续 SAST/SSA 编排审计流程时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 安全代码审计总控

## 目标

这个 skill 用来解决一个常见偏差：只围绕危险 API 做扫描，最后漏掉认证绕过、Cookie 伪鉴权、ownership 丢失、IDOR 这类高价值问题。

它是**总控视角**，不是单点规则库。根据项目实际情况判断需要做什么、调用哪些子 skill、以什么顺序推进。

## 可用子 skill

| skill | 适用场景 |
|-------|---------|
| `sast-scan` | 需要建立候选漏洞集、发现强 sink、动态 SQL/模板/配置风险时 |
| `dataflow-analysis` | 出现 `needs_dataflow_confirmation`、session/cookie/request attribute 风险、ownership/IDOR 线索、controller/service/mapper 关系不清楚时 |
| `business-logic-auth-review` | 登录函数语义可疑、管理接口只依赖 body/query/cookie 判权、service/mapper 查询缺少操作者约束、自动规则无法稳定确认但风险很高时 |
| `result-with-file` | 所有分析完成后，将 source/sink/dataflow/verify 持久化为 Markdown 表格文件 |

子 skill 的调用顺序和组合由你根据项目上下文自行判断。不是每个子 skill 都必须调用——按需加载。

### sast-scan 强制要求

sast-scan 必须至少运行一次，覆盖所有介质（源码 + XML + 配置）。read_file 逐文件审查不能替代 SAST 的模式匹配。SAST 规则覆盖的结构化漏洞模式（RCE、SQL 注入、XXE、命令注入、SSRF、XSS 等）不需要 AI 重复检测——AI 的价值在下面的"AI 补充检测面"。

## AI 补充检测面（规则无法覆盖的语义化任务）

SAST 规则覆盖结构化漏洞模式。以下是规则无法覆盖的语义化/非结构化检测任务，必须由 AI 完成：

### 1. 安全中间件覆盖面分析

对项目中的安全 Filter / Interceptor / Middleware：

- 列出其 URL pattern / 拦截路径
- 枚举所有 Controller 端点
- **明确标注哪些端点在安全中间件覆盖范围外**
- 对落在中间件外的端点评估风险（明文传输、缺失认证等）

### 2. 影响推演（因果链）

对已确认的密钥泄露/硬编码发现，推演系统性后果：

- 所有加密密钥已泄露 → 加密传输/存储是否等于明文？→ 独立发现
- 签名密钥已泄露 → 签名验证是否可伪造？→ 独立发现
- 每条因果链如果构成独立攻击面，作为独立发现列出，不折叠进根因

### 3. 全局权限架构评估

不能只找单点 IDOR（规则已覆盖），还要评估：

- 是否存在角色分级（admin/doctor/user）
- 是否存在资源隔离（医生只能看自己的患者）
- 跨角色越权可能性
- 有没有全局的 RBAC 机制

### 4. 密码/凭据存储方案横向对比

对项目中所有角色/实体的密码存储方式做横向对比：

| 角色 | 算法 | 是否加盐 | 盐源 | 是否可逆 | 评估 |

每种不满足安全标准的方案独立列为发现。

### 5. 配置安全审查

对 web.xml / application.yml / properties 等配置：

- security-constraint / transport-guarantee / HTTPS 强制
- session-timeout / session 管理
- error-page / 异常处理
- 数据库凭据存储方式（明文 vs 环境变量 vs KMS）

## 审计关注面

以下是审计需要覆盖的关注面。无论以何种顺序推进，最终结论必须体现对这些面的覆盖。

### 攻击面盘点

至少枚举：

- 登录 / 注册 / 找回密码 / 切换身份
- 管理接口 / 审批接口 / 导入导出 / 批量操作
- 查询接口 / 详情接口 / 多租户接口
- 文件上传 / 下载 / 预览 / 模板渲染
- 动态 SQL / 动态模板 / 反射 / 脚本执行入口

### 认证与授权

对 Java Web 项目，必须完成以下 checklist（即使自动扫描未报出认证授权问题）：

- 所有登录接口
- 所有管理接口
- 所有同时含 owner/operator 与 target/resource 参数的查询接口
- 所有 `session` / `cookie` / `request attribute` 参与身份流转的代码
- 所有 `mapper.xml` 中 `${}`、动态条件、动态排序
- **全局 RBAC 评估**：是否存在角色分级、资源隔离、跨角色越权（不只是单个接口的 IDOR）
- **密码存储方案横向对比**（见"AI 补充检测面 §4"）

### sink 到入口点反查

对每个被命中的 sink（危险 mapper 方法、session 写入、Cookie 判断等），反查所有能到达该 sink 的 controller 入口点。同一个 sink 可能被多个入口点到达，必须全部枚举。

### 覆盖声明

最终结论必须包含覆盖声明：扫描了哪些介质、覆盖了哪些框架信号、存在哪些扫描缺口。不允许在未声明覆盖范围的情况下给出"无漏洞"结论。

## 结论组织方式

审计结论**仅按入口点组织**。每个入口点 = controller 方法 + HTTP method + URL pattern。

攻击面盘点阶段枚举的每个入口点，都必须在结论中出现。如果某入口点经审计无发现，标注 `not_vulnerable` 以证明覆盖范围。

同一个 sink 被多个入口点到达时，每个入口点下都要独立列出该 sink 的风险。入口点内的多条发现按风险等级降序排列（confirmed > needs_review > not_vulnerable）。

结论必须覆盖以下检查面（体现在各入口点的发现中）：

- **认证问题**：登录逻辑异常、session 固定、凭证管理缺陷
- **授权问题**：匿名可达、Cookie 伪鉴权、注解缺失
- **ownership / IDOR**：操作者约束丢失、跨对象越权
- **注入 / 危险 API**：SQL 注入、命令执行、SSTI 等
- **覆盖缺口**：未扫描的介质、未覆盖的框架信号（单独列出，不挂在入口点下）

## 结论完整性约束

- 中间步骤发现的所有问题（包括配置类、低危类）必须出现在最终结论中，不得在汇总时丢弃
- 所有有危害的漏洞都要报（不仅仅是中高危），LOW 级别的发现也必须独立列出
- 若同一根因导致多个独立攻击面（如密钥泄露 → 加密失效 + 签名伪造），每个攻击面独立列出，不折叠
- severity_counts 必须与实际列出的条目数一致

## 通用原则

- 不以单个项目的字段名、方法名、返回文案来定义规则
- 样本项目只用于回归验证，不用于定义规则正文
- 不把“没有扫描命中”误当成“没有认证授权问题”
