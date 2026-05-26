---
name: redteam-report
description: 生成授权红队外网打点与漏洞存在性验证报告，包含范围、资产、指纹、POC、证据、风险、整改和复测方法。
tags: red-team,report,evidence,remediation,retest
when-to-use: 当红队外网打点或 POC 验证完成，需要输出客户可读的 Markdown 报告或表格结论时使用
allowed-tools: read_file,list_files,rg,list_skills,load_skills
user-invocable: true
context: inline
---

# 红队外网验证报告

## 目标

把授权范围内的外部资产、指纹、候选 POC、漏洞验证结果和整改建议整理为可交付报告。报告只包含证明漏洞存在所需的最小化证据。

## 报告结构

```markdown
# 红队外网打点与漏洞验证报告

## 1. 评估概览

- target_organization:
- authorized_scope:
- test_window:
- allowed_testing:
- excluded_scope:
- methodology:

## 2. 外部资产清单

| No. | Asset | Type | Status | Fingerprint | Ownership Evidence | Risk Signal |
|---|---|---|---|---|---|---|

## 3. 指纹与 POC 匹配

| No. | Asset | Fingerprint Evidence | Candidate POC | Applicability | Notes |
|---|---|---|---|---|---|

## 4. 漏洞验证结果

| No. | Finding | Asset | Severity | Status | Evidence | Recommendation |
|---|---|---|---|---|---|---|

## 5. 重点发现

### Finding: [漏洞名称]

- Asset:
- Severity:
- Status:
- Template/Skill:
- Evidence:
- Impact:
- Remediation:
- Retest:

## 6. 未复现与阻塞项

| No. | Target | Candidate Issue | Status | Blocker | Next Step |
|---|---|---|---|---|---|

## 7. 整改优先级

1. Critical confirmed findings
2. High confirmed findings
3. High-confidence likely findings
4. Exposed management/API surfaces
5. Weak fingerprint or informational exposure
```

## 状态规范

| 状态 | 用法 |
|---|---|
| confirmed | 已通过最小化验证确认 |
| likely | 指纹或版本高度匹配，但缺少安全验证条件 |
| not_reproduced | 执行安全验证未复现 |
| blocked | 授权、工具、网络、账号、窗口或模板风险阻塞 |

## 证据脱敏

必须脱敏：

- token、cookie、session、authorization header
- 手机号、身份证、邮箱、姓名等 PII
- AK/SK、密码、私钥、连接串
- 真实业务订单、用户、财务、文件内容

示例：

```text
Authorization: Bearer eyJ********
Set-Cookie: SESSION=********
access_key=AKIA************
phone=138****1234
```

## 写作要求

- 用户使用中文时，报告默认中文。
- 结论要区分 confirmed、likely、blocked，不能夸大。
- 不描述拿权限、拖数据、后渗透路径。
- 每条 confirmed finding 必须有可复核证据和复测方法。
