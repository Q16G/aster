# ASTER

**A**gent-based **S**ecurity **T**esting & **E**valuation **R**untime

基于 ReAct 框架的安全分析 Agent TUI。集成 Semgrep SAST 扫描、SyntaxFlow 数据流追踪、MCP 工具协议和多 LLM Provider 支持，覆盖代码审计、渗透测试、主机防护三大安全场景。

## 特性速览

- **三大安全 Agent** — 代码审计 / 渗透测试 / 主机防护，YAML 一键自定义
- **ReAct 执行引擎** — 四阶段循环（Plan → Step → Summary → FinalAnswer），支持子 Agent 委派
- **Semgrep SAST** — 内嵌本地安全规则集（覆盖 Go/Java/Python/JS/PHP/C，持续扩展中）
- **SyntaxFlow 数据流** — 通过 yak SSA 引擎进行 topdef/bottomUse 追踪验证
- **MCP 协议** — stdio / SSE / Streamable HTTP，全局或按 Agent 挂载外部工具
- **7 大 LLM Provider** — OpenAI、Anthropic、DeepSeek、Groq、OpenRouter、Together、Ollama
- **24 个安全技能** — 按需注入 Agent 上下文，运行时动态启用/禁用
- **终端 TUI** — Bubbletea 交互界面，会话管理、主题切换、快捷键操作

---

## 快速开始

### 安装

```bash
# 从源码构建
git clone <repo-url> && cd sastx
go build -o aster ./cmd/aster

# 或 go install
go install aster/cmd/aster@latest
```

### 首次配置

首次运行 `aster`，自动生成 `~/.aster/` 目录及默认配置。你只需设置一个 Provider 的 API Key：

```bash
# 方式一：环境变量（最快）
export OPENAI_API_KEY=sk-your-key
aster

# 方式二：编辑配置文件
vim ~/.aster/config.yaml
```

最小 `config.yaml`：

```yaml
default_provider: openai

providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: sk-your-key
    default_model: gpt-4o
```

### 运行

```bash
aster
```

启动后进入 TUI 交互界面，默认加载 `code-audit` Agent。通过 `/agent` 切换到渗透测试或主机防护模式。

---

## 配置详解

### 配置优先级

从高到低，首个非空值生效：

```
CLI 参数 > ASTER_* 环境变量 > config.yaml > Provider 内置默认 > 硬编码兜底(openai/gpt-4o)
```

### CLI 参数

```bash
aster --provider deepseek --model deepseek-chat --api-key sk-xxx --base-url https://api.deepseek.com/v1
```

| 参数 | 说明 |
|------|------|
| `--provider` | Provider 名称 |
| `--model` | 模型 ID |
| `--base-url` | API 端点 URL |
| `--api-key` | API 密钥 |

### 环境变量

| 变量 | 说明 |
|------|------|
| `ASTER_PROVIDER` | 覆盖默认 Provider |
| `ASTER_MODEL` | 覆盖默认模型 |
| `ASTER_BASE_URL` | 覆盖 API 端点 |
| `ASTER_API_KEY` | 覆盖 API 密钥 |
| `OPENAI_API_KEY` | OpenAI 专用 |
| `ANTHROPIC_API_KEY` | Anthropic 专用 |
| `DEEPSEEK_API_KEY` | DeepSeek 专用 |
| `GROQ_API_KEY` | Groq 专用 |
| `OPENROUTER_API_KEY` | OpenRouter 专用 |
| `TOGETHER_API_KEY` | Together 专用 |

### config.yaml 完整结构

```yaml
default_provider: openai

providers:
  <name>:
    base_url: <url>              # API 端点
    api_key: <key|${ENV_VAR}>    # 密钥，支持环境变量引用
    default_model: <model_id>    # 该 Provider 的默认模型

mcp_servers:
  <name>:
    description: <string>        # 描述（可选）
    type: stdio|sse|streamable-http
    command: <path>              # stdio 模式：可执行文件路径
    args: [<arg1>, ...]          # stdio 模式：命令参数
    url: <url>                   # HTTP 模式：服务器 URL
    headers:                     # HTTP 模式：请求头（可选）
      Authorization: "Bearer ${TOKEN}"
    env:                         # 额外环境变量（可选）
      KEY: value
    resident: false              # 是否常驻连接（默认 false）
```

### 内置 Provider

| Provider | Base URL | 环境变量 | 默认模型 |
|----------|----------|----------|----------|
| **openai** | `https://api.openai.com/v1` | `OPENAI_API_KEY` | gpt-4o |
| **anthropic** | `https://api.anthropic.com/v1` | `ANTHROPIC_API_KEY` | claude-sonnet-4 |
| **deepseek** | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` | deepseek-chat |
| **groq** | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` | llama-3.3-70b-versatile |
| **openrouter** | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | anthropic/claude-sonnet-4 |
| **together** | `https://api.together.xyz/v1` | `TOGETHER_API_KEY` | meta-llama/Llama-3-70b-chat-hf |
| **ollama** | `http://localhost:11434/v1` | — | qwen2.5:latest |

所有 Provider 通过 OpenAI 兼容协议接入。运行时通过 `/provider` 命令或 `Ctrl+K` 在线切换。

---

## 权限模式

控制 Agent 执行 Bash 命令时的授权策略。通过 `/mode` 命令切换。

### 三种模式

| 模式 | 行为 | 适用场景 |
|------|------|----------|
| **YOLO** | 所有命令自动执行，无需确认 | 可信隔离环境、CTF |
| **MANUAL** | 每条命令都需人工确认 | 生产环境、敏感操作（默认） |
| **AI** | 基于风险评估自动决策 | 日常使用推荐 |

### AI 模式决策流程

```
命令提交
  │
  ├─ 命中 allowlist？ ─── 是 ──→ 自动执行
  │
  └─ 否 ──→ 风险评估
              │
              ├─ risk: low ──→ 自动执行
              └─ risk: high/uncertain ──→ 请求人工确认
```

---

## Agent 系统

### 内置 Agent

| Agent | 定位 | 核心技能 |
|-------|------|----------|
| **code-audit** | 代码安全审计 — Semgrep SAST + SyntaxFlow 数据流 | `sast-scan`, `dataflow-analysis` |
| **pentest** | 渗透测试 — 浏览器自动化 + Web 漏洞检测 | `agent-browser`, SQL 注入/XSS/IDOR 等 12 项 |
| **host-defense** | 主机防护 — 基线检查 + 入侵检测 + 应急响应 | `baseline-check`, `intrusion-detection`, `malware-detect` |

Agent 定义文件位于 `~/.aster/agents/`，启动时按字母序加载，第一个为默认 Agent。

### Agent YAML Schema

```yaml
name: my-agent                    # Agent 标识名
role: |                           # 角色定义
  安全代码审计专家
background: |                     # 能力背景
  精通 OWASP Top 10，熟悉多种语言的安全编码规范
instruction: |                    # 行为指令
  1. 先扫描再分析
  2. 每个发现必须有数据流证据

# 模型覆盖（可选，不设则用全局默认）
model_id: gpt-4o

# 可用工具
tool_names:
  - list_files
  - read_file
  - rg
  - bash
  - list_skills
  - load_skills

# 可用技能（Agent 上下文中可加载的技能列表）
skill_names:
  - sast-scan
  - dataflow-analysis

# Agent 专属 MCP 服务器（仅该 Agent 可用）
mcp_servers:
  - name: syntaxflow
    type: stdio
    command: yak
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]

# 执行策略
policies:
  max_iterations: 1000            # 最大迭代次数
  allow_bash: true                # 是否启用 bash 工具
  enable_history_compaction: true # Token 超限时自动压缩历史
  result_source: latest_step_result  # 结果提取策略
  publish_contract: sast-findings    # 输出合约名

# 输出合约（结构化输出验证）
output_contracts:
  sast-findings:
    schema: |
      {"type":"object","required":["total_findings","findings"],"properties":{...}}
    example: |
      {"total_findings":3,"findings":[...]}
    summary_policy: |
      保留所有发现的 id、标题和严重等级
```

### 自定义 Agent 示例

创建 `~/.aster/agents/api-audit.yaml`：

```yaml
name: api-audit
role: API 接口安全审计专家
background: |
  专注于 REST/GraphQL API 的认证、授权、输入校验和速率限制审计。
  熟悉 JWT、OAuth2、RBAC 等安全机制。
instruction: |
  1. 先用 list_files 了解项目结构
  2. 用 rg 搜索路由定义和中间件配置
  3. 加载 sast-scan 技能进行静态分析
  4. 重点关注：未鉴权端点、SQL 注入、越权访问
  5. 输出结构化审计报告

skill_names:
  - sast-scan
  - sql-injection-comprehensive
  - auth-comprehensive
  - idor-detection

tool_names:
  - list_files
  - read_file
  - rg
  - bash
  - list_skills
  - load_skills

policies:
  max_iterations: 500
  allow_bash: true
  enable_history_compaction: true
```

保存后重启 aster，即可通过 `/agent api-audit` 切换。

### Agent 切换

```
/agent                  # 打开 Agent 选择器
/agent code-audit       # 直接切换到 code-audit
Ctrl+K                  # 快捷键打开选择器
```

---

## 技能系统

24 个内嵌安全分析技能，按 Agent 的 `skill_names` 字段控制可用范围：

| 类别 | 技能 |
|------|------|
| **SAST** | `sast-scan` — Semgrep 多通道扫描（本地规则集，覆盖 6 语言） |
| **数据流** | `dataflow-analysis` — SyntaxFlow MCP 追踪（topdef/bottomUse） |
| **Web 安全** | `sql-injection-comprehensive`, `file-upload`, `cors-misconfiguration`, `jwt-weakness`, `idor-detection`, `vertical-privilege-escalation`, `unauthorized-access` |
| **认证安全** | `auth-comprehensive`, `registration-abuse`, `notification-abuse` |
| **隐私安全** | `sensitive-info-exposure`, `secret-detection` |
| **主机安全** | `baseline-check`, `intrusion-detection`, `malware-detect`, `emergency-response`, `log-analysis` |
| **浏览器** | `agent-browser` — Web 安全浏览器自动化 |
| **依赖** | `dependency-audit` — 第三方组件审计 |

### 加载机制

```
Agent YAML: skill_names → SkillsCatalog 构建可用列表
                              ↓
运行时: Agent 调用 load_skills 工具 → 技能指令注入当前 prompt context
                              ↓
两种执行模式:
  - inline: 技能指令直接注入当前 Agent 上下文
  - fork:   启动子 Agent 独立执行技能任务
```

### 第二阶段框架层规则补充

当前自建规则已经补到一批高价值的框架/API 入口，重点覆盖“通用漏洞类别已存在，但主流框架入口仍缺席”的空位：

- Go：`http.Redirect`、`gin.Context.Redirect`、`Header().Set/Add(...)`、`sqlx`、`reform`、`pop`、`ent`、Beego / gin 上传落盘路径
- Java：Spring MVC `redirect:`、`RedirectView`、`ModelAndView("redirect:...")`、`GroovyShell.evaluate`、`ScriptEngine.eval`、`Part.write(...)`、下载响应头文件名注入、Controller 视图名可控 SSTI
- Python：Flask / Django / FastAPI `redirect(...)`、`render_template_string(...)`、`jinja2.Template(...).render(...)`、Django Storage / FastAPI UploadFile 保存路径
- JavaScript / TypeScript：Koa / Fastify / Next.js `redirect(...)`、Vue `v-html` 存储型 XSS 入口、`undici` SSRF sink

当前 README 不再手写维护规则总数，具体覆盖范围以 `skills/semgrep-rules/` 目录和对应最小样例为准。

### 运行时管理

```
/skill                        # 查看所有技能状态
/skill enable sast-scan       # 启用技能
/skill disable sast-scan      # 禁用技能
```

---

## MCP 集成

### 全局 MCP vs Agent 专属 MCP

| 类型 | 定义位置 | 可见范围 | 适用场景 |
|------|----------|----------|----------|
| **全局** | `~/.aster/config.yaml` 的 `mcp_servers` | 所有 Agent | 通用工具（如 SyntaxFlow） |
| **Agent 专属** | Agent YAML 的 `mcp_servers` 字段 | 仅该 Agent | 特定场景工具 |

### 三种传输协议

**stdio — 本地子进程**

```yaml
mcp_servers:
  syntaxflow:
    type: stdio
    command: /usr/local/bin/yak
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
    resident: false
```

**sse — Server-Sent Events**

```yaml
mcp_servers:
  remote-tool:
    type: sse
    url: https://mcp.example.com/sse
    headers:
      Authorization: "Bearer ${MCP_TOKEN}"
```

**streamable-http — 流式 HTTP**

```yaml
mcp_servers:
  cloud-tool:
    type: streamable-http
    url: https://mcp.example.com/api
    headers:
      Authorization: "Bearer ${MCP_TOKEN}"
```

### 将 MCP 挂载到指定 Agent

在 Agent YAML 中添加 `mcp_servers` 字段，该 MCP 仅对此 Agent 可用：

```yaml
# ~/.aster/agents/code-audit.yaml
name: code-audit
role: 代码安全审计专家
skill_names:
  - sast-scan
  - dataflow-analysis
tool_names:
  - list_files
  - read_file
  - rg
  - bash

# 专属 MCP：仅 code-audit 可使用
mcp_servers:
  - name: syntaxflow
    description: SyntaxFlow 数据流分析引擎
    type: stdio
    command: yak
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
```

全局 MCP 对所有 Agent 可见，Agent 专属 MCP 仅在切换到该 Agent 时加载。两者的工具都会注册到 Agent 的可用工具列表中。

### 运行时管理

```
/mcp                          # 查看所有 MCP 服务器状态
/mcp connect syntaxflow       # 连接 MCP 服务器
/mcp disconnect syntaxflow    # 断开连接
```

---

## ReAct 执行引擎

### 四阶段架构

```
用户输入
  │
  ▼
┌─────────────────────────────────────────────────┐
│  Plan Phase — 任务分解                            │
│  将用户输入拆解为有序步骤（简单任务自动跳过）          │
└────────────────────────┬────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────┐
│  Step Phase — Think-Act-Observe 循环              │
│                                                   │
│  Think: LLM 分析当前状态，决定下一步行动            │
│    ↓                                              │
│  Act: 调用工具（bash/read_file/rg/MCP/sub_agent） │
│    ↓                                              │
│  Observe: 获取工具执行结果，判断是否完成            │
│    ↓                                              │
│  未完成 → 回到 Think（直到 max_iterations）        │
└────────────────────────┬────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────┐
│  Summary Phase — 步骤结果摘要                      │
└────────────────────────┬────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────┐
│  FinalAnswer Phase — 输出结构化结果                 │
│  （按 output_contracts 验证格式）                  │
└─────────────────────────────────────────────────┘
```

### 执行策略参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `max_iterations` | 最大迭代次数 | 1000 |
| `allow_bash` | 是否启用 bash 工具 | true |
| `enable_history_compaction` | Token 超限时压缩历史 | true |
| `result_source` | 结果提取策略：`latest_step_result` / `final_answer` / `step_summary` | latest_step_result |
| `publish_contract` | 输出合约名（要求结构化输出） | — |

### 子 Agent 委派

Agent 可通过 `sub_agent` 工具将子任务委派给其他 Agent：

- 子 Agent 继承父级的工具、MCP 和 bash 权限
- 独立的工作空间命名空间（`agents/skill-{name}-{callId}`）
- 递归深度限制防止无限嵌套

---

## 外部依赖

ASTER 核心功能开箱即用，以下外部工具可增强特定场景能力：

### 渗透测试 — agent-browser

`pentest` Agent 依赖 [agent-browser](https://github.com/vercel-labs/agent-browser) 进行浏览器自动化、HAR 流量抓取和交互式 Web 安全测试。

```bash
# npm（推荐）
npm install -g agent-browser && agent-browser install

# Homebrew (macOS)
brew install agent-browser && agent-browser install

# Cargo (Rust)
cargo install agent-browser && agent-browser install
```

**系统要求：** Chrome 浏览器（首次 `agent-browser install` 自动下载）。

> 未安装时：pentest Agent 的浏览器自动化技能不可用，但 SQL 注入、IDOR、CORS 等检测技能仍可工作。

### 代码分析 — yak 引擎（SyntaxFlow SSA）

`dataflow-analysis` 技能通过 MCP 调用 [yak 引擎](https://github.com/yaklang/yaklang) 的 SSA 编译与 SyntaxFlow 查询，实现数据流追踪。

```bash
# macOS / Linux
bash <(curl -sS -L http://oss.yaklang.io/install-latest-yak.sh)

# 验证
yak version
```

**支持分析语言：** Java、PHP、JavaScript、Go、Python、C

安装后配置 `~/.aster/config.yaml`：

```yaml
mcp_servers:
  syntaxflow:
    type: stdio
    command: /usr/local/bin/yak  # 替换为 `which yak` 输出
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
```

> 未安装时：`sast-scan`（Semgrep）仍可独立工作，但 `dataflow-analysis` 不可用。

---

## TUI 操作

### 斜杠命令

| 命令 | 说明 |
|------|------|
| `/agent [name]` | 切换 Agent |
| `/provider [name]` | 切换 Provider |
| `/model [name]` | 切换模型 |
| `/skill [enable\|disable] <name>` | 启用/禁用技能 |
| `/mcp [connect\|disconnect] <name>` | 连接/断开 MCP |
| `/mode [yolo\|manual\|ai]` | 切换权限模式 |
| `/session [new\|list\|switch\|delete]` | 会话管理 |
| `/new` | 新建会话 |
| `/clear` | 清空聊天 |
| `/verbose` | 切换工具详情显示 |
| `/theme` | 切换主题 |
| `/help` | 帮助 |
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

---

## 目录结构

```
~/.aster/
├── config.yaml              # 全局配置（Provider + MCP）
├── agents/                  # Agent YAML 定义
│   ├── code-audit.yaml
│   ├── pentest.yaml
│   ├── host-defense.yaml
│   └── example.yaml
├── data.db                  # 会话存储（SQLite）
└── sessions/                # 会话数据
```

```
源码:
cmd/aster/                   # CLI 入口
internal/
  react/                     # ReAct Agent 框架（执行引擎、调度器、工厂）
  ai/                        # LLM 抽象层（OpenAI 兼容协议）
  tui/                       # 终端 UI（Bubbletea）
  mcp/                       # MCP 服务器管理
  builtin_tools/             # 内置工具集
  builtin_providers/         # Provider 预设
  service/                   # 技能服务
  memory/                    # Agent 时间线记忆
skills/                      # 内嵌技能定义（SKILL.md）
  semgrep-rules/             # SAST 规则（6 语言，本地自建 + 社区精选）
```

---

## 致谢

ASTER 的构建离不开以下开源项目：

| 项目 | 用途 | 许可证 |
|------|------|--------|
| [Yaklang](https://github.com/yaklang/yaklang) | SyntaxFlow SSA 数据流分析引擎 | AGPL-3.0 |
| [agent-browser](https://github.com/vercel-labs/agent-browser) | AI 浏览器自动化 CLI | Apache-2.0 |
| [Semgrep](https://github.com/semgrep/semgrep) | SAST 静态分析扫描引擎 | LGPL-2.1 |
| [Bubbletea](https://github.com/charmbracelet/bubbletea) / [Lipgloss](https://github.com/charmbracelet/lipgloss) | 终端 TUI 框架与样式库 | MIT |
| [mcp-go](https://github.com/mark3labs/mcp-go) | Go MCP 协议实现 | MIT |

---

## License

MIT
