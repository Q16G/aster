---
name: redteam-methodology
description: 授权红队外网打点与高危漏洞存在性验证总控流程，编排范围确认、信息收集、指纹识别、POC 匹配、最小化验证和报告输出。
tags: red-team,external-recon,attack-surface,nuclei,poc,verification
when-to-use: 当用户要求进行授权红队外网打点、外部攻击面梳理或高危漏洞存在性验证时使用
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
context: inline
---

# 红队外网打点与漏洞验证总控

> Source attribution: adapted in ASTER style from selected Apache-2.0 workflows in `mukul975/Anthropic-Cybersecurity-Skills`, especially external reconnaissance, attack surface management, and web/API penetration testing skills.

## 目标

在用户本轮给出的目标范围内完成外网打点和高危漏洞存在性验证。输出可复核证据、风险影响、整改建议和复测方法。

本技能不用于获取真实业务数据、拿 shell、写 WebShell、持久化、提权、横向移动、凭据捕获、绕过检测或破坏性利用。

## 目标输入策略

用户给出域名、IP 或 URL 时，将其直接视为本轮授权范围，立即进入外部信息收集和分诊流程，不要要求用户填写完整授权表格。

仅当用户完全没有给出目标，或目标无法解析为域名、IP、URL 时，才简短询问目标。不要输出大段授权字段表。

可从用户输入中提取：

| 字段 | 说明 |
|---|---|
| 目标组织 | 公司、业务线或系统名称 |
| 授权域名 | 根域、子域或具体 URL |
| 授权 IP | CIDR、单 IP 或明确排除无 IP 探测 |
| 测试窗口 | 允许主动扫描和验证的时间 |
| 排除范围 | 不允许访问的域名、IP、路径、系统 |
| 允许动作 | passive-only、active-recon、poc-validation |
| 输出格式 | 简版结论、Markdown 报告、表格清单 |

缺少测试窗口、排除范围或允许动作时，使用保守默认值继续：

- 测试窗口：当前会话。
- 排除范围：未声明。
- 允许动作：先执行 passive recon 和轻量 HTTP 指纹；运行 nuclei 或主动扫描前只做一句确认，除非用户已经明确要求 POC 验证。

## 工作流

### Phase 1: 目标提取

1. 从用户输入提取域名、IP、CIDR 或 URL。
2. 将提取到的目标记录为本轮范围。
3. 如果没有任何目标，简短询问目标。
4. 明确禁止动作：数据读取、写入、shell、提权、横向、持久化。

### Phase 2: 外部信息收集

加载 `external-recon`。优先使用被动信息源和轻量请求：

- 域名、子域、证书透明度、DNS 记录
- Web 标题、响应头、状态码、favicon、登录入口
- API 文档、Swagger/OpenAPI、GraphQL 入口
- 管理后台、VPN、OA、SSO、测试环境
- JS 文件中的 API 路径、版本、产品线索

### Phase 3: 指纹和暴露面分诊

加载 `fingerprint-triage`，将资产信号整理为：

- 产品/组件候选
- 版本或范围证据
- 可疑路径和功能点
- 适用的通用漏洞类型
- 适用的 nuclei 模板关键词

### Phase 4: POC 匹配

加载 `nuclei-poc-verification`。从 `pocs/nuclei/` 中按以下顺序筛选：

1. 产品名、厂商名、CVE、CNVD、QVD、XVE。
2. `info.tags`、`info.severity`、`metadata`。
3. `http.path` 和 matcher 里的特征路径。
4. 文件名中的漏洞类型：sqli、rce、upload、ssrf、lfi、unauth、xss、infoleak。

### Phase 5: 最小化验证

只验证漏洞存在，不扩大战果：

- SQL 注入：优先布尔差异、错误特征、延时上限受控验证，不 dump 表和数据。
- 文件上传：只上传无害文本或图片样本；不上传脚本、不写 WebShell。
- 命令执行/RCE：优先版本/指纹/matcher 证据；需要回显时只使用无害命令并避免读取敏感文件。
- 反序列化：优先指纹和安全 matcher；不投递产生 shell、写文件或出网回连的 gadget。
- SSRF：优先请求可控的安全回显地址；不访问云元数据凭据路径或内网敏感服务。
- 文件读/LFI：只验证非敏感固定文件或错误差异；不读取配置、密钥、业务数据。
- 未授权：只验证入口可达和最小页面证据；不浏览或导出真实数据。

### Phase 6: 报告输出

加载 `redteam-report` 和 `result-with-file`，输出：

- 授权范围和测试限制
- 外部资产清单
- 指纹证据
- 候选 POC 和适用理由
- 验证状态：confirmed / likely / not_reproduced / blocked
- 脱敏证据
- 风险影响
- 整改建议
- 复测方法

## 状态定义

| 状态 | 含义 |
|---|---|
| confirmed | 已在授权范围内通过最小化证据确认漏洞存在 |
| likely | 指纹或版本命中，但缺少安全验证条件 |
| not_reproduced | 按安全验证步骤未复现 |
| blocked | 缺少授权、工具、网络、账号、窗口或目标不可达 |

## 安全边界

- 未授权范围不测。
- 破坏性模板不跑。
- 高危模板先读 YAML 再判断风险。
- 证据只保留最小样本并脱敏。
- 不把“拿权限/获取数据”作为成功条件。
