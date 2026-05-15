---
name: CORS-配置错误检测
description: 检测跨域资源共享配置错误；当任务涉及跨域访问控制、Origin 白名单、凭证跨域读取风险评估时触发；适用于 Web API、前后端分离、网关代理场景。
author: builtin
enabled: true
tags: cors,misconfig,web-security,auth
trigger_keywords: CORS,跨域,Origin,ACAO,Access-Control-Allow-Origin,Access-Control-Allow-Credentials
---

# CORS 检测：跨域配置错误

## 目标
识别可能导致跨站读取敏感数据的 CORS 配置错误，包括：
- `Access-Control-Allow-Origin`（ACAO）反射任意 Origin
- `ACAO: *` 与 `Access-Control-Allow-Credentials: true` 组合
- 错误信任 `null` Origin 或宽泛后缀匹配

## 方法论
1. **基线请求（必做）**：不带 `Origin` 与带合法 `Origin` 各发起一次，记录 ACAO/ACAC/Vary。
2. **恶意 Origin 变异**：使用 `https://evil.example`、`null`、相似子域等 Origin 重放请求。
3. **凭证场景验证**：对需要登录的接口携带 Cookie/Authorization，观察是否允许跨域读敏感响应。
4. **预检验证（按需）**：发送 `OPTIONS` 预检，检查 `Access-Control-Allow-Methods/Headers` 是否过宽。
5. **复核**：采用足以排除缓存、网关改写与偶发头部波动的方式，确认跨域读取行为真实存在。

## 闭环验证要求（必须遵守）
- 所有结论必须形成"前置条件/输入 -> 系统处理 -> 实际效果/危害 -> 可复核证据"的完整证据链，重点证明风险真实生效，而不是只看响应。
- 仅凭上传成功、请求成功、状态码变化、单次报错、配置落盘、前端提示、登录跳转、返回文案等中间信号，不得直接判定 `confirmed`。
- 若只有中间信号、缺少真实效果/危害验证，结论最多为 `suspected`。
- 对写操作、上传、配置变更、认证命中、权限变更、解析触发等场景，必须补做访问、回读、触发、解析、执行或副作用验证，证明风险真正落地并说明具体危害。
- 无法闭环时，必须明确缺失环节与下一步验证建议，不得跳过验证直接下结论。

## 实际效果验证方向（至少证明一类）
- 恶意 Origin 在真实浏览器跨域场景下能读取受保护接口返回的敏感数据。
- 凭证场景下，浏览器会携带 Cookie/Authorization，且响应对恶意站点可读。
- 若只有 ACAO/ACAC 头部异常，但未证明浏览器端真实可读性或敏感数据暴露，不能给 `confirmed`。

## 判定标准

| 现象 | 判定 |
|------|------|
| 恶意 Origin 被允许，且在真实浏览器跨域场景下可读取敏感响应（含凭证场景） | confirmed |
| 配置看似宽松（如反射/`null`），但尚未证明真实浏览器可读性或敏感数据暴露 | suspected |
| 仅允许受控白名单 Origin，且敏感接口无跨域读风险 | not vulnerable |

## 关键检测要点
- 必须结合"是否可读取敏感数据"判定风险，不能只看头字段存在。
- `ACAO: *` 常见于公开静态资源；若无敏感数据与凭证，不应直接判高危。
- 注意 `Vary: Origin`、缓存层与网关改写，避免一次性结果误判。

## AI 检测步骤建议
1. 定位可疑 API（用户信息、订单、后台管理等）。
2. 构造合法/恶意 Origin 请求并对比响应头。
3. 若存在登录态，分别在有无凭证下重复验证。
4. 输出证据链：请求头、响应头、关键响应体片段、风险等级。

## 修复建议
- 使用严格 Origin 白名单（精确匹配协议+域名+端口）。
- 禁止在敏感接口对任意 Origin返回可读权限。
- 仅在确有需要时启用 `Access-Control-Allow-Credentials: true`。
