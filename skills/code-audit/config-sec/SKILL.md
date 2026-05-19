---
name: config-sec
description: 配置安全 P1 路由 — 编排敏感信息检测、安全头审计、危险配置审计等配置层面的安全复核。
tags: code-audit,config,secret,header,security-config,p1-router
when-to-use: 部分 MUST（用户未限定审计方向时，secret-detection 默认执行）。当 P0 Router 识别到项目存在配置文件、Web 响应头设置、数据库连接配置时，额外加载 security-header-audit / dangerous-config
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: false
---

# 配置安全路由（P1 Router）

## 角色

你是配置安全维度的 **P1 路由**，负责将配置安全相关的审计需求分发到具体的 Topic Skill。

## 快速分诊

| 观察到的模式 | 加载 | 理由 |
|-------------|------|------|
| （MUST 维度，用户未限定方向时默认执行） | `secret-detection` | 敏感信息检测 + 影响推演，全量审计时不需要信号触发 |
| 设置 HTTP 安全响应头（HSTS / X-Frame-Options / X-Content-Type-Options 等） | `security-header-audit` | 安全头的语义正确性、完整性需要组合分析 |
| 框架配置文件（php.ini / web.xml / application.yml / nginx.conf 等） | `dangerous-config` | 危险配置项（如 allow_url_include=On）的风险评估 |
| 数据库配置（连接字符串、默认密码、权限配置） | `secret-detection` + `dangerous-config` | 凭据泄露 + 配置风险双重检查 |
| Cookie 设置（HttpOnly / Secure / SameSite 标志） | `security-header-audit` | Cookie 安全属性的完整性分析 |

## Sub-Skill Map

```
config-sec (P1 Router, 本文件)
├── secret-detection       ← 敏感信息检测（MUST，硬编码凭据、密钥、泄露影响推演）
├── security-header-audit  ← 安全头审计（HTTP 安全头完整性、Cookie 安全属性）
└── dangerous-config       ← 危险配置审计（框架配置、运行时配置的安全风险）
```

## 推荐流程

1. **secret-detection 默认执行**：作为 MUST 维度，全量审计时不需要信号触发，首先执行敏感信息检测
2. **信号驱动加载**：根据项目中观察到的配置信号加载 `security-header-audit` 和/或 `dangerous-config`
3. **影响推演**：对发现的密钥/凭据泄露，推演系统性后果（因果链），每条独立攻击面单独列出
4. **汇总发现**：将配置安全发现回传给 P0 Router

## 覆盖完整性

本路由中 `secret-detection` 是 MUST 维度，全量审计时默认执行。其他子 skill 按信号触发。用户明确限定方向时按用户意图。

当本路由被触发时，必须确保以下维度被覆盖：

- 硬编码凭据/密钥检测（MUST）
- 密钥泄露的影响推演（因果链）
- 框架/运行时配置中的危险项（如存在配置文件）
- HTTP 安全头的完整性和正确性（如为 Web 应用）
- Cookie 安全属性（HttpOnly / Secure / SameSite）
