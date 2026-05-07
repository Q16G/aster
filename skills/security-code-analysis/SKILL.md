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

它的职责是：

1. 先盘攻击面和权限边界
2. 再决定如何调用 `sast-scan`
3. 命中需要时再调 `dataflow-analysis`
4. 必要时再补 `business-logic-auth-review`

也就是说，它是**总控视角**，不是单点规则库。

## 固定工作流

### 1. 攻击面盘点（必须做）

至少枚举：

- 登录 / 注册 / 找回密码 / 切换身份
- 管理接口 / 审批接口 / 导入导出 / 批量操作
- 查询接口 / 详情接口 / 多租户接口
- 文件上传 / 下载 / 预览 / 模板渲染
- 动态 SQL / 动态模板 / 反射 / 脚本执行入口

### 2. 认证与授权盘点（必须做）

对 Java Web 项目，必须完成以下 checklist：

- 所有登录接口
- 所有管理接口
- 所有同时含 owner/operator 与 target/resource 参数的查询接口
- 所有 `session` / `cookie` / `request attribute` 参与身份流转的代码
- 所有 `mapper.xml` 中 `${}`、动态条件、动态排序

即使自动扫描没有直接报出认证授权问题，也必须人工完成这组 checklist。

### 3. 多介质 SAST

调用 `sast-scan` 建立候选集，并要求其输出：

- 扫描面覆盖声明
- 高置信结果
- 需要数据流确认的结果
- 高噪声分桶

### 4. 按需调用数据流分析

当出现以下任一情况时，继续调用 `dataflow-analysis`：

- `needs_dataflow_confirmation`
- session / cookie / request attribute 风险
- ownership / IDOR / authz 线索
- controller/service/mapper 关系不清楚

### 5. 按需调用业务逻辑专项复核

若发现以下信号，再调用 `business-logic-auth-review`：

- 登录函数成功/失败逻辑可疑
- 管理接口只依赖 body/query/cookie 判断权限
- service / mapper 查询看起来缺少操作者约束
- 自动规则无法稳定确认，但风险很高

### 6. sink → 入口点反查

所有子 skill 完成后，对每个被命中的 sink（危险 mapper 方法、session 写入、Cookie 判断等），用 `rg` 或调用链分析反查所有能到达该 sink 的 controller 入口点。

目的：同一个 sink 可能被多个 controller 入口点到达，必须全部枚举，不能只报一条。

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

## 通用原则

- 不以单个项目的字段名、方法名、返回文案来定义规则
- 样本项目只用于回归验证，不用于定义规则正文
- 不把“没有扫描命中”误当成“没有认证授权问题”
