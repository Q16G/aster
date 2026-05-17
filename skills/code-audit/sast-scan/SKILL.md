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

## 推荐输出模板

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

### 高置信结果
- ...

### 需要数据流确认
- ...

### 高噪声结果
- ...
```

## 禁止事项

- 不要只报“发现多少条”，不说明扫描面
- 不要把高噪声 SSTI/JNDI 直接混进主结论
- 不要因为 `--lang java` 就只看 `*.java`，忽略 XML / 配置 / 模板
- 不要在 XML mapper 没扫到时，仍宣称 SQL 注入已被完整覆盖
