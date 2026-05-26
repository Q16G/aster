---
name: nuclei-poc-verification
description: 使用本地 nuclei YAML 模板库进行授权范围内的 POC 筛选、风险分级和最小化漏洞存在性验证。
tags: red-team,nuclei,poc,cve,verification,vulnerability-validation
when-to-use: 当需要使用 nuclei 模板库验证授权目标是否存在某个 CVE、产品漏洞或高危 Web/API 漏洞时使用
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "<target_url_or_scope> [template_keyword_or_cve]"
arguments:
  - target
  - template_keyword
context: inline
---

# nuclei POC 验证

## 目标

读取并使用本地 `pocs/nuclei/` 模板库，对授权目标进行漏洞存在性验证。验证目标是证明风险存在，不获取数据、不拿权限、不执行破坏性操作。

## 模板库

默认模板路径：

```text
pocs/nuclei/
```

模板是 nuclei YAML，重点读取：

- `id`
- `info.name`
- `info.severity`
- `info.tags`
- `info.metadata`
- `http.method`
- `http.path`
- `http.raw`
- `matchers`
- `extractors`

## 筛选流程

1. 确认目标在授权范围内。
2. 用 `rg` 搜索模板：
   - 产品名：`rg -n "yonyou|seeyon|jira|wordpress" pocs/nuclei`
   - CVE：`rg -n "CVE-2023-xxxxx" pocs/nuclei`
   - 漏洞类型：`rg -n "sqli|rce|upload|ssrf|lfi|unauth|xss" pocs/nuclei`
3. 用 `read_file` 阅读候选 YAML。
4. 判断模板风险：
   - safe: GET/HEAD、版本检测、指纹匹配、只读 matcher。
   - caution: POST、状态改变可能、上传无害文件、延时验证。
   - blocked: webshell、命令执行副作用、敏感文件读取、凭据抓取、出网回连、持久化。
5. 只对 safe/caution 模板给出最小化验证方案。

## 可执行 nuclei 的条件

只有同时满足以下条件，才可以建议或调用 nuclei：

- 用户已给出明确授权目标。
- 模板已阅读并确认不包含禁止动作。
- 目标数量受控。
- 命令指定模板路径，不使用无限制批量模板目录。
- 输出保存为证据文件或报告摘要。

命令格式示例：

```bash
nuclei -u https://authorized.example.com -t pocs/nuclei/path/to/template.yaml -severity low,medium,high,critical -no-color
```

## 禁止执行的模板行为

- 上传脚本、WebShell 或可执行 payload。
- 执行系统命令获得 shell 或读取敏感文件。
- 读取数据库、配置、密钥、token 或真实业务数据。
- 利用反序列化 gadget 产生写文件、回连、进程启动。
- 访问云元数据凭据接口。
- 爆破账号、口令、验证码或 token。

## 证据要求

每个结论包含：

| 字段 | 说明 |
|---|---|
| template_id | nuclei 模板 id |
| template_path | 本地模板路径 |
| target | 授权目标 |
| validation_status | confirmed / likely / not_reproduced / blocked |
| evidence | 脱敏响应片段、状态码、matcher 说明 |
| risk | 风险等级 |
| safe_method | 最小化验证方法 |
| remediation | 整改建议 |

## 降级模式

如果未安装 nuclei：

1. 读取模板。
2. 解释适用条件和 matcher。
3. 给出安全手工验证步骤。
4. 标记为 `likely` 或 `blocked`，不伪造 confirmed。
