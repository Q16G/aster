---
name: secret-detection
description: 敏感信息检测 — 扫描代码中的密钥、凭证、Token 泄露
tags: code-audit,secret,credential,trufflehog
when-to-use: 当需要检查代码中是否存在硬编码的密钥、API Key、密码等敏感信息时
allowed-tools: read_file,list_files,rg,bash
user-invocable: true
---

# 敏感信息检测

## 目标
扫描代码仓库中硬编码的密钥、凭证、Token 等敏感信息，评估泄露风险并提供处置建议。

## 检测模式

### 高置信度模式（正则匹配）
使用 rg 搜索以下高风险模式：

**云服务密钥**：
- AWS: `AKIA[0-9A-Z]{16}`
- Azure: `[a-zA-Z0-9+/]{86}==`
- GCP: `AIza[0-9A-Za-z_-]{35}`

**API Key / Token**：
- Generic API Key: `(?i)(api[_-]?key|apikey)\s*[:=]\s*['"][a-zA-Z0-9]{20,}['"]`
- Bearer Token: `(?i)bearer\s+[a-zA-Z0-9_\-\.]+`
- JWT: `eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_\-]+`

**数据库连接串**：
- `(?i)(mysql|postgres|mongodb|redis):\/\/[^\s'"]+`
- `(?i)(password|passwd|pwd)\s*[:=]\s*['"][^'"]{4,}['"]`

**私钥**：
- `-----BEGIN (RSA |EC |DSA )?PRIVATE KEY-----`
- `-----BEGIN OPENSSH PRIVATE KEY-----`

### 工具扫描（如可用）
- TruffleHog: `trufflehog filesystem <target_path> --json`
- Gitleaks: `gitleaks detect --source <target_path> --report-format json`

## 误报过滤
- 排除测试文件中的 mock 值
- 排除示例配置中的占位符（`xxx`, `your-key-here`, `changeme`）
- 排除注释中的文档引用
- 验证密钥格式是否合法（熵值检查）

## 处置建议
1. **立即轮换**：确认泄露的密钥必须立即轮换
2. **环境变量**：将硬编码值迁移到环境变量
3. **密钥管理**：推荐使用 Vault/AWS Secrets Manager 等密钥管理服务
4. **Git 历史**：检查 git 历史中是否有已删除但仍可访问的密钥
5. **.gitignore**：确保 `.env`、密钥文件等已加入 `.gitignore`

## 输出要求
- 发现的敏感信息列表（脱敏显示，仅显示前后几个字符）
- 文件位置和行号
- 置信度评估（high/medium/low）
- 处置建议和优先级
