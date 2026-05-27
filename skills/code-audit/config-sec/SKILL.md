---
name: config-sec
description: 配置安全子清单 — 逐项排查敏感信息、安全头、危险配置。
tags: code-audit,config,secret,header,security-config
when-to-use: 当需要聚焦审计配置安全维度，或项目存在配置文件、Web 响应头设置、数据库连接配置时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 配置安全任务清单

> 配置安全覆盖三个维度：硬编码敏感信息（MUST）、HTTP 安全头、框架危险配置。敏感信息检测为全量审计必做项。

| # | 任务项 | skill | 适用条件 |
|---|--------|-------|---------|
| 1 | 敏感信息检测 | `secret-detection` | **MUST** — 硬编码凭据、密钥、配置中的敏感信息 |
| 2 | 安全头审计 | `security-header-audit` | Web 应用且存在 HTTP 响应头设置 |
| 3 | 危险配置审计 | `dangerous-config` | 存在框架配置文件（php.ini/web.xml/application.yml 等） |

对每项：评估适用条件 → 适用则 `load_skills` 加载并执行 → 标注结果。

## 固定检查项

适用并加载子 skill 后，按以下 checklist 逐项执行，确保覆盖完整。每项标注 `[x] done` 或 `[-] n/a (原因)`。

### 1. 敏感信息检测（→ `secret-detection`）

- 正则扫描云服务密钥（AWS AKIA、Azure、GCP AIza）
- 检测 API Key/Token 硬编码（bearer token、JWT）
- 扫描数据库连接串泄露（mysql://、postgres://、password 字段等）
- 检测私钥泄露（RSA、EC、DSA、OpenSSH 私钥）
- 扫描 git history 中已删除但仍可访问的密钥（git log -p / TruffleHog / Gitleaks）
- 过滤误报（测试文件 mock、示例占位符、注释文档、熵值校验排除随机字符串）
- 生成脱敏列表（文件位置、行号、置信度评估）

### 2. 安全头审计（→ `security-header-audit`）

- 检查 HSTS（Strict-Transport-Security）是否设置
- 验证 X-Content-Type-Options: nosniff 是否设置
- 检查 X-Frame-Options / CSP frame-ancestors 是否设置
- 审计 Content-Security-Policy 配置（详细语法与绕过分析见 client-side-sec / csp-audit）
- 检查 Referrer-Policy 和 Permissions-Policy 是否设置
- 检查所有 Set-Cookie 操作的安全属性（HttpOnly、Secure、SameSite）
- 检查 X-Powered-By/Server 头是否泄露版本信息
- 审计 Access-Control-Allow-Origin 是否过宽，重点检查 `Allow-Credentials: true` + 宽泛 origin 组合（凭据泄露）

### 3. 危险配置审计（→ `dangerous-config`）

- PHP 配置检查（allow_url_include/fopen、display_errors、register_globals、open_basedir、disable_functions）
- Java/Spring 配置检查（transport-guarantee、devtools、actuator endpoints、h2-console、stacktrace 暴露）
- 数据库配置检查（默认密码、明文凭据、过高权限账户）
- 检查生产环境是否开启 debug/verbose 模式
- 验证错误页面是否暴露技术细节
- 审计日志配置（是否记录敏感信息）
- 检查上传目录权限和配置文件可读性
