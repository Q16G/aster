---
name: dangerous-config
description: 危险配置专项审计 — 检查框架配置文件、运行时配置中的安全风险项（如 allow_url_include、debug 模式、宽松权限等）。
tags: code-audit,config,php-ini,web-xml,application-yml,dangerous
when-to-use: 当 config-sec P1 Router 识别到项目存在框架配置文件（php.ini / web.xml / application.yml 等）时
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---

# 危险配置专项审计

## 目标

审计项目中框架配置文件和运行时配置的安全性。危险配置往往是 RCE 或信息泄露的前提条件（如 PHP 的 `allow_url_include=On` 使 LFI 升级为 RCE），但难以用 source→sink 规则覆盖。

## 按技术栈检查

### PHP

| 配置项 | 危险值 | 风险 |
|--------|-------|------|
| `allow_url_include` | `On` | LFI → RCE（php://input） |
| `allow_url_fopen` | `On` | SSRF / 远程文件读取 |
| `display_errors` | `On`（生产） | 信息泄露（路径、SQL 语句） |
| `expose_php` | `On` | 版本信息泄露 |
| `register_globals` | `On` | 变量覆盖攻击 |
| `session.cookie_httponly` | `0` | XSS 可窃取 session cookie |
| `session.cookie_secure` | `0` | 明文传输 session cookie |
| `session.use_strict_mode` | `0` | Session 固定攻击 |
| `open_basedir` | 未设置 | 任意文件读取范围无限制 |
| `disable_functions` | 空 | 危险函数未禁用 |

### Java (web.xml / application.yml / Spring)

| 配置项 | 危险值 | 风险 |
|--------|-------|------|
| `<transport-guarantee>` | `NONE` | 未强制 HTTPS |
| `<session-timeout>` | 过大或未设置 | Session 长期有效 |
| `spring.devtools.restart.enabled` | `true`（生产） | 开发工具暴露 |
| `management.endpoints.web.exposure.include` | `*` | Actuator 端点全部暴露 |
| `spring.h2.console.enabled` | `true` | H2 控制台暴露 |
| `server.error.include-stacktrace` | `always` | 堆栈信息泄露 |

### 通用配置

| 配置类型 | 检查点 |
|---------|--------|
| 数据库配置 | 默认密码、明文凭据、过高权限账户 |
| 日志配置 | 是否记录敏感信息（密码、token、个人信息） |
| 调试模式 | 生产环境是否开启 debug/verbose |
| 错误页面 | 是否暴露技术细节（phpinfo、stacktrace） |
| 文件权限 | 上传目录是否可执行、配置文件是否可读 |

## 结论口径

按配置文件组织。每条发现标注：配置项、当前值、风险描述、建议值、severity。

与 `secret-detection` 的分工：硬编码凭据/密钥由 `secret-detection` 负责，本 skill 负责非凭据类的配置风险项。
