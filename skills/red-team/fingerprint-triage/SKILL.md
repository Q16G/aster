---
name: fingerprint-triage
description: 将外部资产信号转化为产品、组件、版本和漏洞类型假设，并路由到 nuclei 模板或 ASTER 现有技能。
tags: red-team,fingerprint,triage,nuclei,product-detection,vulnerability-routing
when-to-use: 当已有资产标题、响应头、路径、JS、favicon 或版本线索，需要判断适用 POC 或测试技能时使用
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: false
context: inline
---

# 指纹分诊

> Source attribution: adapted in ASTER style from Apache-2.0 attack surface and web/API testing workflows in `mukul975/Anthropic-Cybersecurity-Skills`.

## 目标

把授权外部资产的可观察信号转化为可验证假设，减少误报和盲目打 POC。

## 输入信号

- URL、域名、IP、端口、协议
- HTTP 标题、状态码、重定向链
- 页面标题、登录页文案、版权信息
- Cookie 名称、Server、X-Powered-By
- favicon hash、静态资源路径
- JS bundle 中的 API、版本、产品关键字
- 错误页面、默认页面、readme、manifest、package 信息
- 已知 CVE、CNVD、QVD、XVE、产品名

## 分诊步骤

1. 整理资产信号表。
2. 生成产品/组件候选，并标记证据强度。
3. 区分“强指纹”和“弱指纹”：
   - 强指纹：明确产品名、版本、官方路径、特征 API、唯一页面文案。
   - 弱指纹：通用框架、相似标题、模糊路径、单一 header。
4. 根据漏洞类型路由：
   - SQL 注入 → `SQL注入-多策略综合检测`
   - 命令执行 → `command-injection`
   - 文件上传 → `文件上传-多策略综合检测`
   - SSRF → `ssrf-testing`
   - XXE → `xxe-testing`
   - SSTI → `ssti-testing`
   - 路径穿越/LFI → `path-traversal-lfi`
   - 未授权/越权 → `access-control`、`越权访问-IDOR检测`、`越权访问-未授权访问检测`
   - API/JWT/CORS → `api-token-sec`、`JWT-弱密钥与信息泄露检测`、`CORS-配置错误检测`
5. 根据产品、CVE、tags 和漏洞类型搜索 `pocs/nuclei/`。

## nuclei 模板匹配建议

优先匹配：

- 文件名命中产品名或 CVE。
- `info.metadata.vendor/product` 命中。
- `info.tags` 命中产品、框架、漏洞类型。
- `http.path` 与资产暴露路径吻合。
- `matchers` 验证版本或页面特征，而不是直接执行破坏性动作。

## 输出格式

```markdown
## 指纹分诊

| Asset | Fingerprint | Evidence | Confidence | Candidate Skills | Candidate Templates |
|---|---|---|---|---|---|
```

## 安全边界

- 指纹不足时，不把 POC 命中当作 confirmed。
- 不因弱指纹批量运行高危模板。
- 对 RCE、反序列化、上传类模板必须先读 YAML，再判断是否可安全验证。
