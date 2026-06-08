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

盘点时除查 CVE 外，顺带识别无可读源码的依赖分流，别一律当反编译候选移交。**关键判据：攻击面可见性不对称**——数据流污点跨入依赖外部可观测，但入口点/信任边界逻辑（自动注册的 controller、自研 filter 打进 jar、编程式注册的 route）封在产物内、消费侧 import 看不见。故"重要性（是否在关键路径）"对**第三方**用得上，对**同厂商自有闭源则归属即触发**（消费侧证不了它不在关键路径，循环依赖）：
- **外部可证不在关键路径**（边角依赖、且可正面证明仅标准化通道）→ 不移交反编译。
- **同厂商自有闭源无源码**（私服自研包、本地路径依赖、Vendor/坐标指向项目方或同厂商的闭源 jar）→ **归属即列为反编译候选移交 `dependency-decompile`，不要求先证在关键路径**（入口点封在 jar 内、消费侧不可观测）——这类是 SCA 数据库覆盖不到、又最需要人工看清行为的盲区。
- **位置/重要性拿不准**（不透明依赖看不见内部，无法确定在不在关键路径）→ **偏放行当候选移交**，别默认判它"不在关键路径"而漏移交。
- **在关键路径 + 第三方异厂商商业闭源**（外部厂商坐标/Vendor + 商业或专有许可，如 Oracle JDBC(BCL)、达梦驱动）→ 按 **安全/sink 决策在依赖内还是在调用方** 判，不按"异厂商"也不按"标不标准"豁免：**(a) 决策在调用方、依赖只是标准化通道**（如 DB 驱动：拼 SQL 在调用方，驱动只按 JDBC 规范执行）→ 行为已知，可免移交；**(b) 安全/sink 决策在依赖内部**（如商业 OEM/低代码平台把鉴权/路由/渲染封进厂商 jar，或自称标准协议的闭源鉴权 SDK——授权决策在它内部就证不了它真按标准做）→ "行为已知"不成立，**仍作反编译候选移交并标法律（EULA）**。
- **诚实性约束**：关键路径上落 (b) 的依赖、以及**同厂商自有闭源未查清内部的**，只要不反编译，那条穿过它的污点/攻击面问题就留**显式缺口/`needs_review`**交回，CVE 扫描只覆盖已知漏洞，不等于这个闭源组件在污点路径上行为安全；要"免移交不留缺口"须**正面证明**它属 (a) 类（决策在调用方），不能靠"廉价扫描没扫到入口点"放行。
- 归属判据复用 `dependency-decompile` / `project-framework-analysis` 的 `MANIFEST` Vendor、`pom` groupId、`LICENSE`/`NOTICE`，**归属或位置拿不准都偏放行当候选**（关键路径上欠覆盖优于漏判）。
- **移交时随候选附坐标与仓库线索**：把识别到的 `groupId:artifactId:version`（含 `pom.xml`/`build.gradle` 依赖声明、私服/`<repositories>`/`settings.xml` mirror 线索）一并交给 `dependency-decompile`，便于下游在本地无件时按坐标取件（sources 优先），不必从头重查坐标。

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
