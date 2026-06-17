---
name: dependency-audit
description: >-
  依赖安全审计（SCA）——按依赖清单 / 锁文件提取 SBOM，匹配已知 CVE 库（NVD / GitHub Advisory），
  对每个命中判定 vulnerable code 是否在项目实际消费路径上（关键路径判据）。
when-to-use: 当需要检查项目依赖是否存在已知安全漏洞时
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 依赖安全审计（SCA + 关键路径判定）

## 1. 触发线索 / 适用信号

按 "依赖清单 / 锁文件 / 已构建产物 / 容器镜像" 四维识别本能力命中场景。

**依赖清单 / 锁文件维度**（项目根存在以下任一）：
- Java：`pom.xml`（Maven）/ `build.gradle` / `build.gradle.kts`（Gradle）/ `*.lockfile`
- Node.js：`package.json` + `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`
- Python：`requirements*.txt` / `Pipfile` + `Pipfile.lock` / `poetry.lock` / `pyproject.toml`
- Go：`go.mod` + `go.sum`
- PHP：`composer.json` + `composer.lock`
- Ruby：`Gemfile` + `Gemfile.lock`
- Rust：`Cargo.toml` + `Cargo.lock`

**容器 / 基础镜像维度**：
- `Dockerfile` 含 `FROM <image>:<tag>` 拉取基础镜像
- `docker-compose.yml` 引用第三方镜像
- 容器层用 `apt-get install` / `apk add` / `yum install` 落地系统包

**已构建产物维度**（无源码、只看到产物时仍可粗筛）：
- 项目目录含 `*.jar` / `*.war` / `*.ear`（含 `META-INF/MANIFEST.MF`）
- `node_modules/` 已落地的传递依赖树
- `vendor/` 目录里直接 vendored 的三方代码（Go / PHP 常见）

**反向信号**（不命中本能力）：
- 纯单文件脚本 / 无外部依赖的玩具项目
- 用户明确只要看自家源码（不审计第三方）的纯白盒审计场景

---

## 2. 造成原因（共享章节）

项目使用了含已知 CVE 的依赖库 → 攻击者利用已公开的 PoC 在该库的脆弱代码路径上触发漏洞 → 漏洞实际触达取决于 vulnerable code 是否在项目消费路径上。

成因核心可分为四档：

- **已知 CVE 直接依赖**：项目直接 `require` / `import` 含 CVE 的版本，且 vulnerable function 在调用链上。
- **传递依赖 CVE**：A 依赖 B、B 依赖 C，C 含 CVE。直接依赖审计漏掉传递层，是 SCA 最常见盲区——直接依赖只有几十个，传递依赖可能上千。
- **过期 / 不再维护依赖**：库已停止维护，新发现的 0day 无 patch 可用，风险持续累积。
- **恶意依赖**：typosquatting（包名误植）/ 包仓库账号被劫持 / 维护者主动投毒——这类不需要"漏洞"，包本身就是恶意代码（供应链投毒）。

关键判据：CVE 命中本身只代表"理论上有风险"。**真实风险 = CVE 命中 + vulnerable code 在项目消费路径**。SCA 工具的核心痛点正是 N 个 CVE 但只有 M 个真正在消费路径上——本能力把"是否在关键路径"作为静态可达性的核心判据。

---

## 3. 领域 source-sink 数据流模型

**本能力 n/a**（原因：SCA 本质是"依赖坐标 + 版本"与"已知 CVE 库"的版本比对 + 库匹配，不走代码层 source → sink 的数据流模型。CVE 命中是 (package, version, cve_id) 三元组级别的事实，不是用户输入流到危险 sink 的污点追踪。需要追依赖内部数据流时移交 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译后接 [dataflow-analysis](../dataflow-analysis/SKILL.md)）。

---

## 4. 常见类型（共享章节）

按 SCA 维度的主流命中类型（按已知主流覆盖，不追求穷举）：

| 类型 | 静态识别特征 | 主要判定难点 |
|---|---|---|
| **已知 CVE 直接依赖** | manifest 里直接 declared 的 (groupId, artifactId, version) 命中 NVD / GitHub Advisory | 是否实际消费 vulnerable function |
| **传递依赖 CVE** | lock 文件里的传递坐标命中 CVE，但 manifest 看不到 | 调用链跨多层依赖，判可达成本高 |
| **过期 / 不再维护依赖** | 上次发版超过 N 年；upstream 已 archive；无 CVE 不代表安全 | 0day 风险，需做替换评估 |
| **恶意 / typosquatting 包** | 包名与流行库相近（`lodahs` vs `lodash`）/ 安装时执行可疑脚本 | 需对比 npm/pypi 主流命名 + 检查 install hook |
| **lock 与 manifest 不一致** | `package.json` 写 `^1.0.0`，但 `package-lock.json` 锁了 `1.5.3-known-cve` | 构建漂移——开发/CI/生产实际拉到的版本可能不同 |
| **多版本共存** | 同一库（如 `log4j-core`）出现 1.x / 2.x 多份；其中一份命中 CVE | shaded jar / fat jar 隐藏多版本 |
| **基础镜像系统包 CVE** | `FROM ubuntu:18.04` 拉到 `openssl` / `glibc` 的旧版 CVE | OS 层 CVE 通常被业务侧忽视 |

---

## 5. 入口点定位

按依赖管理生态找清单与锁文件——粗筛能否命中真实候选取决于扫描面是否覆盖到正确的 manifest / lock 文件。

> 下列生态 / 工具仅作类似项目示例 不限于此；以目标实际栈为准。

### 按依赖管理工具差异

> §6 跨框架代码变体在本能力 n/a（见 §6），此处合并展开"按依赖管理工具差异"。SCA 不是代码 pattern 对比，而是依赖生态差异——每个生态的清单文件、锁文件格式、传递依赖展开方式都不同。

#### Java（Maven / Gradle）

- Maven：`pom.xml` 直接依赖 + `mvn dependency:tree -DoutputType=text` 展开传递树
- Gradle：`build.gradle` / `build.gradle.kts` + `gradle dependencies --configuration runtimeClasspath`
- shaded / fat jar：`mvn dependency:tree` 不展开 shaded 内容，需反编译 jar 看 `META-INF/MANIFEST.MF` + 类路径

#### Node.js（npm / yarn / pnpm）

- 直接依赖：`package.json` 的 `dependencies` / `devDependencies` / `optionalDependencies`
- 传递依赖：`package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`（npm v7+ 全展开为扁平树）
- 工具：`npm ls --all` / `yarn list --pattern '*'` / `pnpm list --depth Infinity`

#### Python

- 清单：`requirements.txt` / `setup.py` / `pyproject.toml`（PEP 621）
- 锁文件：`Pipfile.lock` / `poetry.lock`
- 工具：`pip freeze` / `pip-audit` / `poetry show --tree`

#### Go

- 清单：`go.mod`
- 锁：`go.sum`（含校验和）
- 工具：`go list -m all`（全模块列表）/ `go mod graph`（依赖图）/ `govulncheck ./...`

#### PHP

- 清单：`composer.json`
- 锁：`composer.lock`
- 工具：`composer show -t` / `composer audit`

#### Ruby

- 清单：`Gemfile`
- 锁：`Gemfile.lock`
- 工具：`bundle list` / `bundle audit`

#### Rust

- 清单：`Cargo.toml`
- 锁：`Cargo.lock`
- 工具：`cargo tree` / `cargo audit`

#### 容器 / 基础镜像

- `Dockerfile` 的 `FROM <image>:<tag>` 是 OS 包 CVE 的入口
- 镜像层用 `apt` / `apk` / `yum` 安装的系统包同样需扫
- 工具：`trivy image <image>` / `grype <image>`

### 通用建议

- 优先从 manifest 找直接依赖列表，再用工具展开传递依赖
- lock 文件存在时**以 lock 文件为准**（manifest 只有 range，lock 才是实际锁定版本）
- 关键路径上的闭源依赖（无源码 jar / 已 archive 包）作为反编译候选移交 [dependency-decompile](../dependency-decompile/SKILL.md) 追内部数据流

---

## 6. 跨框架代码变体

**本能力 n/a**（原因：SCA 关心的是依赖坐标与 CVE 库匹配，不是代码层 pattern 对比。同一漏洞（如 Log4Shell）在 Spring / Dropwizard / Vert.x 项目里"危险形态"都是同一句 `log.info(userInput)`——差异不在代码 pattern，而在 §5 已展开的"按依赖管理工具差异"。代码层 pattern 对照请参 [sast-scan](../sast-scan/SKILL.md) §6 或对应单漏洞 skill）。

---

## 7. 思考检查点（共享章节）

加载本 skill 时按这些问题思考：

- 这个 CVE 命中的 vulnerable function 是否在项目实际消费路径？只是 `import` 但未被调用，算不算？
- 传递依赖的 CVE 在 manifest 里看不到——直接依赖审计是否漏掉了它？lock 文件有没有展开？
- lock 文件与 manifest 是否一致？开发 / CI / 生产环境实际拉到的版本是不是同一个？（防构建漂移）
- 容器基础镜像的 OS 层 CVE（`openssl` / `glibc` / `apt` 包）有没有覆盖？业务侧通常忽视。
- 这个依赖是不是"归属即触发"型（库一旦引入就自动装载危险逻辑，如 Log4j JNDI lookup）？若是，业务未直接调用也按 confirmed-static 处理。

---

## 8. 检测方法论 / 数据流追踪

> 本能力**只到候选 + 静态可达性**——动态利用证据走对应漏洞的黑盒 / graybox skill；关键路径上的闭源依赖追内部数据流走 [dependency-decompile](../dependency-decompile/SKILL.md) + [dataflow-analysis](../dataflow-analysis/SKILL.md)。本节方法论描述粗筛与判定，不规定 plan / step 编排。

### Step 0：依赖生态识别

- 按 §5 信号识别项目用的依赖管理工具（一个项目可能多语言并存）
- 识别容器 / 基础镜像层（`Dockerfile` 存在时）
- 列出所有 manifest + lock 文件清单作为扫描面

### Step 1：SBOM 提取

每语言用对应工具生成完整传递依赖列表：

```bash
# Java / Maven
mvn dependency:tree -DoutputType=text > sbom-mvn.txt
# Java / Gradle
gradle dependencies --configuration runtimeClasspath > sbom-gradle.txt
# Node.js
npm ls --all --json > sbom-npm.json
# Python
pip freeze > sbom-pip.txt    # 或 pip-audit --format=json
# Go
go list -m all > sbom-go.txt
# PHP
composer show -t > sbom-php.txt
# Rust
cargo tree > sbom-cargo.txt
# 容器镜像
trivy image --format json <image> > sbom-image.json
```

### Step 2：CVE 库匹配

按生态选对应扫描器（优先用支持离线 CVE 库的工具，便于审计环境复现）：

- **通用**：Trivy / Grype / Snyk / Sonatype OSS Index / OWASP Dependency-Check
- **语言特定**：
  - Node：`npm audit` / `yarn audit` / `pnpm audit`
  - Python：`pip-audit` / `safety check`
  - Go：`govulncheck ./...`（注意：govulncheck 已内置可达性判断，会标 vulnerable call vs vulnerable but not called）
  - Rust：`cargo audit`
  - Java：`mvn org.owasp:dependency-check-maven:check` / Trivy
  - Ruby：`bundle audit`
- **容器**：Trivy / Grype 扫 `FROM` 基础镜像 + 安装层

### Step 3：关键路径判定（核心）

CVE 命中 ≠ 真实风险——需判 vulnerable code 是否在项目消费路径。按下列优先级：

1. **直接调用**：grep CVE advisory 提到的 vulnerable function / class / method 名在项目源码里的使用
   - 命中即标 `reachable=true`
2. **传递路径**：调用链从项目代码 → 中间库 → vulnerable function
   - 用 `go list -deps` / `npm ls <dep>` / `mvn dependency:tree -Dincludes=<gav>` 看反向依赖
3. **归属即触发**：库一旦被引入就自动装载危险逻辑（典型如 Log4Shell 的 JNDI lookup、Spring4Shell 的 `ClassLoader` 暴露）
   - 即便业务未直接调用也按 `reachable=triggered-by-import`，等同 confirmed-static
4. **反射 / 配置驱动消费**：vulnerable function 通过反射调用 / 配置驱动加载
   - 静态不可判，标 `reachable=static-unknown`
5. **明确不在消费路径**：CVE 仅影响测试代码 / 仅影响特定平台 / 项目未启用该功能
   - 标 `reachable=false`，留必要的判定证据（grep 输出 + 配置位置）

### Step 4：闭源依赖移交

关键路径上但闭源 / 无源码可读的依赖——sast-scan / dataflow-analysis 无法继续追踪：

- 同厂商自有闭源（私服自研 jar / 本地路径依赖 / Vendor 指向项目方）→ 归属即触发，作为反编译候选移交 [dependency-decompile](../dependency-decompile/SKILL.md)
- 第三方异厂商商业闭源（如 Oracle JDBC / 达梦驱动）：
  - 决策在调用方 + 依赖只是标准化通道（如 JDBC 驱动按规范执行）→ 行为已知，可免移交
  - 安全 / sink 决策封在依赖内部（如商业 OEM / 低代码平台把鉴权 / 路由 / 渲染封进厂商 jar）→ 移交反编译并标法律（EULA）约束
- 拿不准 → 偏放行当候选移交，别默认判它"不在关键路径"漏移交

移交时随候选附坐标与仓库线索：`groupId:artifactId:version` + manifest 声明 + 私服 `<repositories>` / `settings.xml` mirror 配置，便于下游按坐标取件（sources 优先）。

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标项目实际依赖栈动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 所有依赖管理工具的 manifest + lock 文件都已识别（多语言项目并存时不漏）
- [ ] 每个生态都有完整 SBOM（传递依赖全展开，不只是直接依赖）
- [ ] CVE 库匹配覆盖：通用扫描器（Trivy / Grype）+ 语言特定工具（govulncheck / pip-audit / npm audit / cargo audit）
- [ ] 每条 CVE 命中按"是否在关键路径"判定（直接调用 / 传递 / 归属即触发 / 反射 / 不在路径），不停留在"命中"
- [ ] 容器基础镜像 + 安装层 OS 包独立扫描（Dockerfile 存在时）
- [ ] lock 与 manifest 一致性核验（防构建漂移）
- [ ] 关键路径上的闭源依赖已作为反编译候选移交 [dependency-decompile](../dependency-decompile/SKILL.md)
- [ ] 不在 NVD / GitHub Advisory 覆盖的恶意 / typosquatting 包独立排查

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 [dependency-decompile](../dependency-decompile/SKILL.md) / `result-with-file` 机器消费；产物聚合或省略会让"该依赖该 CVE 是否落地"无法溯源，因此是刚性要求。

### 白盒判定上限

本能力作为 SCA 粗筛 + 关键路径判定的原子能力，判定上限为 `static-confirmed`（CVE 命中 + 关键路径可达性已证），**不等于动态 confirmed**。升级路径：

| 状态 | 判据 | 上限 |
|---|---|---|
| `static-confirmed` | CVE 命中 + vulnerable function 在调用链上 / 归属即触发 | 白盒上限，落 `status=needs_review` + `confidence=static-confirmed` |
| `static-unknown` | CVE 命中但反射 / 配置驱动消费 / 闭源依赖深处 | 不能默认 not_vulnerable，落 `status=needs_review` + 标 `unknown` 原因 |
| `not_vulnerable` | CVE 命中但 vulnerable function 仅在 test scope / 未启用功能 / 版本号实际是 vendor patch | 必须留判定证据 |
| `partial-coverage` | SBOM 提取不全 / lock 缺失 / 闭源依赖未反编译就下"已审"结论 | 结论降级 |

**特殊规则**：归属即触发类（Log4Shell 风格——库引入即装载危险逻辑）即便业务未直接调用也按 `static-confirmed` 处理（why：库装载逻辑本身已触发，业务调用与否不改变风险落地）。

**升级路径**（白盒不能独立给 confirmed）：
- 走 graybox 流程：用静态可达性候选指导黑盒端 PoC
- 黑盒拿到强证据（实际触发 + 可观测效果）后，结论从 `static-confirmed` 升为 `confirmed`

**禁止**仅凭 CVE 命中直接判 `confirmed`——无可观测效果证据，仅依赖坐标匹配不构成动态利用。

### 产物契约（必须遵守）

**为什么这里是「必须」**：产物结构是下游机器消费的接口——`(package, version, cve_id, reachable)` 四元组任一不同即独立成行，聚合或省略会让 `result-with-file` 计数闸门失效，并让 [dependency-decompile](../dependency-decompile/SKILL.md) 无法回溯到具体依赖坐标。

每确认一条 CVE 命中**立即** append 一行到 `shared/coverage-ledger/findings/dependency-audit.jsonl`，不等汇总阶段回头整理（why："事后总结"是聚合 / 区间 / "等 N 个 CVE" 省略的根源）：

```json
{
  "id": "dep-001",
  "title": "log4j-core 2.14.1 含 Log4Shell (CVE-2021-44228)",
  "severity": "critical",
  "cwe": "CWE-502",
  "source": "org.apache.logging.log4j:log4j-core:2.14.1",
  "sink": "JndiManager.lookup",
  "entry_point": "systemic",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "file_location": "pom.xml:42",
  "source_report": "dependency-audit",
  "description": "..."
}
```

字段约束：
- `id` 带 `dep-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（粗筛默认 `needs_review`）
- `confidence ∈ static-confirmed | static-unknown | needs-dynamic-confirmation`
- `(package, version, cve_id, reachable)` **四元组任一不同即各自独立成行**——禁止合并折叠
- `source` 填依赖坐标（`groupId:artifactId:version` / `pkg@version`）
- `sink` 填该 CVE 影响的 vulnerable function / class
- `entry_point` 填 `systemic`（SCA 没有 HTTP 入口点）；若 CVE 仅在特定 HTTP 端点触发可填具体入口
- `file_location` 填发现该坐标声明的 manifest / lock 文件 `file:line`

**禁止**：
- 聚合计数（"3 个 Critical CVE + 12 个 High"）—— 丢失了具体坐标，下游无法消费
- "等" / "..." / "（其余 N 条略）" 省略 finding —— 看似覆盖完整实则漏检
- 同一组件多个 CVE 合并成一行 —— 每个 CVE 独立成行
- 因传递依赖多就只列直接依赖却宣称"完整审计已完成"

### 反例义务（必须遵守）

> **why**：SCA"已完整覆盖"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该项目依赖侧无风险"。

写"未发现 CVE"或"依赖已全部审计"前，产物必须包含：
- SBOM 提取完整性证据（每个生态都生成了完整传递依赖列表）
- CVE 库匹配覆盖证据（用了哪些扫描器、CVE 库版本、扫描日期）
- 关键路径上的闭源依赖移交清单（移交给 [dependency-decompile](../dependency-decompile/SKILL.md) 的坐标列表）
- `static-unknown` 单元格的具体原因（反射 / 配置驱动 / 闭源深处）

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库（共享章节）

### FP（看似命中实际不构成）

**反例 1：CVE 命中但 vulnerable function 仅在 test scope**

- 抽象规则：CVE 影响的代码路径仅在测试期间触发，生产构建不打入
- 具体场景：`junit` 含 CVE 但 `<scope>test</scope>`；`devDependencies` 里的 `webpack-dev-server` 含 CVE 但生产构建不打入
- 关键识别特征：依赖声明带 `scope=test` / `devDependencies` / `build-time only`
- 排除方法：核对依赖在 manifest 中的 scope；标 `reachable=false` 并附 scope 证据；不在 prod runtime classpath 上的判 not_vulnerable

**反例 2：版本号命中但实际 vendor 了官方 patch**

- 抽象规则：manifest 写的版本号是脆弱版本，但 vendor / fork 已合入官方 patch
- 具体场景：`pom.xml` 写 `log4j-core:2.14.1` 但实际 jar 是公司内部 fork 已合入 JNDI 修复
- 关键识别特征：版本号与官方 CVE-affected 版本对得上，但 jar 内 `META-INF/MANIFEST.MF` 标了 vendor / fork 信息
- 排除方法：反编译 jar 看 `JndiManager.lookup` 实际代码；标 `reachable=false` 并附反编译证据

**反例 3：CVE 仅影响特定平台 / 配置**

- 抽象规则：某些 JS 包 CVE 仅影响 Windows / 仅影响某 Node 版本 / 仅影响某配置组合
- 具体场景：`tar` 的某 CVE 仅在 Windows 解压时触发；项目跑 Linux container 不命中
- 关键识别特征：CVE advisory 明确列出 affected platform / version range
- 排除方法：核对项目运行环境；标 `reachable=false` 并附环境证据

### FN（看似不命中实际是真洞）

**反例 4：传递依赖深度截断**

- 抽象规则：依赖图过深时工具默认截断，CVE 漏报
- 具体场景：`npm ls` 默认深度 / `mvn dependency:tree` 不展开 shaded jar
- 关键识别特征：扫描产出的依赖数远小于实际 lock 文件展开数
- 确认方法：用 `--all` / `--depth Infinity` 强制全展开；shaded jar 反编译看真实坐标

**反例 5：npm `optionalDependencies` 被消费**

- 抽象规则：`optionalDependencies` 工具默认按"可选"处理跳过审计，但项目实际安装并消费
- 具体场景：项目用 `fsevents` 作为 optional 但 darwin 环境实际加载；其中含 CVE
- 关键识别特征：`optionalDependencies` 块里出现，但 runtime 实际 `require`
- 确认方法：把 `optionalDependencies` 纳入扫描面；grep 项目源码是否实际 `require`

**反例 6：shaded jar / fat jar 含已知漏洞类但 manifest 看不到**

- 抽象规则：项目把第三方 jar 打成 fat jar / shade 重打包，manifest 不再写原坐标
- 具体场景：`uber-jar` 内部嵌入了 `log4j-core` 旧版本但 `pom.xml` 没有 log4j 声明
- 关键识别特征：`mvn dependency:tree` 看不到的依赖，但反编译 jar 能找到对应类
- 确认方法：用 Trivy / Grype 扫 jar 文件本身（按 class hash 比对）；按反编译产物找真实坐标

**反例 7：vendored 代码（直接 copy 进项目目录）失去依赖追踪**

- 抽象规则：开发者直接把第三方代码 copy 到项目目录（`vendor/` / `internal/`），不再走依赖管理
- 具体场景：Go 项目用 `vendor/`、PHP 项目直接 copy 一个老版本库到 `lib/`
- 关键识别特征：`vendor/` / `lib/` 目录里有看起来不像自家代码的文件
- 确认方法：对 `vendor/` 目录单独跑 Trivy / Grype 文件系统扫描

### 易混淆案例

**反例 8：CVE 命中但 vulnerable function 通过反射调用**

- 抽象规则：项目通过 `Class.forName` / 反射调用 vulnerable function，静态 grep 找不到
- 具体场景：`Class.forName(config.get("jndi.factory"))` 间接走到 vulnerable lookup
- 关键识别特征：grep vulnerable function 名找不到调用点，但项目有反射 / 配置驱动调用
- 排除方法：标 `reachable=static-unknown` + 反射点行号；不能默认 not_vulnerable

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到"依赖坐标 + 版本 + CVE 库匹配 + 项目源码 grep 可达性"为止。

下面这些情形 SCA **无法继续追踪**，必须标 `static-unknown`，**不允许**默认为 not_vulnerable：

1. **CVE 库覆盖盲区**：
   - 0day（未公开的漏洞，NVD / Advisory 还没收录）
   - 仅在私有 Advisory 数据库（公司内部 / 商业 SaaS）发布的 CVE
   - **处置**：标"扫描时间 + CVE 库版本"作为基线，告知用户该日期之后的新 CVE 未覆盖

2. **闭源依赖内部不可见**：
   - 三方 jar / dll / so / 闭源 SDK 的内部数据流
   - 同厂商自有闭源（私服自研包）的入口点 / 鉴权 / 路由都封在产物内
   - **处置**：归属即触发或位置拿不准时偏放行当候选移交 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译

3. **传递依赖图谱不可见的部分**：
   - shaded jar / fat jar 重打包后原坐标丢失
   - vendored 代码（直接 copy 入项目）失去依赖追踪
   - 动态加载的依赖（运行时 `Class.forName` / 插件机制）
   - **处置**：对 jar 文件本身 / vendored 目录单独扫描；动态加载点标 `static-unknown`

4. **反射 / 配置驱动消费**：
   - vulnerable function 通过反射 / 配置文件 / 注解处理器调用
   - **处置**：标 `reachable=static-unknown` + 反射点行号

5. **构建漂移**：
   - manifest 写的 range（`^1.0.0`）与 lock 实际锁定的版本不一致
   - dev / CI / prod 环境拉到不同版本
   - **处置**：以 lock 文件为准；多环境多 lock 时每个独立审计

6. **基础镜像层不可见**：
   - 多阶段构建（multi-stage build）最终镜像里看不到中间层 CVE
   - 用 `scratch` 基础镜像直接 copy 二进制——CVE 在源码侧但镜像层看不到
   - **处置**：对最终镜像 + 源码构建链各自扫描

**底线**：本能力写"该项目无 CVE 风险"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

---

## 12. 修复建议（共享章节）

### 源头治理（首选）

- **锁文件版本钉死**：所有项目都用 lock 文件（`package-lock.json` / `Pipfile.lock` / `poetry.lock` / `Cargo.lock` / `go.sum`），CI 强制校验 lock 与 manifest 一致
- **定期 audit**：CI 集成 `npm audit` / `pip-audit` / `govulncheck` / `cargo audit` 等，新 CVE 出现时自动告警
- **关键路径白名单**：明确"哪些依赖在生产消费"——非生产依赖（test / devDependencies）单独审计标准
- **自动化 PR 升级**：dependabot / renovate / `npm-check-updates` 等工具自动开 PR 升级依赖

### 边界过滤（次选，深度防御）

- **SBOM 注入 CI/CD 卡点**：构建阶段生成 SBOM（CycloneDX / SPDX），扫描后 CVE 命中阻断合并
- **制品仓库代理**：Nexus / Artifactory / JFrog 等做依赖代理，配置"拒绝已知漏洞版本"策略
- **包来源固定**：禁止从公共 registry 直接拉取，所有依赖必须经内部 mirror

### 兜底拒绝

- **运行时最小权限**：业务进程禁用不必要的系统能力（capabilities / seccomp 白名单）
- **依赖完整性校验**：使用 lock 文件 hash（`go.sum` / `package-lock.json` integrity）+ 包签名验证

### 关键路径上闭源依赖的特殊处置

- 同厂商自有闭源（无源码）→ 反编译追内部行为（[dependency-decompile](../dependency-decompile/SKILL.md)）
- 第三方异厂商商业闭源（决策封内部）→ 反编译 + 标法律（EULA）约束
- 不能反编译时 → 显式留缺口（`needs_review`），不能因为"廉价扫描没扫到"就放行

### 参考

- [OWASP Dependency-Check](https://owasp.org/www-project-dependency-check/)
- [GitHub Advisory Database](https://github.com/advisories)
- [NVD / CVE](https://nvd.nist.gov/)
- [OWASP Top 10 A06:2021 — Vulnerable and Outdated Components](https://owasp.org/Top10/A06_2021-Vulnerable_and_Outdated_Components/)
