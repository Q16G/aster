---
name: dependency-decompile
description: >-
  关键路径上无源码依赖的源码恢复——把闭源 jar / war / class / dll / so / 混淆包反编译回可读源码，
  供下游 dataflow-analysis / 入口点审计接力追踪。触发线索：dependency-audit 标记某依赖落在关键路径
  且无可读源码、入口点 / 信任边界逻辑封在闭源 jar 里（自研鉴权 filter 打进 jar、starter 自动注册的
  controller）、数据流污点跨入无源码依赖、安全/sink 决策发生在依赖内部（含自称标准协议的闭源鉴权
  SDK）。反向信号：行为已知的常见公共库（按已知语义推理即可）、官方源码可直接取到、依赖只是被
  调用的标准化数据/协议通道（如 JDBC 驱动，决策在调用方）。
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[artifact_path] [--lang java|python|js|dotnet|native]"
arguments:
  - artifact_path
  - lang
---

# 无源码依赖反编译（源码恢复）

本能力是 **Cat-B-Cross 反编译工具能力**——接 [dependency-audit](../dependency-audit/SKILL.md) 标记的闭源 / 关键路径依赖，提供源码恢复能力供 [dataflow-analysis](../dataflow-analysis/SKILL.md) / 入口点审计接力。本能力的产物是"可分析的源码 + 来源 / 不确定性标注"，**不做漏洞判定**——判定交回触发它的上游 skill。

## 1. 触发线索 / 适用信号

按"上游触发面 + 制品类型 + 攻击面可见性"维度识别本能力命中场景。

### 上游触发面

- **SCA 盘点**：[dependency-audit](../dependency-audit/SKILL.md) 枚举依赖时发现无可读源码、落在关键路径（入口点 / 信任边界 / 污点必经路径）上的依赖，列为反编译候选移交本能力
- **入口点 / 攻击面分析**：入口点 handler、信任边界上的过滤器 / 拦截器 / 中间件逻辑封在无源码依赖里（典型：自研鉴权 filter 打进闭源 jar、Spring Boot starter 自动注册的 controller / endpoint 逻辑在 jar 内）
- **数据流追踪**：[dataflow-analysis](../dataflow-analysis/SKILL.md) 的污点传播 / sink 进入无源码依赖，反编译恢复后继续追链路
- **递归触发**：反编译/恢复产物 A 的源码后，若 A 又引用无可读源码的依赖 B（自研内部模块、私服包、闭源 SDK），B 从本节起点重新过 triage

### 制品类型维度

- Java：jar / war / aar / class
- Android：APK / dex（外层包 + dex 字节码）
- .NET：DLL / EXE
- Go：编译产物（含符号表 vs 已 strip）
- Native：so / dylib / dll
- Python：`.pyc` / `.pyo` 已编译模块

### 攻击面可见性不对称（关键判据）

- "数据流污点跨入依赖"**外部可观测**——调用点能看到污点进了哪个依赖
- "入口点与信任边界逻辑"**封在产物内部、消费侧 import 分析看不见**（starter 盲区、自研 filter 打进 jar、编程式注册的 route 都属此类）

推论：对同厂商自有闭源，"用消费侧 import 证明它不在关键路径"是循环的——不打开就证不了。

### 反向信号（不命中本能力）

- 行为已知的常见公共库（Spring、Jackson、fastjson、commons-*、Netty、lombok 等）——按已知语义推理即可，已知漏洞交 [dependency-audit](../dependency-audit/SKILL.md) 查 CVE
- 官方源码可直接取到（本机 `~/.m2`/`~/.gradle` 已有 `-sources.jar`、包目录里有未压缩源、随附 source map 或调试符号）——直接读源码，优先级高于反编译产物
- vulnerable function 在项目源码里直接 grep 即命中——不需要打开依赖内部
- 依赖只是被调用的标准化数据/协议通道（如 JDBC 驱动，决策在调用方）——靠已知语义/标准规范/CVE 说清即可

---

## 2. 造成原因

**本能力 n/a**（原因：反编译工具不针对单漏洞，无成因可写。本能力只是"解除无源码盲区"，漏洞成因由触发它的上游 skill 承担——SQLi 看对应 SQL 注入审计 skill / [dataflow-analysis](../dataflow-analysis/SKILL.md)；鉴权绕过看 business-logic-auth-review；以此类推）。

---

## 3. 领域 source-sink 数据流模型

**本能力 n/a**（原因：反编译只恢复源码，不做数据流追踪——跨函数 / 跨文件 / 跨依赖的 source-to-sink 追踪由 [dataflow-analysis](../dataflow-analysis/SKILL.md) 接力。本能力的产物是"反编译后的可读源码"，作为 dataflow-analysis 的新 SSA IR 加载源）。

---

## 4. 常见类型

**本能力 n/a**（原因：反编译工具不针对漏洞分类。攻击变体属于具体漏洞的属性，由对应漏洞维度 skill 列出）。

---

## 5. 入口点定位

本能力的"入口点"是"需反编译的制品"——按制品类型 + 关键路径维度判优先级。

> 下列制品类型 / 项目栈仅作类似示例 不限于此；以目标实际栈为准。

### 按制品类型定位

| 制品类型 | 典型位置 | 工具与产物 |
|---|---|---|
| **Java jar / war / aar** | `WEB-INF/lib/*.jar`、`~/.m2/repository`、私服坐标 | jadx / CFR / Procyon / Fernflower → `.java` 源 |
| **Android APK / dex** | APK 包外层 + 内嵌 `classes*.dex` | `apktool` 解资源 + JADX 反编译 dex → `.java` 源 |
| **.NET DLL / EXE** | 项目 `bin/`、IIS 部署目录、`packages/` | ILSpy / dnSpy → C# 源 |
| **Go 编译产物** | 单个可执行文件（含符号 vs 已 strip） | 含符号表：`go-decompile` 系工具 + Ghidra；已 strip：Ghidra + 手工签名识别 |
| **Native so / dylib / dll** | `lib*.so` / `*.dylib` / `*.dll`，应用打包目录或系统库 | Ghidra / IDA Pro / Binary Ninja → 反汇编 + 反编译伪代码 |
| **Python `.pyc` / `.pyo`** | `__pycache__/`、site-packages | `uncompyle6` / `decompyle3`（强依赖字节码版本） |

### 按项目结构定位

- **Java Web**：先解 war/ear——`unzip -o app.war -d app_war`，关注 `WEB-INF/lib/*.jar`（依赖）与 `WEB-INF/classes/`（自身字节码）；嵌套 fat-jar 同理逐层 `unzip`
- **本地根本没有这个 jar**（典型：自研内部模块/私服包只在依赖声明里、不在产物里）→ 先按坐标取件（见 §8 「按坐标网络取件」），sources 优先；取到 plain jar 再反编译
- **Android**：APK 同时含资源与 dex，资源走 apktool、代码走 JADX；两者输出可合并到同一目录
- **Go / Native**：先 `file` 看产物类型与是否 strip，`strings` / `nm -D` / `objdump -d` 做符号与反汇编速查

### 关键路径优先级

本能力对每个制品按下列维度排优先级（详细 triage 见 [references/triage-decision.md](references/triage-decision.md)）：

- 同厂商自有闭源 → 归属即触发查证（候选），不要求先证在关键路径——反编译主战场
- 第三方异厂商商业 + 决策在依赖内 (b)（含 OEM / 低代码平台承载鉴权路由、自称标准协议的闭源鉴权 SDK） → 反编译并标法律
- 第三方异厂商商业 + 决策在调用方 (a)（标准化数据 / 协议通道，如 JDBC 驱动） → 不反编译，靠已知语义 + CVE 说清
- 归属 `unknown` + 在关键路径 → 偏放行当候选

---

## 6. 跨框架代码变体

**本能力 n/a**（原因：反编译针对二进制制品而非框架代码模式——反编译工具按字节码格式 / 制品类型选择，与"该制品是 Spring / Django / Gin 还是 Express"无关。反编译产物里的代码 pattern 由触发它的上游 skill 按对应漏洞维度的跨框架变体表去识别）。

---

## 7. 思考检查点

加载本能力时按这些问题思考：

- 这个制品反编译产出是否会被实际消费？——同厂商自有闭源因入口点封在产物内、消费侧不可观测，归属即触发；第三方依赖须先证落 (b) 决策在依赖内才触发。详见 [references/triage-decision.md](references/triage-decision.md) 维度 4
- 反编译质量是否足以追内部数据流？——是否已混淆（Proguard / R8 / ConfuserEx）、优化级别 -O3、native AOT、Go 已 strip；混淆等级会决定是否值得开 `--deobf` 还是要标 `static-unknown`
- 是否值得这次反编译的成本？——反编译有时间 / 上下文成本，产物还带不确定性；与项目漏洞总量对比、与上游触发面的紧迫性对比；维度 1（常见公共库）/ 维度 2（本机已有源码）/ 维度 4.2 落 (a) 任一命中即不反编译，详见 triage 详表
- 反编译产物是否需要写入仓库——law 边界（第三方商业件 EULA 可能禁逆向）、隔离要求（取件会发起外联）、产物存储位置；按 `## 产物契约` 落 `shared/decompiled/<package>-<version>/`
- 反编译后的代码引用时是否标"反编译产物"——避免与项目真实源码混淆；混淆环境下方法名 / 局部变量名可能非原始、行号非源始行号；按 [common/closure-verification.md](../../common/closure-verification.md) §取证完整性

---

## 8. 检测方法论 / 数据流追踪

### 触发条件判定（与 dependency-audit 协作）

只对"关键路径 + 闭源 + 可达性不可静态判定"的依赖反编译。详细 triage 决策树（包括决策点 (a)/(b)、归属调节、捷径边界四闸门）见 [references/triage-decision.md](references/triage-decision.md)。

主要分支概述（顺序仅作建议，可按现场调整）：

1. **行为已知的常见公共库** → 不反编译，按已知语义推理；已知漏洞交 [dependency-audit](../dependency-audit/SKILL.md) 查 CVE
2. **本机已落盘的官方源码** → 取源码读，0 反编译；翻 `~/.m2`、`~/.gradle`、包目录、source map、调试符号
3. **轻量探查定性**（公不公共）→ `rg` import / 包名前缀 / `pom.properties` / `MANIFEST.MF` / `javap -p` 单类速看
4. **非公共库 + 取不到源码**：按"关键路径 + 决策点 (a)/(b) + 归属"三步定动作——同厂商自有闭源归属即触发查证；第三方落 (b) 触发；落 (a) 免反编译

### 工具选择

按制品类型选定工具，**先探本机可用性**（`command -v <tool>`）：

| 语言 / 制品 | 主要工具 | 备选 / 交叉验证 |
|---|---|---|
| Java jar / war / class | jadx（首选，可读性好）| CFR / Procyon / IntelliJ Fernflower（交叉对照时用） |
| Java 单类速查 | `javap -p -c -constants Target.class`（签名 / 字节码 / 常量池） | `javap -v`（常量池 + 注解，破 starter 盲区） |
| Android APK / dex | apktool（解资源）+ JADX（反编译 dex） | dex2jar + jadx 兜底 |
| .NET DLL / EXE | `ilspycmd Target.dll -o out_src`（ILSpy 命令行） | dnSpy（GUI 复核） |
| Go 含符号表 | `go-decompile` 系 + Ghidra 辅助 | Ghidra `analyzeHeadless` |
| Go 已 strip / Native so / dylib / dll | Ghidra headless（`analyzeHeadless`） | IDA Pro / Binary Ninja（深挖时） |
| Python `.pyc` / `.pyo` | `decompyle3` / `uncompyle6`（与字节码版本强相关） | 失败回退：在包目录找随包 `.py` 源 |

### 反编译流程

1. **制品定位**：按 §5 找到目标制品物理路径（解包 war / ear、定位 jar/dll/so）
2. **本地无件兜底**：若该依赖须看内部但本地无件，按坐标取件——sources 优先（mvn `dependency:get -Dclassifier=sources` → `pip download` 取 sdist → npm registry tarball），sources 失败再取 plain jar 反编译；详细取件路径与防误拉闸门见下「按坐标网络取件」
3. **工具选定**：按制品类型 + 混淆等级（普通反编译器 vs 加 `--deobf`）
4. **反编译输出**：
   - Java：`jadx --no-res --no-imports -j <核数> -d out app.jar`（跳资源、多线程，大 jar 快数倍）；混淆 `jadx --deobf` 自动给 `a.b.c` 生成稳定名
   - 单类精准反编译：`unzip x.jar 'com/foo/Target.class'` 单取出来再喂反编译器；大 jar 几千类整包全是噪音
5. **SHA256 比对**（why：避免供应商被替换）：反编译前对原制品做 `sha256sum`，与 SCA 阶段记录的 hash 比对；不一致 → 标"供应链可疑"
6. **写入仓库**：按 `## 产物契约` 落 `shared/decompiled/<package>-<version>/` 目录，与项目源码分离
7. **标注来源**：每段引用都标"反编译自 xxx.class，工具 = jadx，方法名可能经混淆"——按 [common/closure-verification.md](../../common/closure-verification.md) §取证完整性

### 按坐标网络取件（捷径 D）

本地翻遍都没有 jar/源码、且该依赖须看内部时，按坐标去取——**sources 优先**（拿到官方 sources 即 0 反编译且可信）：

- 触发条件：同厂商自有闭源归属即触发；第三方依赖落 (b) 才触发
- 取件前过**防误拉闸门**：坐标的 `groupId:artifactId` 是否命中本 reactor 某个兄弟 module？是 → 直接 read 源码，禁止拉 jar 反编译（why：源码比反编译产物可信、且免外联）。例外：现场产物（war/jar）可能打包旧版/不同 build 的该模块 → 以产物为准
- **定坐标**：Maven 用 `mvn help:effective-pom` 解平展（裸读 pom 会拿到假版本，是最常见取错件的坑）；Gradle 用 `build.gradle(.kts)` / `gradle.lockfile`
- **私服 / 凭证**：让构建工具自己读 `~/.m2/settings.xml` / 项目 `<repositories>` / Gradle `repositories{}`；本能力不解析 URL、**不回显 / 不记录凭证**
- **取件命令**：`mvn dependency:get -Dartifact=g:a:v -Dclassifier=sources`；私服没进 settings.xml 时显式补 `-DremoteRepositories=id::default::https://<私服>/...`（解 `dependency:get` 的仓库盲区——单跑时通常只认 settings.xml 的 repo/mirror）
- **Python**：`pip download` 取 sdist（机制不同：不走 mvn，口径一致——先本地、本地无再按坐标取，源码优先）
- **取件成功 ≠ 可信**：plain jar 反编译产物照旧受 (b) 的"必须完整覆盖目标面"约束；`-sources.jar` 与现场部署的 `.class` 未必同源同版本——对 (b) 类关键件，sources 来源 / 版本存疑时与实际 class（`javap`/字节码）抽样交叉核对；SNAPSHOT 件标快照时间
- **外联与隔离**：取件会发起外联——在网络隔离的审计/渗透交付场景，自动对客户私服或公网仓库发请求可能违反隔离要求或在公网留下暴露被审计方的请求记录；environment 明确要求隔离时按交付约定决定是否放行
- **取不到怎么办**：私服 404/401、版本解析失败、断网 → 按 `## 产物契约` 留 `needs_review` + 显式缺口并写明原因，不直接结束

### 接力 dataflow-analysis

反编译后把产物路径告知 [dataflow-analysis](../dataflow-analysis/SKILL.md)，作为新的 SSA IR 加载源——`rg` 在反编译输出目录里定位上游关心的类 / 方法 / 调用链（source、sink、鉴权、过滤、加解密等），按需 `read_file` 精读。

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标制品动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 上游触发面已明确（SCA 候选 / 入口点 / 数据流 / 递归触发面）
- [ ] 按 [references/triage-decision.md](references/triage-decision.md) 过完维度 1-4，明确"为何要反编译此件"或"为何不反编译"
- [ ] 本地缓存 / 包目录 / source map / 调试符号已翻过（0 外联，sources 优先）
- [ ] 本地无件且须看内部 → 按坐标取件，sources 优先 → 失败取 plain jar
- [ ] 防误拉闸门已过（不把仓库内有 src 的兄弟 module 当外部依赖反编译）
- [ ] 工具选定与制品类型匹配（不混淆用普通反编译，混淆开 `--deobf`）
- [ ] 原制品 SHA256 已记录，与 SCA 阶段 hash 比对
- [ ] 产物落 `shared/decompiled/<package>-<version>/` 目录，未与项目源码混入
- [ ] manifest.jsonl 已 append 元数据行（package / version / hash / decompiler / 触发原因）
- [ ] 引用反编译代码做证据时已标"反编译自 xxx，工具 = X，方法名可能经混淆"
- [ ] 混淆环境下方法名 / 局部变量名标"名字不可信"，按结构 / 字符串常量 / 调用关系识别语义
- [ ] 反编译失败 / 取件失败 → 已落 `needs_review` + 显式缺口，未直接判"无法分析"留空

### 产物契约（必须遵守）

> **why**：反编译产物是下游 [dataflow-analysis](../dataflow-analysis/SKILL.md) / 上游审计 skill 的接口，结构 / 命名 / 来源标注缺失会让下游无法回溯源头、无法判断混淆不确定性、无法与项目真实源码区分。这是审计可追溯的硬底线，属交付契约不属检查建议。

**目录结构**：
- 反编译产物落到 `shared/decompiled/<package_name>-<version>/` 目录
- 目录命名按"包名 + 版本"——同厂商自有件 `<groupId>-<artifactId>-<version>`；第三方件以 Maven 坐标为准
- 与项目真实源码（`src/`）物理分离，避免与项目源码混淆

**manifest 落行**：每次反编译 append 一行 metadata 到 `shared/decompiled/manifest.jsonl`：

```json
{
  "package": "com.example.foo-bar",
  "version": "1.2.3",
  "artifact_sha256": "...",
  "decompiler": "jadx 1.4.7",
  "decompiled_at": "<ISO8601 timestamp>",
  "key_path_reason": "同厂商自有闭源，归属即触发；入口点封在 jar 内、消费侧不可观测"
}
```

字段约束：
- `package` 与 `version` 必填，对应取件坐标或现场制品标识
- `artifact_sha256` 填原制品 hash（反编译前 `sha256sum` 取得），用于供应链一致性校验
- `decompiler` 填实际工具名 + 版本
- `key_path_reason` 写**为什么这件值得反编译**——具体到触发面、归属判定、决策点 (a)/(b) 结论；不写"看起来该反"之类模糊表达
- 来源不确定时标"取自 `<私服/中央仓>` 坐标 `g:a:v` 的 sources"或"反编译自 `app.war!WEB-INF/lib/foo.jar`，工具 = jadx"
- 拉到的 plain jar 反编译产物保留混淆不确定性；SNAPSHOT 件标快照时间

**取证完整性**：引用反编译代码做证据时遵循 [common/closure-verification.md](../../common/closure-verification.md) §取证完整性——不在此复述，按 §6.4 引用：
- 标注来源（产物路径 + 反编译工具）
- 标注不确定性（混淆环境下方法名 / 局部变量名可能非原始、行号非源始行号）
- 不得伪装成项目真实源码

**发现接回上游**：
- 反编译只是恢复源码，漏洞判定通常交回触发它的上游 skill 落库
- 仅当本能力在恢复代码中**直接确认**了漏洞/需复核项时，append 一行 jsonl 到 `shared/coverage-ledger/findings/dependency-decompile.jsonl`（避免双重记账）
- 字段约束沿用 sast-scan 同口径：`id` 带 `dec-` 前缀全局唯一；`status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`；`source` / `file_location` 标注反编译来源（含产物路径与工具）；混淆环境下 `confidence` 酌情降级

---

## 9. 闭环要求（必须遵守）

**本能力 n/a**（原因：反编译不做漏洞判定，无 confirmed / suspected / not_vulnerable 三态。漏洞判定的闭环由触发它的上游 skill 承担——上游引用 [common/closure-verification.md](../../common/closure-verification.md)。本能力的"交付契约"是 `## 8 产物契约` 段的目录结构 + manifest 落行 + 来源标注，不在此重复闭环判定语义）。

---

## 10. 具象化反例库

**本能力 n/a**（原因：反编译工具不针对单漏洞，FP / FN 由具体漏洞维度 skill 列出。本能力的"失败模式"在 §11 静态分析边界统一表达——混淆 / 加壳 / native AOT / 取件失败等情形必须标 `static-unknown` 或 `decompile-incomplete`，不能默认为安全）。

---

## 11. 静态分析边界

> 反编译的能力上限是**还原可分析的源码**，不是"看清所有代码"。下面这些情形反编译质量受限，**必须明确标注**，不允许默认为"该制品无问题"或 not_vulnerable。

### 反编译能力受限的情形

1. **代码混淆（Proguard / R8 / ConfuserEx 等）**
   - 现象：类名 `a.a.a`、方法名 `o0Oo`、字符串常量加密 / 缺失
   - 处置：标 `static-unknown` + 标注混淆等级；开 `--deobf` 自动给稳定名但语义仍不可信；改按结构、字符串常量、调用关系、常量池识别语义，**不要拿混淆名当语义证据**

2. **激进优化级别**
   - native：`-O3` 编译后内联 / 循环展开 / 死代码删除，反编译伪代码与源码结构差距大
   - Go AOT：编译已 strip 符号；只能靠 Ghidra + 手工签名识别
   - .NET AOT / IL2CPP：托管语言原生 IL 信息丢失，ILSpy 失效
   - 处置：标 `decompile-incomplete` + 标注优化级别；反编译产物只能作"辅助参考"，不作单独定论依据

3. **加密 / 加壳**
   - 现象：APK 加固（爱加密 / 360 加固 / 梆梆）、.NET 加壳（ConfuserEx 抗调试）、native UPX / VMProtect
   - 处置：标"反编译失败 + 原因"（壳类型 / 加固厂商），**不能默认 not_vulnerable**；按 [references/triage-decision.md](references/triage-decision.md) 四闸门——降级为 `needs_review` 而非"无法分析留空"

4. **native 代码（C / Rust 编译后无符号）**
   - 现象：`.so` / `.dylib` / `.dll` 编译产物，无调试符号、无 RTTI
   - 反编译质量受限：Ghidra / IDA 输出的伪代码是"语义近似"而非真实源码——指针类型 / 结构体 / 函数边界都需人工矫正
   - 处置：仅作辅助；定论必须配合符号交叉验证（`strings` / `nm -D` / `objdump -d`）

5. **取件失败 / 反编译器不可用**
   - 现象：私服 404/401、版本解析失败、断网、反编译器没装、反编译过程 OOM / 超时
   - 处置：按 §8 fallback 段——至少做最低限度结构盘点（`javap -p`、`MANIFEST.MF`、`strings`），显式声明 `decompile_available: false` + 已尝试手段 + 仍缺失部分；不留空、不伪造、不直接结束

6. **跨制品边界（递归触发面）**
   - 反编译出的源码 A 又引用无可读源码的依赖 B（依赖的依赖）
   - 处置：B 从 §1 触发线索重新过 triage，不得因 B 是"依赖的依赖"停在第一层；递归边界由 triage 主闸门收敛（落 (a) 或外部可证不在关键路径即停）

### 白盒底线

**不假装看到看不到的代码**。本能力的可观测能力到反编译产物的可读源码为止——混淆名不是语义、伪代码不是真源、取件失败不等于安全。

- 反编译产物 + 关键路径上落 (b) 的依赖、以及同厂商自有闭源归属即候选但未查清内部的 → 不得用"走 CVE / 行为已知"假性闭环冒充安全
- 反编译失败 / 覆盖不全 / 关键字符串被加密解不出 → 判 `needs_review`，写明"已覆盖 X、未覆盖 Y、缺失原因"，把未覆盖目标当作缺口交回上游
- 不留空、不伪造——拿不到就如实写拿不到（含"私服取件失败"），绝不编造"看起来该是这样"的方法体

---

## 12. 修复建议

**本能力 n/a**（原因：反编译工具不针对漏洞，无修复路径可写。各漏洞修复路径由对应单漏洞维度 skill 给出——例如 SQLi 修复参对应 SQL 注入审计 skill / pentest 链路上的 §12；鉴权绕过参 business-logic-auth-review §12；以此类推）。

---

## 相关文件

- [references/triage-decision.md](references/triage-decision.md) — 完整 triage 决策树（维度 1-4、决策点 (a)/(b)、归属调节、快捷判定矩阵、捷径边界四闸门）
- [common/closure-verification.md](../../common/closure-verification.md) — 取证完整性 / 闭环判定共享口径
- [../dependency-audit/SKILL.md](../dependency-audit/SKILL.md) — SCA 候选 / CVE 查询上游
- [../dataflow-analysis/SKILL.md](../dataflow-analysis/SKILL.md) — 反编译产物的下游消费者（污点链续追）
- [../sast-scan/SKILL.md](../sast-scan/SKILL.md) — 粗筛产物作为本能力的另一条触发面
