---
name: dependency-decompile
description: 自研/闭源/无源码依赖的源码恢复 — 当 SCA 盘点、入口点/攻击面分析或数据流追踪遇到一个没有可读源码的依赖（编译产物 jar/war/class、闭源或自研二进制库、混淆代码）时，优先取官方源码、取不到再反编译恢复可分析源码供上游审计复用；产出按取证完整性标注来源与不确定性。
tags: code-audit,decompile,dependency,reverse,jar
when-to-use: 当代码审计需要分析一个无可读源码的依赖（编译 jar/war/class、闭源或自研二进制库、混淆代码），且它不是行为已知的常见公共库、又无法直接读到源码时——典型触发面：SCA 盘点出自研/不常见依赖、入口点/中间件逻辑落在无源码依赖里、数据流污点进入无源码依赖
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[artifact_path] [--lang java|python|js|dotnet|native]"
arguments:
  - artifact_path
  - lang
---

# 无源码依赖反编译（源码恢复）

## 目标与边界

本 skill 只做一件事：**当上游审计撞上一个没有可读源码的依赖时，恢复出可分析的源码**，解除"无源码"造成的分析盲区，再把恢复结果接回上游继续审。

边界（别越权）：
- **不查 CVE / 不做 SCA** —— 那是 `dependency-audit`；它负责发现候选并移交，本 skill 负责恢复源码。
- **不追源码内污点链** —— 那是 `dataflow-analysis`。
- 本 skill 的产物是"可分析的源码 + 来源/不确定性标注"，漏洞判定通常交回触发它的上游 skill。

## 三个触发面（需求从哪来）

反编译需求不止来自数据流，下面任一处遇到"无可读源码、又非常见库"的依赖，都应走本能力：

1. **SCA 盘点（来自 `dependency-audit`）**：枚举依赖时发现**自研 / 不常见、且无可读源码**的依赖，除查 CVE 外，把它列为**反编译候选**移交本 skill——这是最早能识别候选的环节。
2. **入口点 / 攻击面分析（来自 `project-framework-analysis` / `security-code-analysis`）**：入口点 handler、或信任边界上的过滤器/拦截器/中间件逻辑**封在无源码依赖里**（典型：自研鉴权 filter 打进闭源 jar），不反编译看清它实际做了什么，入口点判定与信任边界就是盲的。
3. **数据流追踪（来自 `dataflow-analysis`）**：污点传播/sink 进入无源码依赖，反编译恢复后再继续追链路，别直接判 `unresolved_gaps`。

## 触发 triage（默认不反编译，反编译是少数例外）

**绝大多数依赖都不该反编译**：要么是行为已知的常见库（按已知语义推理即可），要么能直接取到官方源码——这两类都不反编译。反编译有成本、产物还带混淆不确定性，只对"确属自研/闭源、又取不到源码"的少数依赖才做。命中某个触发面后，按以下顺序判断，命中即停：

1. **是行为已知的常见公共库吗？**（如 Spring、Jackson、fastjson、commons-*、Netty、lombok 等）
   是 → **不反编译**：按已知语义推理其 source/sink/副作用；若关注其已知漏洞，交 `dependency-audit` 查 CVE。
2. **本地已有现成源码吗？**（本环境按**无外网**处理：不发起 `mvn dependency:sources` / `pip download` / 公共制品仓检索等联网下载，只找本机已落盘的源码）
   有 → **取源码，不反编译**。官方/原始源码无混淆、名/行号可信，优先级高于反编译：
   - Java：`find ~/.m2 ~/.gradle -name '*-sources.jar' 2>/dev/null` 查本地缓存；jar/war 同目录、`WEB-INF/lib` 旁、解包目录里找配套 `-sources.jar`。
   - Python：很多 wheel/包目录里直接带 `.py` 源（非纯 `.pyc`），优先读这些。
   - JS：包内随附的 `.map` source map、或未压缩（非 `.min`）的发布文件。
   - native / .NET：随附调试符号（`.pdb` / DWARF）。
3. **拿不准是不是自研的？**（仅凭名称/坐标无法判断是常见库还是自研封装）→ 先做**轻量探查**定性，别直接整包反编译、也别跳过。按从便宜到贵的顺序：
   - **先从消费侧的 import / 包名推导（最便宜，根本不碰 jar）**：`rg '^import' 项目源码`，看实际引用了哪些外部包、用在哪（入口点/sink 附近才重要）、被多少文件引用（用得越多攻击面越大）；包名前缀直接暗示性质——项目特有前缀（`org.<项目名>.*` / `com.<公司名>.*`）多为自研封装，`org.apache.*` / `com.google.*` / `org.springframework.*` 等是公共库。**注意 starter 盲区**：Spring Boot starter 会自动注册自带的 controller/endpoint（逻辑在 jar 内、项目源码里看不到对应 import），所以 import 计数低的 starter 仍可能暴露大量 web 面——这类要结合入口点/框架枚举（见 `project-framework-analysis`）判断，别只按 import 数定攻击面。
   - 再看 jar 内廉价元信息（全离线）：`unzip -p x.jar 'META-INF/maven/*/pom.properties'` 取 `groupId/artifactId/version`（定坐标金标准），`META-INF/MANIFEST.MF` 的 Vendor / Implementation-Title 辅证。
   - 仍拿不准 → 用 `javap -p` 或**只反编译几个代表性类**速看（不整包反编译），据此定性：是常见库 → 回第 1 步当常见库处理；是自研/闭源 → 进第 4 步。
4. **确属自研/闭源、又取不到源码**（混淆、只有编译产物）→ 才真正动手恢复源码：先按下方「反编译捷径与快捷判定」用最省力路径，仍不够才走「Java playbook」整包反编译。

## 反编译捷径与快捷判定（离线优先，能不反编译就不反编译）

反编译有成本、产物还可能失真，下面这些**极低成本的本地动作**经常让你少反编译甚至不反编译。按"先白嫖信息 → 再精准下刀 → 最后才整包"的顺序用，全部离线、不碰外网：

### 捷径 A：不反编译就拿到等价信息

- **`pom.properties` 一键定坐标**：`unzip -p x.jar 'META-INF/maven/*/pom.properties'` 直接拿 `groupId/artifactId/version`，据此判常见库 / 找本地 `-sources.jar`，比看 MANIFEST 猜准得多。
- **翻本地缓存，源码可能早在硬盘上**：`find ~/.m2 ~/.gradle -name '*-sources.jar' 2>/dev/null` —— 命中直接 unzip 读，0 反编译。
- **`spring.factories` / `META-INF/spring/*.imports` 明文列自动注册点**：`unzip -p starter.jar 'META-INF/spring*'` —— starter 自动装配的 AutoConfiguration / Controller 类名在这明文列着，直接破"import 计数看不到的 starter 盲区"，定位 jar 内 endpoint 不用反编译整包。

### 捷径 B：只取"够用的信息"，不反编译方法体

- **javap 三连，决定要不要深挖**：
  - `javap -p X.class` → 类/方法/字段签名，看 API 形状（够判 source/sink 角色就停）。
  - `javap -v X.class` → 常量池 + class 版本号（major 61 = JDK17…）+ **注解**（不反编译就看到 `@RestController`/`@RequestMapping`，立判是否 web 入口）。
  - `javap -c X.class` → 字节码，留给关键逻辑的交叉验证（见取证完整性）。
- **strings/rg 在 `.class` 二进制里搜常量先定位**（最被低估的捷径）：你要找的往往就是"哪个类拼了这条 SQL / 执行了命令 / 配了这条路由"。`strings x.jar | rg -i 'select |exec|/api/|password'`，或对解包后的类目录 `rg -a <关键字>` —— 命中哪个类，只反编译那一个。

### 捷径 C：确需反编译，走精准不走整包

- **只反编译目标类**：`unzip x.jar 'com/foo/Target.class'` 单取出来再喂反编译器；大 jar 几千类，整包全是噪音。
- **jadx 加速**：`jadx --no-res --no-imports -j <核数> -d out x.jar`（跳资源、多线程，大 jar 快数倍）。
- **混淆专项**：`jadx --deobf` 自动给 `a.b.c` 生成稳定名，跨类追踪不再对着混淆名抓瞎。

### 快捷判定矩阵（极低成本信号 → 一眼定动作）

| 信号（成本极低） | 判定 |
|---|---|
| `pom.properties` 命中公共坐标 / 包名 `org.apache.*`·`com.google.*`·`org.springframework.*` | 公共库 → 不反编译 |
| 本地缓存或包内有配套 `-sources.jar` | 取源码 → 不反编译 |
| 包名 `org.<项目>.*`·`com.<公司>.*` 且无公共坐标 | 自研 → 候选 |
| 类名 `a.a.a`/`o0Oo` + 字符串常量被加密/缺失 | 混淆闭源 → 强候选，上 `--deobf` |
| import 计数高且在 sink/入口点附近 | 攻击面大 → 优先 |
| import 计数低但 `spring.factories` 注册了 Controller | starter 盲区 → 仍要看 |

### 捷径的边界：定性可抽样，定论必须覆盖重点（强约束）

捷径是为了**更快定位重点**，不是为了**少看**。抽样 / 单类反编译 / `javap` 速看只支持「**路由定性**」——判断是不是自研、要不要继续深挖；**不支持下否定结论**。三条闸门：

- **"非漏洞 / 行为安全 / 无此能力"这类否定结论，前提是已把触发本次恢复的目标面完整反编译并读过**：即那个入口点 handler、命中的 sink 类、信任边界上的鉴权/过滤/加解密逻辑所在类**及其调用链**，而不是"我抽看的那几个类没发现问题"。目标面没覆盖到，就**没有资格判 `not_vulnerable`**。
- **覆盖不全 / 反编译失败 / 关键字符串被加密解不出 → 判 `needs_review`，并写明"已覆盖 X、未覆盖 Y、缺失原因"**，把未覆盖目标当作缺口**交回上游**继续追，绝不用"局部无发现"替代完整结论。同口径见 `dataflow-analysis`「别因看不到就直接判 unresolved」、`common/closure-verification.md`「降级 ≠ 删除、宁可 suspected 不夸大」。
- 一句话：**捷径帮你"快速找到该看的"，但"该看的"必须真看到**；快不等于可以漏，欠覆盖优于误判无害。

## Java playbook（主战场）

Java Web 审计里最常见：依赖只有编译 jar/war/class、或框架把逻辑封进自研 jar。

1. **定位与解包**
   - war/ear 先解包：`unzip -o app.war -d app_war`，关注 `WEB-INF/lib/*.jar`（依赖）与 `WEB-INF/classes/`（自身字节码）。
   - 嵌套 fat-jar 同理逐层 `unzip`。
2. **优先 sources.jar**（同 triage 第 2 步）：拿到就直接 read_file，跳过反编译。
3. **反编译器（jadx 首选；需要交叉对照时再上第二个，先探本机可用性 `command -v`）**：
   - jadx：`jadx -d out_src app.jar`（整包反编译，可读性好，首选；加速/混淆/单类参数见上方「捷径 C」）。
   - 第二反编译器（jadx 还原可疑、需比对字节码语义时用）：本机若装了 CFR `java -jar cfr.jar app.jar --outputdir out_src` 或 Procyon `java -jar procyon-decompiler.jar app.jar -o out_src`；都没装可退到 IntelliJ IDEA 自带的 fernflower——`find /Applications ~ -name 'java-decompiler*.jar' 2>/dev/null` 定位后 `java -cp <jar> org.jetbrains.java.decompiler.main.decompiler.ConsoleDecompiler <in.jar> <outdir>`。
   - 单类速查签名/常量/字节码：`javap -p -c -constants Target.class`（无需整包反编译，定位某个方法时最快）。
4. **反编译后**：`rg` 在 `out_src` 里定位上游关心的类/方法/调用链（source、sink、鉴权、过滤、加解密等），按需 read_file 精读。
5. **混淆处理**：若类名/方法名被混淆成 `a.b.c`，**标注"名字不可信"**，改按结构、字符串常量、调用关系、常量池来识别语义，不要拿混淆名当语义证据。

## 其余语言（轻量带过，确需才深入）

- **Python**：`.pyc` → `decompyle3` / `uncompyle6`（与字节码版本强相关，高版本可能失败，需本机已装）；失败回退到 triage 第 2 步——在本地包目录找随包 `.py` 源，不联网取 sdist。
- **JS**：压缩/混淆代码 → 优先找配套 `.map` source map 还原；无 map 则 `prettier` 美化 + 反混淆，仅恢复到能读懂控制流即可。
- **.NET**：`ilspycmd Target.dll -o out_src`（ILSpy 命令行）。
- **native（.so/.dll/可执行）**：先 `strings` / `nm -D` / `objdump -d` 做符号与反汇编速查；确有深挖必要再上 Ghidra headless（`analyzeHeadless`）——成本高，仅在该依赖确属分析关键路径时。

## 工具不可用时的 fallback（不许直接结束）

沿用 `dataflow-analysis` 的口径：反编译器没装 / 反编译失败，**也不能直接判"无法分析"留空**。至少做最低限度结构盘点：

- Java：`javap -p`（看类/方法签名）、`unzip -p app.jar META-INF/MANIFEST.MF`（看版本/主类/依赖声明）、`strings` 抓常量字符串（SQL 片段、URL、命令、密钥特征）。
- 显式声明：`decompile_available: false`、用了哪些 fallback 手段、已尽力提取到的信息、仍缺失的部分。
- **不留空、不伪造**——拿不到就如实写拿不到，绝不编造"看起来该是这样"的方法体。

## 恢复后如何接回上游

- 把恢复源码当作**带不确定性的源码**投喂回触发它的上游分析：SCA 候选 → 看清依赖真实行为；入口点/中间件 → 还原信任边界与鉴权逻辑；数据流 → 续追 source/sink/调用边界。
- 不要把反编译当独立任务的终点；目标始终是**解除上游那一步的"无源码"盲区**，让原分析能继续闭环。

## 取证完整性（强约束）

引用恢复代码做证据时，必须遵守 `common/closure-verification.md` 的取证完整性条款（用 read_file 复用其正文，勿在此复制）：

- **标注来源**：如"反编译自 `app.war!WEB-INF/lib/foo.jar` 的 `com.x.Bar#doFilter`，工具 = jadx"。
- **标注不确定性**：混淆环境下方法名/局部变量名可能非原始、行号非源始行号。
- **不得伪装成项目源码**：反编译产物与项目真实源码必须可区分，绝不冒充。

## 发现即落行（coverage-ledger/findings）

仅当你在恢复代码中**直接确认**了漏洞/需复核项时，**立即** append 一行规范化 jsonl 到 `shared/coverage-ledger/findings/dependency-decompile.jsonl`；若只是完成源码恢复、判定交回上游 skill，则由上游落库，避免双重记账。

一发现一行，**绝不写区间/计数/抽样**，字段：

```json
{"id","title","severity","cwe","source","sink","entry_point","status","confidence","file_location","source_report","description"}
```

- `id` 带前缀全局唯一（如 `dec-001`）。
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`。
- `source` / `file_location` 标注反编译来源（含产物路径与工具）；混淆环境下 `confidence` 酌情降级。
- 下游 `result-with-file` 直接消费这些 jsonl 机械派生 `findings-index.md` 并做计数闸门，你无需再手写索引。
