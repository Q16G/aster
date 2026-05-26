---
name: external-recon
description: 授权外部信息收集技能，梳理域名、子域、端口、Web 入口、API、后台、公开文件和资产归属证据。
tags: red-team,recon,osint,subdomain,asset-discovery,attack-surface
when-to-use: 当需要在授权范围内进行外网资产发现、被动侦察、入口识别或攻击面清单整理时使用
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
context: inline
---

# 外部信息收集

> Source attribution: adapted in ASTER style from Apache-2.0 skills `conducting-external-reconnaissance-with-osint`, `performing-open-source-intelligence-gathering`, and `performing-subdomain-enumeration-with-subfinder` in `mukul975/Anthropic-Cybersecurity-Skills`.

## 目标

在授权范围内收集外部攻击面信号，为后续指纹识别和漏洞存在性验证提供输入。默认优先被动侦察；主动扫描必须确认授权。

## 输入

- 授权域名、IP 或 URL
- 排除范围
- 允许方式：passive-only / active-recon / poc-validation
- 输出粒度：资产清单 / 高风险入口 / 报告

## 资产发现清单

### 域名与子域

- 根域、子域、历史域名、品牌域名
- 证书透明度记录
- DNS A/AAAA/CNAME/MX/TXT/NS
- CDN、WAF、云厂商归属
- 通配符解析和停放域名

### 站点入口

- 首页、登录页、注册页
- 管理后台、OA、SSO、VPN、邮箱、网关
- API 文档、Swagger/OpenAPI、GraphQL
- 静态资源、JS bundle、source map
- 测试、预发、dev、uat、staging 环境

### 服务暴露

- HTTP/HTTPS 标题和状态码
- 端口和协议，仅在 active-recon 被授权时进行
- 服务 banner、产品页面、默认页面
- 中间件、框架、CMS、OA、ERP、网关、安全设备

### 公开信息

- 公开代码仓库中的域名、API 路径、配置样例
- 公开文档中的系统截图、接口说明、部署信息
- 可公开访问的备份、日志、配置、包信息文件

## 推荐工具策略

工具不可用时不阻塞，改为手工或 AI 分析模式。

| 工具 | 用途 | 备注 |
|---|---|---|
| `subfinder` | 被动子域枚举 | 推荐 |
| `httpx` | 存活、标题、指纹 | 推荐 |
| `nmap` | 端口和服务 | 主动扫描需授权 |
| `nuclei` | 模板验证 | POC 验证需授权 |
| `rg` | 本地模板和报告搜索 | 内置可用 |

## 输出格式

```markdown
## 外部资产清单

| Asset | Type | Status | Title/Fingerprint | Ownership Evidence | Risk Signal | Notes |
|---|---|---|---|---|---|---|
```

## 安全边界

- 没有授权范围时不执行主动探测。
- 不爆破目录、账号、口令或验证码。
- 不访问、下载或扩散真实业务数据。
- 归属证据不足的资产标记为 `ownership_unconfirmed`。
