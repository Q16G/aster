---
name: client-side-sec
description: 客户端安全 P1 路由 — 编排 CSP 策略审计、客户端 JS 安全审计等前端/客户端安全维度的专项复核。
tags: code-audit,csp,xss,dom,javascript,client-side,p1-router
when-to-use: 当 P0 Router 识别到项目有前端 JS 安全敏感逻辑、CSP 设置、DOM 操作、postMessage 等信号时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: false
---

# 客户端安全路由（P1 Router）

## 角色

你是客户端安全维度的 **P1 路由**，负责将客户端/前端安全相关的审计需求分发到具体的 Topic Skill。

## 快速分诊

| 观察到的模式 | 加载 | 理由 |
|-------------|------|------|
| `header("Content-Security-Policy: ...")` / CSP meta 标签 / CSP 相关响应头 | `csp-audit` | CSP 策略的语义分析（unsafe-inline/unsafe-eval/宽泛 source 等）需要理解指令组合 |
| 客户端 JS 中操作 token / credential / sensitive data | `client-js-audit` | 客户端 token 存储位置（localStorage vs cookie）、泄露路径需要上下文推理 |
| `innerHTML` / `document.write` / `eval` / `postMessage` | `client-js-audit` | DOM XSS 变体、跨源消息安全需要追踪数据流 |
| JS 中的安全决策逻辑（权限检查、feature flag、加解密） | `client-js-audit` | 客户端安全决策可被绕过，需要评估是否有服务端兜底 |

## Sub-Skill Map

```
client-side-sec (P1 Router, 本文件)
├── csp-audit          ← CSP 策略审计（指令语义分析、绕过风险）
└── client-js-audit    ← 客户端 JS 安全审计（DOM XSS、token 安全、postMessage）
```

## 推荐流程

1. **信号收集**：扫描项目中的 CSP 相关 header、前端 JS 文件、HTML 模板
2. **加载子 skill**：根据快速分诊表加载对应的 Topic Skill
3. **执行检查**：子 skill 按各自方法论执行
4. **汇总发现**：将客户端安全发现回传给 P0 Router

## 覆盖完整性

当本路由被触发时，必须确保以下维度被覆盖：

- CSP 策略是否存在及其有效性（如存在 CSP header）
- 客户端 JS 中的安全敏感操作（DOM 操作、token 处理、跨源通信）
- 客户端安全决策是否有服务端校验兜底
