---
name: dataflow-analysis
description: 数据流分析指南 — 使用 SyntaxFlow MCP 工具进行 topdef/bottomUse/注解查找和数据流追踪
tags: code-audit,dataflow,syntaxflow,mcp
when-to-use: 当需要对 semgrep 发现做数据流追踪确认，或进行跨函数/跨文件污点分析时
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path] [--lang java|go|python|js|php|c]"
arguments:
  - target_path
  - lang
---

# 数据流分析（SyntaxFlow MCP）

## 目标
通过 `syntaxflow` MCP Server 的 `ssa_compile` 和 `ssa_query` 工具，对代码进行数据流分析：**topdef**（定义溯源）、**bottomUse**（使用追踪）、**注解查找**。用于验证 semgrep 发现、追踪污点传播、分析鉴权缺失等。

## 前置条件
- `syntaxflow` MCP Server 已连接（通过 `/mcp` 检查状态）
- 若未连接，通过 TUI 的 `/mcp` 命令连接，或确认 `yak` 已安装（`yak version`）

## MCP 工具参考

### ssa_compile — 编译源代码

将源代码编译为 SSA 中间表示，持久化到数据库，可复用于多次查询。

**参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `target` | string | 是 | 项目目录路径 |
| `language` | string | 是 | 语言：`java`, `php`, `js`, `golang`, `yak`, `c`, `python` |
| `program_name` | string | 否 | 自定义程序名（推荐设置，便于复用）。不填则自动生成 |
| `base_program_name` | string | 否 | 增量编译：基于已有程序做差量编译，仅重编译变更文件 |
| `re_compile` | boolean | 否 | 全量重编译：删除旧数据从头编译。注意：这不是增量编译！ |

**使用模式：**
1. **首次编译**：`target` + `language` + `program_name` → 返回 program_name
2. **复用查询**：编译一次，后续直接用 program_name 做多次 ssa_query
3. **增量编译**（代码改动后）：设置 `base_program_name` = 之前的 program_name → 返回新的 diff program_name
4. **全量重编译**（罕见）：设置 `re_compile=true`

**缓存机制**：若 program_name 已存在且源码未变化，自动命中缓存，不重复编译。

### ssa_query — 执行 SyntaxFlow 查询

在已编译的 SSA 程序上执行 SyntaxFlow 数据流查询。

**参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `program_name` | string | 是 | ssa_compile 返回的程序名 |
| `rule` | string | 是 | SyntaxFlow 规则文本 |

**输出内容：**
- Alert Variables：命中的告警变量及其值（文件位置、行号、上下文代码）
- Other Variables：非告警捕获变量
- Check Messages：断言检查消息
- 每个值包含：字符串表示、文件路径、行列位置、周围代码上下文

## SyntaxFlow DSL 语法

### 核心操作符

| 操作符 | 含义 | 示例 |
|--------|------|------|
| `.` | 成员访问/调用链 | `Runtime.getRuntime().exec()` |
| `#->` | **topdef**（向上追踪定义来源） | `$var #-> as $source` |
| `-->` | **bottomUse**（向下追踪使用点） | `$var --> as $usage` |
| `?{}` | 条件过滤 | `*?{opcode: call}` |
| `as $var` | 捕获到变量 | `exec(*) as $sink` |
| `check $var then "msg"` | 断言非空 | `check $sink then "found injection"` |
| `alert $var` | 标记为告警发现 | `alert $sink` |

### 三大核心能力

#### 1. topdef — 定义溯源（Use-Def 链）
从变量使用处**向上**追踪到定义来源，判断数据是否用户可控。

```syntaxflow
// 追踪 exec() 参数的来源
Runtime.getRuntime().exec(* #-> as $source) as $sink;
check $sink then "found command execution";
alert $source;
```

```syntaxflow
// 追踪 SQL 查询参数来源
*.execute(* #-> as $source) as $sink;
check $sink then "found SQL execution";
alert $source;
```

#### 2. bottomUse — 使用追踪（Def-Use 链）
从变量定义处**向下**追踪到所有使用点，判断数据流向哪些危险函数。

```syntaxflow
// 追踪 request.getParameter() 的去向
*.getParameter(*) as $input --> as $usage;
check $usage then "user input flows to";
alert $usage;
```

```syntaxflow
// 追踪密码变量的使用
$password --> as $usage;
check $usage then "password variable used at";
alert $usage;
```

#### 3. 注解查找
查找带有特定注解的方法/类，用于定位 HTTP 入口点。

```syntaxflow
// 查找所有 Spring Controller 端点
*?{.annotation.*Mapping} as $endpoints;
check $endpoints then "found HTTP endpoint";
alert $endpoints;
```

```syntaxflow
// 查找所有 @RequestMapping 方法
*.annotation.RequestMapping as $mapping;
$mapping... as $methods;
alert $methods;
```

```syntaxflow
// 查找缺少 @PreAuthorize 的 Controller 方法
*?{.annotation.*Mapping && !.annotation.PreAuthorize} as $unprotected;
check $unprotected then "endpoint without auth check";
alert $unprotected;
```

### 数据流递归配置

SyntaxFlow 的数据流追踪（`#->` / `-->`）支持四种递归配置项，控制追踪行为：

| 配置项 | 语义 | 数据流行为 | 典型场景 |
|--------|------|-----------|---------|
| `until` | 匹配到即停止流动 | 命中后停止 | 找到第一个常量来源就停下 |
| `hook` | 对每个节点执行规则，不影响结果 | 始终继续 | 沿途收集辅助信息 |
| `include` | 正向过滤：仅保留匹配的 Value | 匹配保留，其余丢弃 | 只保留经过安全检查的路径 |
| `exclude` | 反向过滤：移除匹配的 Value | 匹配移除，其余保留 | 排除非常量值 |

**语法格式：**
```syntaxflow
// 单配置
$var #{until: `*?{opcode: const}`}-> as $result

// 多行配置（用 <<<TAG ... TAG 包裹）
$var #{hook: <<<HOOK
    *.fieldName as $fields
HOOK
}-> as $result
```

**实战示例：**

```syntaxflow
// until — 追踪到常量来源即停止（硬编码凭据检测）
$func(* #{until: `*?{opcode: const}`}-> ) as $hardcoded;
check $hardcoded then "hardcoded value found";
alert $hardcoded;
```

```syntaxflow
// hook — 沿途收集中间变量
$input #{hook: <<<HOOK
    *.toString() as $conversions
HOOK
}-> as $final;
alert $final;
```

```syntaxflow
// include — 只保留经过安全检查的路径
$sink #{include: `* & $sanitized`}-> as $safe_paths;
```

```syntaxflow
// exclude — 排除常量值（只保留动态输入）
$param #{exclude: `*?{opcode: const}`}-> as $dynamic_sources;
check $dynamic_sources then "dynamic input found";
alert $dynamic_sources;
```

四种配置可同时出现在一个递归块中组合使用。

## 典型工作流

### 场景 1：验证 semgrep SQL 注入发现

1. **编译目标项目**
   ```
   ssa_compile(target="/path/to/project", language="java", program_name="myapp")
   ```

2. **追踪 SQL 执行点的参数来源（topdef）**
   ```
   ssa_query(program_name="myapp", rule=`
     *.executeQuery(* #-> as $source) as $sink;
     *.execute(* #-> as $source) as $sink;
     check $sink then "SQL execution found";
     alert $source;
   `)
   ```

3. **判断**：若 $source 包含 `getParameter()`、`getHeader()` 等用户输入方法 → **Confirmed**；若 $source 全是常量 → **False Positive**

### 场景 2：鉴权缺失分析

1. **查找所有 HTTP 入口**
   ```
   ssa_query(program_name="myapp", rule=`
     *?{.annotation.*Mapping} as $endpoints;
     check $endpoints then "HTTP endpoint";
     alert $endpoints;
   `)
   ```

2. **查找有鉴权注解的入口**
   ```
   ssa_query(program_name="myapp", rule=`
     *?{.annotation.*Mapping && .annotation.PreAuthorize} as $protected;
     alert $protected;
   `)
   ```

3. **AI 比对**：不在 $protected 中但在 $endpoints 中的方法 → **Auth Missing**

### 场景 3：敏感数据泄露追踪

1. **追踪敏感字段的流向（bottomUse）**
   ```
   ssa_query(program_name="myapp", rule=`
     *.getPassword() as $sensitive --> as $usage;
     *.getSSN() as $sensitive --> as $usage;
     check $usage then "sensitive data flows to";
     alert $usage;
   `)
   ```

2. **判断**：若 $usage 包含 `response.write()`、`log.info()` 等输出方法 → **Confirmed Leak**

## AI 补充分析范围

SyntaxFlow 提供数据流事实后，AI 负责以下非强 sink 点的智能判断：

| 分析类型 | AI 判断内容 | 依赖的 SyntaxFlow 输出 |
|---------|-----------|---------------------|
| 鉴权缺失 | HTTP 入口是否缺少认证/授权检查 | 注解查找 → 入口方法列表 |
| 越权风险 | 用户 ID 参数是否直接用于数据查询 | topdef → 参数来源追踪 |
| 敏感数据泄露 | 敏感字段是否暴露在响应中 | bottomUse → 数据流向追踪 |
| 业务逻辑漏洞 | 条件分支是否存在绕过可能 | topdef + bottomUse 组合 |
| 配置安全 | 安全相关配置项是否设置正确 | 注解查找 → 配置类定位 |

## 输出判定

对每个待确认的发现，给出以下判定之一：
- **Confirmed** — 数据流验证通过，存在从 source 到 sink 的完整污点路径
- **Auth Missing** — AI 鉴权分析发现入口缺少认证/授权
- **False Positive** — 数据流证伪，source 为常量或经过有效清洗
- **Needs Review** — 数据流不确定，需人工审查
