# ASTER

**A**gent-based **S**ecurity **T**esting & **E**valuation **R**untime

基于 ReAct 框架的安全分析 Agent TUI，集成 Semgrep SAST 扫描、SyntaxFlow 数据流追踪、MCP 工具协议和多 LLM Provider 支持。

## 特性

- **多 Agent 协作** — 代码审计、渗透测试、主机防护三大内置 Agent，支持 YAML 自定义扩展
- **ReAct 执行引擎** — 思考-行动-观察循环，支持子 Agent 委派、任务规划、历史压缩
- **Semgrep 多通道扫描** — 内嵌 119 条安全规则（Go/Java/Python/JS/PHP/C），覆盖 OWASP Top 10
- **SyntaxFlow MCP** — 通过 yak SSA 引擎进行 topdef/bottomUse 数据流追踪验证
- **MCP 协议支持** — stdio / SSE / Streamable HTTP，动态连接外部工具服务器
- **7 大 LLM Provider** — OpenAI、Anthropic、DeepSeek、Groq、OpenRouter、Together、Ollama
- **终端 TUI** — 基于 Bubbletea 的交互界面，支持会话管理、主题切换、快捷键操作
- **技能系统** — 24 个内嵌安全分析技能，按需加载注入 Agent 上下文

## 快速开始

### 安装

```bash
go install aster/cmd/aster@latest
```

或从源码构建：

```bash
git clone <repo-url> && cd sastx
go build -o aster ./cmd/aster
```

### 配置

首次运行会自动在 `~/.aster/` 下生成默认配置：

```bash
aster
```

编辑 `~/.aster/config.yaml` 配置 Provider：

```yaml
default_provider: openai

providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    default_model: gpt-4o
```

也可通过命令行参数或环境变量覆盖：

```bash
# 命令行参数
aster --provider deepseek --model deepseek-chat --api-key sk-xxx

# 环境变量
export ASTER_PROVIDER=openai
export ASTER_API_KEY=sk-xxx
export ASTER_MODEL=gpt-4o
```

### 运行

```bash
aster
```

## 架构

```
cmd/aster/              CLI 入口
internal/
  react/                ReAct Agent 框架（执行引擎、工厂、事件系统）
  ai/                   LLM 抽象层（OpenAI 兼容协议）
  tui/                  终端 UI（Bubbletea）
  mcp/                  MCP 服务器管理
  builtin_tools/        内置工具集
  builtin_providers/    Provider 预设配置
  service/              技能服务
  memory/               Agent 执行时间线记忆
skills/                 内嵌技能定义（SKILL.md）
  semgrep-rules/        SAST 规则（Go/Java/Python/JS/PHP/C）
examples/               示例代码
```

## Agent

ASTER 预置三个安全 Agent，以 YAML 文件形式存放在 `~/.aster/agents/`，用户可直接编辑：

| Agent | 说明 | 核心技能 |
|-------|------|----------|
| **code-audit** | 代码安全审计 — Semgrep SAST + SyntaxFlow 数据流追踪 | `sast-scan`, `dataflow-analysis` |
| **pentest** | 渗透测试 — 浏览器自动化 + Web 漏洞检测 | `agent-browser`, SQL 注入/XSS/IDOR 等 12 项检测技能 |
| **host-defense** | 主机防护 — 基线检查 + 入侵检测 + 应急响应 | `baseline-check`, `intrusion-detection`, `malware-detect` 等 |

### 自定义 Agent

在 `~/.aster/agents/` 下创建 `.yaml` 文件，启动时自动加载：

```yaml
name: my-agent
role: 角色描述
background: |
  能力背景，支持多行
instruction: |
  行为约束指令
skill_names:
  - sast-scan
  - dataflow-analysis
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
  - load_skills

# 可选
# model_id: gpt-4o
# policies:
#   max_iterations: 1000
#   allow_bash: true
#   enable_history_compaction: true
# mcp_servers:
#   - name: my-server
#     type: stdio
#     command: my-server-bin
```

文件名按字母排序，第一个为启动时的默认 Agent。

## 技能

24 个内嵌安全分析技能，按 Agent 按需加载：

| 类别 | 技能 |
|------|------|
| **SAST** | `sast-scan` — Semgrep 多通道扫描（本地规则 + 社区注册表 + OWASP） |
| **数据流** | `dataflow-analysis` — SyntaxFlow MCP 数据流追踪（topdef/bottomUse） |
| **Web 安全** | `sql-injection-comprehensive`, `file-upload`, `cors-misconfiguration`, `jwt-weakness`, `idor-detection`, `vertical-privilege-escalation`, `unauthorized-access` |
| **认证安全** | `auth-comprehensive`, `registration-abuse`, `notification-abuse` |
| **隐私安全** | `sensitive-info-exposure`, `secret-detection` |
| **主机安全** | `baseline-check`, `intrusion-detection`, `malware-detect`, `emergency-response`, `log-analysis` |
| **浏览器** | `agent-browser` — Web 安全浏览器自动化测试 |
| **依赖** | `dependency-audit` — 第三方组件安全审计 |

技能定义位于 `skills/<name>/SKILL.md`，采用 YAML frontmatter + Markdown 格式。

## 内置工具

| 工具 | 说明 |
|------|------|
| `list_files` | 文件列表（支持递归、过滤） |
| `read_file` | 读取文件内容（支持行范围） |
| `rg` | Ripgrep 代码搜索（内嵌 rg 二进制） |
| `bash` | Shell 命令执行（三种权限模式：yolo / manual / ai） |
| `list_skills` | 列出可用技能 |
| `load_skills` | 按名称加载技能 |
| `sub_agent` | 委派子 Agent 执行 |
| `human_confirm` | 请求用户确认 |

## MCP 集成

在 `~/.aster/config.yaml` 中配置 MCP 服务器：

```yaml
mcp_servers:
  syntaxflow:
    description: SyntaxFlow 数据流分析引擎
    type: stdio
    command: /path/to/yak
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
    resident: false

  remote-server:
    type: streamable-http
    url: https://mcp.example.com/api
    headers:
      Authorization: "Bearer ${MCP_TOKEN}"
```

支持协议类型：`stdio`、`sse`、`streamable-http`。

也可为特定 Agent 配置专属 MCP 服务器（写在 agent YAML 的 `mcp_servers` 字段中）。

## Provider 支持

| Provider | 默认模型 | 说明 |
|----------|---------|------|
| **OpenAI** | gpt-4o | gpt-4o / gpt-4o-mini / gpt-4.1 / o3-mini |
| **Anthropic** | claude-sonnet-4 | claude-sonnet-4 / claude-opus-4 / claude-haiku-4 |
| **DeepSeek** | deepseek-chat | deepseek-chat / deepseek-reasoner |
| **Groq** | llama-3.3-70b | llama-3.3-70b / llama-3.1-8b / mixtral-8x7b |
| **OpenRouter** | claude-sonnet-4 | 多模型聚合网关 |
| **Together** | Llama-3-70B | Llama-3 系列 |
| **Ollama** | qwen2.5:latest | 本地模型（qwen2.5 / llama3 / deepseek-r1） |

所有 Provider 均通过 OpenAI 兼容协议接入，可在 TUI 中通过 `/provider` 在线切换。

## TUI 操作

### 斜杠命令

| 命令 | 说明 |
|------|------|
| `/agent [name]` | 切换 Agent |
| `/provider [name]` | 切换 Provider |
| `/model [name]` | 切换模型 |
| `/skill [enable\|disable] <name>` | 启用/禁用技能 |
| `/mcp [connect\|disconnect] <name>` | 连接/断开 MCP 服务器 |
| `/mode [yolo\|manual\|ai]` | 切换 Bash 权限模式 |
| `/session [new\|list\|switch\|delete]` | 会话管理 |
| `/new` | 新建会话 |
| `/clear` | 清空聊天记录 |
| `/verbose` | 切换工具调用详情显示 |
| `/theme` | 切换明暗主题 |
| `/help` | 显示帮助 |
| `/exit` | 退出 |

### 快捷键

| 快捷键 | 说明 |
|--------|------|
| `Tab` | 切换焦点（输入框 / 侧边栏 / 聊天） |
| `Esc` | 返回输入框 |
| `Ctrl+N` | 新建会话 |
| `Ctrl+O` | 会话选择器 |
| `Ctrl+K` | Agent 选择器 |
| `Ctrl+M` | 模型选择器 |
| `Ctrl+L` | 清空聊天 |
| `Ctrl+C` | 取消/退出 |

## Semgrep 规则

内嵌 119 条 SAST 规则，覆盖 6 种语言：

| 语言 | 规则类别 |
|------|---------|
| Go | race-condition, path-traversal, unhandled-error, ssrf, crypto, auth |
| Java | crypto, auth, ssrf, misc |
| Python | crypto, auth, ssrf, misc |
| JavaScript | crypto, auth, ssrf, misc |
| PHP | crypto, auth, ssrf, misc |
| C/C++ | crypto, auth, ssrf, misc |

规则在首次使用时自动解压到临时目录，通过 `aster semgrep-rules-path` 获取路径。

## 目录结构

```
~/.aster/
  config.yaml          全局配置（Provider、MCP）
  agents/              Agent YAML 定义
    code-audit.yaml
    pentest.yaml
    host-defense.yaml
    example.yaml
  data.db              会话存储（SQLite）
  sessions/            会话数据目录
```

## License

MIT
