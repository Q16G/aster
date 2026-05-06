---
name: dependency-audit
description: 依赖安全审计 — 检查第三方依赖的已知漏洞（SCA）
tags: code-audit,sca,dependency,trivy
when-to-use: 当需要检查项目依赖是否存在已知安全漏洞时
allowed-tools: read_file,list_files,rg,bash
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 依赖安全审计（SCA）

## 目标
检查项目第三方依赖是否存在已知安全漏洞（CVE），评估风险并提供升级建议。

## 工作流程

### 第一阶段：包管理器识别
扫描目标目录，识别包管理器类型：
- Go: `go.mod` / `go.sum`
- Node.js: `package.json` / `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`
- Python: `requirements.txt` / `Pipfile.lock` / `pyproject.toml`
- Java: `pom.xml` / `build.gradle`
- Rust: `Cargo.lock`
- Ruby: `Gemfile.lock`

### 第二阶段：漏洞扫描
根据可用工具选择扫描方式：

**Trivy（推荐）**：
```bash
trivy fs --scanners vuln --format json <target_path>
```

**语言特定工具**：
- Go: `govulncheck ./...`
- Node.js: `npm audit --json` 或 `pnpm audit --json`
- Python: `pip-audit --format json`
- Java: `mvn org.owasp:dependency-check-maven:check`

### 第三阶段：结果分析
1. 按 CVSS 评分排序（Critical ≥ 9.0 > High ≥ 7.0 > Medium ≥ 4.0 > Low）
2. 对每个漏洞：
   - CVE 编号和描述
   - 受影响的依赖和版本
   - CVSS 评分和攻击向量
   - 是否有修复版本
   - 升级兼容性评估
3. 识别传递依赖中的漏洞

### 第四阶段：修复建议
1. 直接升级：提供升级命令
2. 补丁版本：仅升级补丁版本（最安全）
3. 替代方案：推荐替代库
4. 临时缓解：当无法升级时的缓解措施

## 输出要求
- 漏洞统计摘要
- 按严重程度排序的漏洞列表
- 每个漏洞包含：CVE、受影响组件、当前版本、修复版本、升级命令
- 风险评估和优先修复建议
