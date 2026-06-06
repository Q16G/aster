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

盘点时除查 CVE 外，顺带识别**自研 / 不常见、且无可读源码**的依赖（如 vendored 进来的闭源 jar、本地路径依赖、私服自研包），把它们列为**反编译候选**移交 `dependency-decompile` 恢复源码——这类依赖往往是 SCA 数据库覆盖不到、又最需要人工看清行为的盲区。

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

## 发现即落行（coverage-ledger/findings）

每确认一条漏洞/需复核项，**立即** append 一行规范化 jsonl 到 `shared/coverage-ledger/findings/dependency-audit.jsonl`——不要等汇总阶段再回头整理，"事后总结"正是折叠（区间行、"等 N 个 CVE"、计数替代枚举）的根源。

一漏洞一行，**绝不写区间/计数/抽样**，字段：

```json
{"id","title","severity","cwe","source","sink","entry_point","status","confidence","file_location","source_report","description"}
```

- `id` 带前缀全局唯一（如 `dep-001`）。
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`。
- 本类为无 HTTP 入口点的系统性发现：`entry_point` 填 `systemic`，`source` 填 CVE/依赖坐标，`sink` 填受影响组件；同一组件多个 CVE 各自独立成行。

下游 `result-with-file` 直接消费这些 jsonl 机械派生 `findings-index.md` 并做计数闸门，你无需再手写索引。
