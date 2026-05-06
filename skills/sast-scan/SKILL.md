---
name: sast-scan
description: 基于 Semgrep 的静态应用安全测试（SAST）- 650+ 条本地安全规则覆盖 6 大语言
tags: code-audit,sast,semgrep
when-to-use: 当需要对代码进行静态安全扫描、发现已知漏洞模式、检测强 sink 点时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path] [--lang java|go|python|js|php|c]"
arguments:
  - target_path
  - lang
---

# SAST 静态安全扫描（Semgrep）

> **⚠️ 只使用本地规则扫描，不要使用 `--config auto`、`--config p/xxx` 等在线规则。所有规则已内置于本地。**

## 本地规则路径

ASTER 内置了 650+ 条安全规则（自建规则 + Semgrep 社区 taint-mode 规则，已去除代码风格/配置类规则），启动时已自动提取到以下固定目录：

```
~/.aster/rules/
├── java/          # Java: 反序列化(fastjson/jackson/xstream/hessian/kryo)、SQL注入、JNDI、SSTI、XSS、XXE、SSRF、认证、加密 + 社区 taint 规则
├── go/            # Go: SQL注入(gorm/xorm)、命令注入、模板注入、反序列化、SSRF、认证、加密、竞态条件 + 社区 taint 规则
├── python/        # Python: SQL注入(psycopg2/sqlite3/sqlalchemy/django)、SSTI(mako/tornado)、反序列化(pickle/jsonpickle/shelve)、SSRF(requests/httpx/aiohttp)、XSS + 社区 taint 规则
├── javascript/    # JS/TS: SQL注入(knex/typeorm/sequelize)、SSTI(ejs/pug/handlebars)、反序列化(node-serialize/js-yaml)、原型污染(lodash)、XSS、SSRF + 社区 taint 规则
├── php/           # PHP: SQL注入(mysqli/pdo)、文件包含(LFI/wrapper)、XXE(simplexml/dom)、SSTI(twig)、反序列化、XSS、SSRF + 社区 taint 规则
└── c-cpp/         # C/C++: 缓冲区溢出(gets/sprintf/strcpy/memcpy/scanf)、命令注入(system/popen)、格式化字符串、内存安全 + 社区规则
```

每个语言目录下包含 `community/` 子目录，存放从 Semgrep 官方仓库同步的 taint-mode 安全规则。

## 扫描流程

### 第一步：检查 Semgrep

```bash
semgrep --version
```

若未安装，引导用户：`brew install semgrep` 或 `pip install semgrep`

### 第二步：识别项目语言

扫描目录结构，根据文件扩展名和特征文件判断语言：

| 语言 | 特征文件 | 规则子目录 |
|------|---------|-----------|
| Java | pom.xml, build.gradle, *.java | `java/` |
| Go | go.mod, go.sum | `go/` |
| Python | requirements.txt, setup.py, *.py | `python/` |
| JavaScript/TS | package.json, *.js, *.ts | `javascript/` |
| PHP | composer.json, *.php | `php/` |
| C/C++ | Makefile, CMakeLists.txt, *.c, *.h | `c-cpp/` |

若用户通过 `--lang` 参数指定了语言，跳过自动检测。

### 第三步：执行扫描

```bash
semgrep scan --config "$HOME/.aster/rules/<lang>" <target_path> --json --timeout 600 --max-memory 4096
```

示例（Java 项目）：
```bash
semgrep scan --config "$HOME/.aster/rules/java" /path/to/project --json --timeout 600 --max-memory 4096
```

多语言项目对每种语言分别执行：
```bash
semgrep scan --config "$HOME/.aster/rules/java" --config "$HOME/.aster/rules/go" /path/to/project --json --timeout 600
```

### 常用选项

- 排除目录：`--exclude vendor --exclude node_modules --exclude test --exclude .git`
- 仅高危：`--severity ERROR`
- 增量扫描：`--baseline-commit HEAD~1`

## 结果分析

### 1. 分级
- **CRITICAL/ERROR** — 高置信度漏洞，需立即处理
- **WARNING** — 中置信度，需人工确认
- **INFO** — 低置信度或最佳实践建议

### 2. 数据流确认标记
检查 metadata 中 `needs_dataflow_confirmation` 字段：
- `true` → 标记为"需要 SyntaxFlow 数据流确认"，在后续 `dataflow-analysis` 技能中做 topdef/bottomUse 追踪
- `false` 或缺失 → 仅靠模式匹配即可判定

### 3. 逐条分析
对每个发现：
1. 读取命中位置的上下文代码（前后 5-10 行）
2. 分析是否为真阳性（考虑框架特性、安全中间件、转义函数等）
3. 解释漏洞原理和潜在影响
4. 提供修复建议和安全编码示例
5. 标记明确误报并说明原因

## 输出报告格式

```
## SAST 扫描报告

### 摘要
- 扫描目标：<target_path>
- 检测语言：Java, Go, ...
- 规则来源：本地规则（~/.aster/rules）
- 发现总数：N（Critical: X, High: Y, Medium: Z, Low: W）
- 需数据流确认：M 条

### 发现列表（按严重程度排序）

#### [CRITICAL] rule-id — 漏洞标题
- 文件：path/to/file.java:42
- CWE：CWE-89 (SQL Injection)
- 代码片段：（高亮命中行）
- 分析：（真阳性/疑似/误报 + 理由）
- 修复建议：（代码示例）
- 数据流确认：需要 / 不需要
```
