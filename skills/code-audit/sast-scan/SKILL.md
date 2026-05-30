---
name: sast-scan
description: 多语言多介质静态应用安全扫描（SAST）— 自动检测 RCE/SQLi/XSS/XXE/SSRF/命令注入/反序列化/路径穿越等结构化漏洞模式，覆盖源码、XML 配置、模板文件，输出覆盖声明与分桶结果。
tags: code-audit,sast,semgrep
when-to-use: 当需要对代码进行静态安全扫描、建立高价值漏洞候选集、发现强 sink 或动态 SQL/模板/配置风险时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path] [--lang java|go|python|js|php|c]"
arguments:
  - target_path
  - lang
---

# SAST 静态安全扫描（Semgrep）

> **只使用本地规则扫描**。不要使用 `--config auto`、`--config p/xxx` 等在线规则。所有规则已内置于本地。

## 目标

这个 skill 的职责不是“跑完 Semgrep 就结束”，而是：

1. 建立**高价值候选集**
2. 明确**实际扫描面**
3. 把结果分成**高置信 / 需数据流确认 / 高噪声**
4. 为后续 `dataflow-analysis` 和业务逻辑复核提供输入

它偏向发现：

- SQL 注入、命令执行、代码执行、路径穿越、文件上传、XSS、SSRF、硬编码密钥
- MyBatis `${}`、模板原样输出、危险反序列化、危险脚本执行
- Cookie/session/request-attribute 等信任边界问题的初筛线索

## 本地规则路径

ASTER 启动时会把本地嵌入规则提取到：

```text
~/.aster/rules/
├── java/
├── go/
├── python/
├── javascript/
├── php/
└── c-cpp/
```

每个语言目录下同时包含：

- 自建高价值规则
- `community/` 子目录中的 Semgrep 社区高质量规则

## 扫描流程

### 第一步：检查 Semgrep

```bash
semgrep --version
```

若未安装，引导用户安装 `semgrep`。

### 第二步：识别语言与框架信号

优先通过目录与特征文件判断语言和框架，而不是只看扩展名：

| 语言 | 特征文件 | 需要额外关注 |
|------|---------|-------------|
| Java | `pom.xml`、`build.gradle`、`*.java` | `mapper.xml`、`application*.yml`、`*.properties`、模板目录 |
| Go | `go.mod` | `config/*.yaml`、模板、SQL 构造层 |
| Python | `requirements*.txt`、`setup.py`、`pyproject.toml` | `settings.py`、模板、Jinja2/Flask/Django 配置 |
| JS/TS | `package.json`、`*.js`、`*.ts` | 服务端模板、SSR、配置与中间件 |
| PHP | `composer.json`、`*.php` | Blade/Twig/Smarty、配置与上传点 |
| C/C++ | `Makefile`、`CMakeLists.txt` | 命令执行、内存安全、格式化字符串 |

若用户通过 `--lang` 指定语言，可跳过自动检测，但仍要补做框架信号盘点。

### 第三步：构建扫描面

不要把扫描面缩成“只有源码”。至少按语言保证以下介质进入扫描面：

#### Java 默认扫描面

- `*.java`
- `**/mapper/**/*.xml`
- `**/*.properties`
- `**/*.yml`
- `**/*.yaml`
- 常见模板目录：`templates/`、`views/`、`WEB-INF/`

#### Java 项目的强制要求

若识别到任一信号：

- `org.mybatis`
- `mybatis-spring`
- `mapper/` 目录
- `Mapper.xml`

则必须确认 XML mapper 已进入扫描面。  
若 XML mapper 数为 `0`，不得给出“完整审计已完成”的结论，必须输出阻断性告警。

### 第四步：执行扫描

标准命令：

```bash
semgrep scan --config "$HOME/.aster/rules/<lang>" <target_path> --json --timeout 600 --max-memory 4096
```

建议额外排除明显无关目录：

```bash
--exclude .git --exclude node_modules --exclude vendor --exclude dist --exclude build --exclude out --exclude target
```

> **超时提示**：semgrep 扫描大目录常超过默认上限，通过 bash 工具执行时显式传 `timeout_ms`（如 `600000`）避免被提前截断；被取消时整棵进程树（含 `semgrep-core`）会被自动清理，不会残留。

### 大项目分批扫描

大型项目（monorepo、文件数巨大）一次性扫整个目录有三个风险：被 bash 超时杀掉导致整次结果全丢、`--max-memory` 下 OOM、产出量过大难以逐条处理。

**何时触发**：执行扫描前先用 `list_files` 估算扫描面文件数。`list_files` 默认上限就是 5000、最高 20000，超出会被截断——这本身就是信号。**若扫描面文件数超过 5000，进入分批模式。**

**如何切分**：按顶层模块/目录边界切分，**不要机械地按「每 5000 文件一片」切**。优先沿项目已有的模块边界走，例如：

- maven 多模块的各 module 目录
- go 的各子 module / 子服务目录
- monorepo 下的 `service-a/`、`service-b/`、`web/` 等顶层目录

每个子目录作为一次独立 `semgrep scan` 的 `<target_path>`。

**每批命令**：复用上面的标准命令，只把 `<target_path>` 换成子目录，`--json --timeout 600 --max-memory 4096` 与排除项保持不变，并通过 bash 显式传 `timeout_ms`：

```bash
semgrep scan --config "$HOME/.aster/rules/<lang>" <module_path> --json --timeout 600 --max-memory 4096 \
  --exclude .git --exclude node_modules --exclude vendor --exclude dist --exclude build --exclude out --exclude target
```

**单批失败隔离**：某批超时或 OOM 时，把该模块明确记入「扫描缺口」，其余批的结果仍然有效，**不得因为一批失败就放弃全部**。

**结果归并**（关键）：分批是手段，最终仍必须输出**一份**报告：

- 覆盖声明合并为一份：扫描面统计（文件数 / XML mapper / 配置 / 模板）为各批之和，并列出本次实际分了哪几批、每批对应哪个目录。
- 三个分桶（high_confidence / needs_dataflow_confirmation / high_noise）跨批统一归并、统一去重后再输出。
- 仍受下面「输出要求」的全部硬约束：禁止聚合计数、每个 finding 独占一行、不得用「等/略」省略。**分批不是省略 finding 的借口。**

## 输出要求

### 1. 覆盖声明（必须输出）

报告里必须先写清本次扫到了什么，而不是直接开始列告警。至少包含：

- 扫描目标
- 识别语言
- 规则来源
- 扫描面统计
  - Java 文件数
  - XML mapper 数
  - 配置文件数
  - 模板文件数
- 识别到的框架信号
  - Spring
  - MyBatis
  - Thymeleaf
  - Freemarker
  - 其他显著框架
- 扫描盲区 / 缺口

### 2. 结果分桶（必须输出）

所有结果必须分为三类：

- `high_confidence`
  - 强 sink、直接危险 API、明确动态 SQL、明确危险配置
- `needs_dataflow_confirmation`
  - source/sink 已接近成立，但还需要 `dataflow-analysis`
- `high_noise_patterns`
  - 已知容易大批量误报的模式，如某些 SSTI/JNDI 审计规则

默认主结论只先展示前两类。  
高噪声桶单独列出，不允许它们挤占主结论。

### 3. 逐条分析规则

对每条发现至少做以下判断：

1. 是否真有用户可控输入
2. 是否可达危险 sink / 动态 SQL / 权限决策
3. 是否已有明确防护
4. 该项属于哪类：
   - `Confirmed by pattern`
   - `Needs dataflow confirmation`
   - `Semantic review required`
5. 如果能从代码上下文判断该 sink 的 controller 入口点（方法名 + URL），注明入口点信息，方便下游按入口点汇总

## Java 项目的特别要求

对 Java Web 项目，除了常规 sink 规则，还必须额外注意：

- `request.getAttribute(...)`、`Cookie`、`session` 参与权限或身份流转
- `mapper.xml` 中 `${}`、动态条件、动态排序
- controller/service/mapper 三层之间的参数一致性
- 登录、鉴权、权限边界不一定会直接命中危险 API

所以 Java 项目扫描结束后，若命中以下任一线索：

- `needs_dataflow_confirmation`
- `Cookie` 参与分支判断
- `session` 写入身份字段
- controller 参数同时包含 owner/operator 与 resource/target

则应继续调用：

- `dataflow-analysis`
- 或 `business-logic-auth-review`

## 输出模板（必须遵循）

```text
## SAST 扫描报告

### 覆盖声明
- 扫描目标：<target_path>
- 检测语言：Java
- 规则来源：本地规则（~/.aster/rules/java）
- 框架信号：Spring, MyBatis, Thymeleaf
- 扫描面：
  - Java files: 128
  - XML mappers: 14
  - Config files: 9
  - Templates: 6
- 扫描缺口：<若有>

### 高置信结果（逐条列出，每个 finding 独占一行）

#### CWE-22 路径穿越 / 任意文件下载（N 条）
- [high_confidence] path-traversal: 文件下载路径拼接 @ AccTransactionController.java:102 (source: request.getParameter("filePath"), sink: new File())
- [high_confidence] path-traversal: 文件下载路径拼接 @ AccTransactionController.java:158 (source: request.getParameter("name"), sink: FileInputStream())
- [high_confidence] path-traversal: 文件下载路径拼接 @ PersPersonController.java:545 (source: params.get("photo"), sink: new File())
（此处为模板示意，实际输出时必须逐条列完所有 finding，不得省略）

#### CWE-89 SQL 注入（N 条）
- [high_confidence] sql-injection: MyBatis ${} 动态拼接 @ UserMapper.xml:42 (source: ${name}, sink: SELECT)
- [high_confidence] sql-injection: MyBatis ${} 动态排序 @ OrderMapper.xml:78 (source: ${orderBy}, sink: ORDER BY)
（此处为模板示意，实际输出时必须逐条列完所有 finding，不得省略）

### 需要数据流确认（逐条列出）
- [needs_dataflow] deserialization: ObjectInputStream.readObject @ MsgHandler.java:88 (source: socket input, sink: readObject)
（此处为模板示意，实际输出时必须逐条列完，不得省略）

### 高噪声结果（逐条列出）
- [high_noise] ssti-pattern: 模板变量输出 @ views/user.ftl:12 (source: model.name, sink: ${})
（此处为模板示意，实际输出时必须逐条列完，不得省略）
```

> **格式要求**：每个 finding 独占一行，格式为 `- [桶名] <rule_id>: <简述> @ <file>:<line> (source: <source_expr>, sink: <sink_expr>)`。按 CWE 分组便于阅读，但每条必须独立列出。总数必须与逐条列出的条目数一致。

### 反例（禁止以下写法）

```text
### 高置信结果
- 高置信 50 处任意文件下载 + 13 处路径穿越，分布在 AccTransactionController、
  PersPersonController、BaseController、IvsSnapController、SystemController 等
- 发现 8 处 SQL 注入，主要集中在 mapper 层
```

> 问题：聚合计数（"50 处 + 13 处"）丢失了 63 个具体位置；"等"省略了剩余 Controller；"8 处 SQL 注入"只有计数没有逐条列出。

## 禁止事项

- 不要只报“发现多少条”，不说明扫描面
- 不要把高噪声 SSTI/JNDI 直接混进主结论
- 不要因为 `--lang java` 就只看 `*.java`，忽略 XML / 配置 / 模板
- 不要在 XML mapper 没扫到时，仍宣称 SQL 注入已被完整覆盖
- 不要将多个 finding 合并为聚合计数（如"50 处任意文件下载 + 13 处路径穿越"），每个 finding 必须独占一行，包含 file:line
- 不要用"等"、"..."、"（略）"、"（其余 N 条略）"等方式省略 finding 列表中的条目，所有发现必须完整枚举
- 不要因为项目大就只扫了部分模块却宣称"完整审计已完成"；分批扫描时未扫到的模块必须显式列入"扫描缺口"
