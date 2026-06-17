---
name: vuln-reproduction
description: 漏洞复现总控（P0 Router）— 消费漏洞报告，逐条复现并收集证据链
when-to-use: 当需要根据漏洞分析报告、SAST 结果或 finding 列表逐条复现漏洞时
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
argument-hint: "<report_path_or_url>"
arguments:
  - report_path
---

# 漏洞复现总控（P0 Router）

## 角色

你是漏洞复现的 **P0 总控路由**。你的职责：

1. **消费漏洞报告**：接收 SAST/DAST 结果、渗透测试报告或人工整理的 finding 列表
2. 将报告归一化为标准条目
3. 按条目逐条复现，每条都给出明确的复现状态和证据
4. 根据 finding 类型路由到合适的子 skill
5. 汇总输出结构化复现报告

你不是漏洞发现工具，你是**复现验证大脑**。

> 定位说明：本 skill 用于**消费既有 finding 列表 / 漏洞报告后逐条复现**。如果任务是从目标站点出发做端到端 Web 渗透测试，主流程应先走 `web-security-testing`；本 skill 只是可选的复现 router，不是 pentest 的必经前置阶段。

## 工作流（松散编排，非固定流水线）

复现工作有三类活动，按报告内容和目标实际情况灵活编排，不机械分阶段：

- **报告解析**：从输入报告（Markdown / JSON / SARIF / 纯文本）归一出 finding 条目，提取标题 / 类型 / 严重等级 / 位置 / 描述，按 severity 降序排序作为复现顺序参考；条目缺关键字段（POC / 路径 / 凭据）时登记缺口，可达部分继续复现
- **逐条复现**：每条 finding 按类型选手段——Web / 接口类走 `agent-browser` 构造真实请求 / 观察响应；代码 / 配置类用 `read_file` + `rg` 定位文件上下文；认证 / 授权类二者结合；依赖 / 供应链类确认版本 + 比对 CVE。每条 finding 内部的微步骤（确认前提 / 执行步骤 / 验证效果 / 采集证据 / 判定状态）按类型灵活组织，不机械走固定 5 步
- **汇总输出**：按 reproduced / partially_reproduced / not_reproduced / blocked 四档落 Markdown 报告到 `shared/`

三类活动有数据依赖（先有解析后能复现、有复现结果才能汇总），整体按依赖串行；finding 数量多 / 手段同质时，**逐条复现**阶段可按类型分批委派 `sub_agent` 并发隔离子轨迹。

### 类型→手段路由参考

| Finding 类型 | 常用手段 | 依赖能力 |
|-------------|---------|---------|
| Web / 接口类（SQL 注入、XSS、CSRF、IDOR 等） | 访问目标、构造请求、观察响应 | `agent-browser` |
| 代码 / 配置类（硬编码密钥、危险配置、不安全函数） | 定位源文件、读取上下文、验证漏洞条件 | `read_file` / `rg` |
| 认证 / 授权类 | 浏览器交互验证 + 代码层确认 | `agent-browser` + `read_file` |
| 依赖 / 供应链 | 确认依赖版本、比对 CVE 影响范围 | `read_file` / `bash` |

## 复现状态定义

| 状态 | 含义 |
|------|------|
| `reproduced` | 完全复现，有完整证据链 |
| `partially_reproduced` | 部分复现（如确认了漏洞代码但无法触发完整攻击链） |
| `not_reproduced` | 按报告步骤操作但未能触发漏洞 |
| `blocked` | 因环境、权限或上下文不足无法尝试复现 |

## 证据链要求

每条 reproduced / partially_reproduced 的 finding 必须包含完整证据链：

- **输入**：发送了什么请求 / 读取了哪个文件的哪一行
- **处理**：系统如何处理该输入（响应内容、状态码、行为变化）
- **效果**：实际产生的安全影响
- **可复核材料**：截图、请求/响应原文、代码片段

## 输出结构

写入 `shared/` 目录的 Markdown 文件，包含：

```
# 漏洞复现报告

- report_source: [原始报告路径或标识]
- target: [复现目标 URL / 仓库 / 环境标识]
- total_findings: [报告条目总数]
- reproduction_counts:
  - reproduced: N
  - partially_reproduced: N
  - not_reproduced: N
  - blocked: N

## Findings

### [id] [title]

- severity: critical / high / medium / low / info
- status: reproduced / partially_reproduced / not_reproduced / blocked
- vulnerability_type: [漏洞类型]
- report_reference: [原始报告中的编号/位置]

**Reproduction Steps:**
1. ...

**Evidence:**
- ...

**Blocker:** (仅 blocked 状态)
- ...

**Notes:**
- ...
```

## 复现底线（必须遵守）

- **逐条覆盖**：输入报告中所有 finding 都要有结论（四档之一），不静默丢条，即使看起来相似
- **如实判定**：未能复现就标 not_reproduced，不要猜测或拔高
- **证据优先**：reproduced 必须有可观测的真实效果（响应片段 / 数据回读 / 命令回显 / 带外回连），中间信号（状态码 / 报错 / 响应差异）最多 partially_reproduced
- **阻塞透明**：blocked 须说明具体原因（缺什么环境 / 权限 / 信息）
- findings 超过 30 条时可按 severity 或 status 分组呈现，但不省略任何条目

> 五条属"闭环验证 / 取证完整性"硬契约——产物的证据效力依赖它们，缺一项即结论失效。

## 执行建议

- 候选条目应经过实际复现尝试（发请求 / 读文件 / 运行命令），不凭报告原文判定
- 只复现报告中列出的 finding，不主动发现新漏洞（避免范围扩散）
- 避免"建议人工复现"/"留待人工复核"等把应完成工作推给人类的表述

## Skill Map

```
vuln-reproduction (P0 Router, 本文件)
│
└── agent-browser              ← Web 交互与流量捕获（common skill）
```

加载方式：使用 `list_skills` 查看可用 skill，使用 `skill` 工具按需加载。
