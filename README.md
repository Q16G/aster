<h1 align="center">ASTER</h1>

<p align="center">
  <strong>A</strong>gent-based <strong>S</strong>ecurity <strong>T</strong>esting & <strong>E</strong>valuation <strong>R</strong>untime
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/License-MIT-green" alt="MIT License">
  <img src="https://img.shields.io/badge/Platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey" alt="Platform">
</p>

<p align="center">
基于 ReAct 框架的安全分析 Agent，在终端中完成代码审计、渗透测试、主机防护。<br>
内置 Semgrep 规则集 + SyntaxFlow 数据流追踪 + MCP 工具协议 + 多 LLM Provider 支持。
</p>

<!-- TODO: 在这里放一张终端运行截图或 GIF -->
<!-- <p align="center"><img src="docs/assets/demo.gif" width="720"></p> -->

<p align="center">
  <details>
    <summary>扫码加入 aster 交流群</summary>
    <br>
    <img src="docs/assets/wechat-group-qr.png" width="320" alt="aster 交流群微信二维码">
    <br>
    <sub>二维码失效请提 issue 获取最新入群方式</sub>
  </details>
</p>

---

### 亮点

- **三大安全 Agent** — 代码审计 / 渗透测试 / 主机防护，YAML 声明式定义，支持自定义扩展
- **51+ 安全技能** — 按需注入 Agent 上下文，运行时动态启用/禁用，覆盖 SAST、数据流、Web 安全、认证、主机防护
- **7 大 LLM Provider** — OpenAI、Anthropic、DeepSeek、Groq、OpenRouter、Together、Ollama（本地离线）
- **ReAct 推理引擎** — Plan → Think-Act-Observe → Summary → FinalAnswer 四阶段循环
- **Semgrep SAST** — 内嵌本地规则集，覆盖 Go / Java / Python / JS / PHP / C，零在线依赖
- **SyntaxFlow 数据流** — 通过 yak SSA 引擎的 topdef/bottomUse 追踪验证漏洞可达性
- **MCP 协议扩展** — stdio / SSE / Streamable HTTP 三种传输，全局或按 Agent 挂载工具
- **终端 TUI** — Bubbletea 交互界面，会话管理、主题切换、快捷键操作

---

## 目录

- [快速开始](#快速开始)
- [安装](#安装)
- [模型配置](#模型配置)
- [MCP 集成](#mcp-集成)
- [Skills 的使用](#skills-的使用)
- [自建 Agent 场景](#自建-agent-场景)
- [内置场景介绍与快速开始](#内置场景介绍与快速开始)
- [致谢](#致谢)
- [项目热度](#项目热度)
- [重要安全声明](#重要安全声明)
- [License](#license)

---

## 快速开始

30 秒从零到运行：

```bash
# 1. 下载二进制（以 macOS Apple Silicon 为例，其他平台见下方表格）
#    前往 https://github.com/Q16G/aster/releases 下载最新版本
curl -Lo aster.tar.gz https://github.com/Q16G/aster/releases/download/v0.1.0-alpha-8/aster_0.1.0-alpha-8_darwin_arm64.tar.gz
tar xzf aster.tar.gz && chmod +x aster
sudo mv aster /usr/local/bin/

# 2. 配置 API Key（任选一个 Provider）
export OPENAI_API_KEY=sk-your-key

# 3. 启动
aster
```

首次运行自动生成 `~/.aster/` 目录（含 `config.yaml` 和 Agent 配置）。默认使用 `code-audit` Agent，输入自然语言即可开始安全分析。

---

## 安装

### 从 Releases 下载（推荐）

前往 [GitHub Releases](https://github.com/Q16G/aster/releases) 下载对应平台的预编译二进制，解压后放入 `PATH` 即可使用。无需 Go 环境，无需编译。

| 平台 | 资产名 |
|------|--------|
| macOS (Apple Silicon) | `aster_<版本>_darwin_arm64.tar.gz` |
| macOS (Intel) | `aster_<版本>_darwin_amd64.tar.gz` |
| Linux (x86_64) | `aster_<版本>_linux_amd64.tar.gz` |
| Linux (ARM64) | `aster_<版本>_linux_arm64.tar.gz` |
| Windows (x86_64) | `aster_<版本>_windows_amd64.zip` |

> 下载后 Linux/macOS 执行 `tar xzf <文件名> && chmod +x aster`，Windows 解压 zip 即可。

### 自动更新

已安装的 ASTER 支持自更新，检测 GitHub Releases 最新版本并自动替换：

```bash
aster update
```

### go install

```bash
go install github.com/Q16G/aster/cmd/aster@latest
```

> 要求 Go 1.25+

### 从源码构建

```bash
git clone https://github.com/Q16G/aster.git && cd aster
make build    # 输出 ./aster 二进制
```

---

## 模型配置

### 零配置——环境变量即启动

只需设置任意一个 Provider 的 API Key 环境变量，ASTER 会自动探测可用 Provider：

```bash
export OPENAI_API_KEY=sk-your-key       # OpenAI
# 或
export ANTHROPIC_API_KEY=sk-ant-xxx     # Anthropic
# 或
export DEEPSEEK_API_KEY=sk-xxx          # DeepSeek
```

支持的环境变量：

| 变量 | Provider |
|------|----------|
| `OPENAI_API_KEY` | OpenAI |
| `ANTHROPIC_API_KEY` | Anthropic |
| `DEEPSEEK_API_KEY` | DeepSeek |
| `GROQ_API_KEY` | Groq |
| `OPENROUTER_API_KEY` | OpenRouter |
| `TOGETHER_API_KEY` | Together |

> Ollama 为本地模型，无需 API Key，详见 [本地模型（Ollama）](#本地模型ollama)。

### 最小 config.yaml

首次运行 `aster` 自动生成 `~/.aster/config.yaml`。如需手动指定，编辑该文件：

```yaml
# ~/.aster/config.yaml
default_provider: openai

providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: sk-your-key
    default_model: gpt-4o
```

> `api_key` 支持 `${ENV_VAR}` 语法引用环境变量，避免明文写入配置文件。

### CLI 参数与环境变量覆盖

```bash
aster --provider deepseek --model deepseek-chat --api-key sk-xxx --base-url https://api.deepseek.com/v1
```

| 参数 | 说明 |
|------|------|
| `--provider` | Provider 名称 |
| `--model` | 模型 ID |
| `--base-url` | API 端点 URL |
| `--api-key` | API 密钥 |

也可通过 `ASTER_*` 环境变量覆盖：

| 变量 | 说明 |
|------|------|
| `ASTER_PROVIDER` | 覆盖默认 Provider |
| `ASTER_MODEL` | 覆盖默认模型 |
| `ASTER_BASE_URL` | 覆盖 API 端点 |
| `ASTER_API_KEY` | 覆盖 API 密钥 |

配置优先级（从高到低）：

```
CLI 参数 > ASTER_* 环境变量 > ~/.aster/config.yaml > Provider 内置默认 > 硬编码兜底
```

运行时也可通过 `/provider`、`/model` 命令在线切换。

### 内置 Provider


完整的 `~/.aster/config.yaml` 写法（7 个内置 Provider 全量配置，按需保留所需条目即可）：

```yaml
default_provider: openai

providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    default_model: gpt-4o

  anthropic:
    base_url: https://api.anthropic.com/v1
    api_key: ${ANTHROPIC_API_KEY}
    default_model: claude-sonnet-4

  deepseek:
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}
    default_model: deepseek-chat

  groq:
    base_url: https://api.groq.com/openai/v1
    api_key: ${GROQ_API_KEY}
    default_model: llama-3.3-70b-versatile

  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key: ${OPENROUTER_API_KEY}
    default_model: anthropic/claude-sonnet-4

  together:
    base_url: https://api.together.xyz/v1
    api_key: ${TOGETHER_API_KEY}
    default_model: meta-llama/Llama-3-70b-chat-hf

  ollama:
    base_url: http://localhost:11434/v1
    default_model: qwen2.5:latest
```

> 运行时用 `/provider <name>` 在已配置的 Provider 间在线切换。

### 更多内置 Provider
ASTER 内置 [models.dev](https://models.dev) 注册表，开箱即可识别 **128 个 Provider** 的 Base URL 与 API Key 环境变量。除上面 7 个默认探测项外，以下为常用扩充（用法与上面完全一致，在 `providers` 下按相同结构添加即可）：

| Provider | Base URL | API Key 环境变量 | 示例模型 |
|----------|----------|------------------|----------|
| google | `https://generativelanguage.googleapis.com/v1beta/openai/` | `GEMINI_API_KEY` | gemini-2.5-pro |
| xai | `https://api.x.ai/v1` | `XAI_API_KEY` | grok-3 |
| mistral | `https://api.mistral.ai/v1` | `MISTRAL_API_KEY` | mistral-large-latest |
| moonshotai (Kimi) | `https://api.moonshot.ai/v1` | `MOONSHOT_API_KEY` | kimi-k2-0905-preview |
| zhipuai (智谱 GLM) | `https://open.bigmodel.cn/api/paas/v4` | `ZHIPU_API_KEY` | glm-4.6 |
| alibaba (通义千问) | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `DASHSCOPE_API_KEY` | qwen-max |
| siliconflow (硅基流动) | `https://api.siliconflow.com/v1` | `SILICONFLOW_API_KEY` | deepseek-ai/DeepSeek-V3 |
| modelscope (魔搭) | `https://api-inference.modelscope.cn/v1` | `MODELSCOPE_API_KEY` | Qwen/Qwen2.5-72B-Instruct |
| fireworks-ai | `https://api.fireworks.ai/inference/v1` | `FIREWORKS_API_KEY` | accounts/fireworks/models/kimi-k2-instruct |
| nvidia | `https://integrate.api.nvidia.com/v1` | `NVIDIA_API_KEY` | deepseek-ai/deepseek-r1 |
| huggingface | `https://router.huggingface.co/v1` | `HF_TOKEN` | deepseek-ai/DeepSeek-R1 |
| novita-ai | `https://api.novita.ai/openai` | `NOVITA_API_KEY` | deepseek/deepseek-v3 |
| perplexity | `https://api.perplexity.ai/v1` | `PERPLEXITY_API_KEY` | sonar-pro |
| ollama-cloud | `https://ollama.com/v1` | `OLLAMA_API_KEY` | gpt-oss:120b |
| minimax | `https://api.minimax.io/anthropic/v1` | `MINIMAX_API_KEY` | MiniMax-M2（Anthropic 协议） |

```yaml
providers:
  zhipuai:
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${ZHIPU_API_KEY}
    default_model: glm-4.6
```

> 「示例模型」仅为参考，请以各 Provider 当前模型目录为准，运行时 `/model <id>` 可随时切换。需要表中未列出的厂商，按相同结构填入对应 `base_url` 与 `api_key` 即可——任意 models.dev 收录的 Provider 均受支持。

### 第三方配置（OpenAI 兼容接入）

内置 Provider 之外的任意兼容 OpenAI `/v1/chat/completions` 的服务，**只能通过 `~/.aster/config.yaml` 配置**（自定义 Provider 名称无法被环境变量自动探测）。在 `providers` 下新增一个条目，填写 `base_url`、`api_key`、`default_model`：

```yaml
default_provider: my-llm

providers:
  my-llm:
    base_url: https://llm.internal.example.com/v1
    api_key: ${MY_LLM_API_KEY}
    default_model: my-model-32b
```

> Provider 名称（`providers` 下的 key）任意取，用 `/provider my-llm` 选择。

### Provider 变量添加（env / headers）

在单个 Provider 下，可附加局部环境变量与自定义请求头：

```yaml
providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    default_model: gpt-4o
    headers:                                   # 附加到每次请求的自定义 HTTP 头
      X-Org-Id: org-xxxx
    env:                                       # 该 Provider 局部变量（含代理）
      HTTPS_PROXY: socks5://127.0.0.1:7890
```

- `headers` — 每次请求附带的自定义请求头（如组织 ID、网关鉴权头）。
- `env` — **仅作为该 Provider 的局部变量源**，不修改全局环境；支持 `${VAR}` 引用，并据此自动应用代理（`HTTPS_PROXY` / `HTTP_PROXY`）。

> 需要所有 Provider 与 MCP 共享的变量，放在顶层 `env:` 下；只想影响单个 Provider，放在 `providers.<name>.env` 下。

### 模型变体（variants）—— 开启 thinking / 推理模式

同一基础模型可定义多个**变体**，每个变体是一组预设的请求选项；选定变体后，这组选项会**原样合并进该次请求体的额外字段**（OpenAI 兼容协议下即追加到请求 JSON 顶层；Anthropic 协议下字符串选项转为请求头）。最典型的用途就是**开启模型的 thinking（深度推理）模式**——像 DeepSeek 那样，在变体里写一个 `thinking: true` 字段即可：

```yaml
providers:
  deepseek:
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}
    default_model: deepseek-chat
    variants:
      deep:               # 外层 key = 变体名（自己取，用于选择器，如 deep / fast / high）
        thinking: true    # 内层 = 透传进请求体的字段，原样并入 -> {"model": "...", "thinking": true, ...}
```

注意这里是**两层**，含义不同：

- **外层 key（`deep`）= 变体名**，由你自己命名，仅用于 `模型ID:变体名` 选择器；起什么名字都行（`deep`、`fast`、`high` 等）。
- **内层 map（`thinking: true`）= 真正透传进请求体的字段**，键名以各 Provider 的 API 文档为准，ASTER 不做转换、原样附加（如 DeepSeek 用 `thinking: true`，部分服务用 `reasoning_effort: high`、`enable_thinking: true` 等）。

通过 `模型ID:变体名` 语法选择变体——冒号后的变体名会在 `variants` 中查表，命中则把对应内层选项并入该次请求：

```bash
aster --model deepseek-chat:deep      # 启用 deep 变体，请求体携带 thinking: true
```

运行时切换变体最方便的是 `/variant` 命令：

```text
/variant            # 弹出当前模型的变体选择器（含「无变体」一项），方向键选择
/variant deep       # 直接切到 deep 变体
/variant none       # 清除变体，回到基础模型（none / off / clear / base 等价）
```

也可以用 `/model deepseek-chat:deep` 直接带变体切换，效果相同。

> **提示**：`Ctrl+M` 主模型选择器只列出 [models.dev](https://models.dev) 为模型**预置**的变体，你在 `config.yaml` 里**自定义**的变体不会出现在那里——自定义变体请用 `/variant`（它会把 config 变体与 models.dev 预置变体合并列出）。
>
> 同一模型下可以并列定义多个变体（例如 `deep` 开 thinking、`fast` 关 thinking、再来个不同推理档），运行时用 `/variant` 一键切换、无需改配置重启。

### 多模态添加（supports_vision / supports_audio）

ASTER 默认依据 [models.dev](https://models.dev) 元数据自动推断模型的视觉 / 音频能力。当使用第三方模型、元数据缺失或推断不准时，可在 Provider 下手动声明覆盖：

```yaml
providers:
  my-llm:
    base_url: https://llm.internal.example.com/v1
    api_key: ${MY_LLM_API_KEY}
    default_model: my-vlm-32b
    supports_vision: true      # 强制声明支持图像输入
    supports_audio: false      # 强制声明音频能力
```

> 仅在自动推断结果不符合实际时才需要手动设置；未设置时以元数据推断为准。

### 本地模型（Ollama）

无需 API Key，完全离线运行，可搭配任意 Agent：

```bash
ollama serve          # 启动 Ollama
ollama pull qwen2.5   # 拉取模型
aster --provider ollama
```

或在 `~/.aster/config.yaml` 中配置：

```yaml
default_provider: ollama

providers:
  ollama:
    base_url: http://localhost:11434/v1
    default_model: qwen2.5:latest
```

> **注意**：本地模型推理能力通常弱于云端大模型，复杂审计场景（多步推理、长上下文）效果可能下降。

---

## MCP 集成

通过 [Model Context Protocol](https://modelcontextprotocol.io) 扩展 Agent 的工具集。

### 快速示例

在 `~/.aster/config.yaml` 中添加：

```yaml
mcp_servers:
  my-tool:
    type: stdio
    command: /path/to/my-mcp-server
    args: ["--mode", "production"]
```

运行时管理：

```
/mcp                          # 查看 MCP 服务器状态
/mcp connect my-tool          # 连接
/mcp disconnect my-tool       # 断开
```

### 全局 vs Agent 专属

| 类型 | 定义位置 | 可见范围 |
|------|----------|----------|
| 全局 | `config.yaml` 的 `mcp_servers` | 所有 Agent |
| Agent 专属 | Agent YAML 的 `mcp_servers` | 仅该 Agent |

### 传输协议

**stdio — 本地子进程**

```yaml
mcp_servers:
  syntaxflow:
    type: stdio
    command: /usr/local/bin/yak
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
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
```

---

## Skills 的使用

51+ 个内嵌安全分析技能，按 Agent 的 `skill_names` 配置控制可用范围，运行时按需注入 Agent 上下文：

| 类别 | 技能 |
|------|------|
| **SAST** | `sast-scan` — Semgrep 多语言扫描（本地规则集） |
| **数据流** | `dataflow-analysis` — SyntaxFlow topdef/bottomUse 追踪 |
| **Web 安全** | `sql-injection-comprehensive`, `file-upload`, `cors-misconfiguration`, `jwt-weakness`, `idor-detection`, `vertical-privilege-escalation`, `unauthorized-access` |
| **认证** | `auth-comprehensive`, `registration-abuse`, `notification-abuse` |
| **隐私** | `sensitive-info-exposure`, `secret-detection` |
| **主机** | `baseline-check`, `intrusion-detection`, `malware-detect`, `emergency-response`, `log-analysis` |
| **浏览器** | `agent-browser` — Web 安全浏览器自动化 |
| **依赖** | `dependency-audit` — 第三方组件审计 |

### 加载机制

```
Agent YAML skill_names → 构建可用列表
                          ↓
运行时: Agent 调用 load_skills → 技能指令注入 prompt
                          ↓
执行模式:
  - inline: 注入当前 Agent 上下文
  - fork:   启动子 Agent 独立执行
```

### 运行时管理

```
/skill                        # 查看所有技能状态
/skill enable sast-scan       # 启用
/skill disable sast-scan      # 禁用
```

> `preload_skills` 中的技能为强制启用，不可通过 `/skill disable` 禁用。

---

## 自建 Agent 场景

除三大内置 Agent 外，可通过 YAML 声明任意专属场景的 Agent。

### 创建自定义 Agent

创建 `~/.aster/agents/api-audit.yaml`：

```yaml
name: api-audit
role: API 接口安全审计专家
background: |
  专注于 REST/GraphQL API 的认证、授权、输入校验和速率限制审计。
instruction: |
  1. 先了解项目结构
  2. 搜索路由定义和中间件
  3. 加载 sast-scan 进行静态分析
  4. 重点关注：未鉴权端点、SQL 注入、越权访问

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

保存后重启，通过 `/agent api-audit` 切换使用，或用快捷键 `Ctrl+K` 打开 Agent 选择器。

### Agent YAML 字段说明

| 字段 | 说明 |
|------|------|
| `name` | Agent 标识名 |
| `role` | 角色定义 |
| `background` | 能力背景描述 |
| `instruction` | 行为指令 |
| `model_id` | 模型覆盖（可选） |
| `tool_names` | 可用工具列表 |
| `skill_names` | 可加载的技能列表 |
| `preload_skills` | 强制预加载技能（不可禁用） |
| `mcp_servers` | Agent 专属 MCP 服务器 |
| `policies` | 执行策略参数 |

### 执行策略参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `max_iterations` | 最大迭代次数 | 1000 |
| `allow_bash` | 是否启用 bash 工具 | true |
| `enable_history_compaction` | Token 超限时压缩历史 | true |
| `result_source` | 结果提取策略 | latest_step_result |

### 重置内置 Agent

```bash
aster agent reset           # 仅补充缺失的内置 Agent
aster agent reset --force   # 强制覆盖所有内置 Agent（自定义 Agent 不受影响）
```

---

## 内置场景介绍与快速开始

| 目标 | Agent | 启动命令 |
|------|-------|----------|
| 审计源代码中的安全漏洞 | `code-audit`（默认） | `aster` |
| 对运行中的 Web 应用渗透测试 | `pentest` | 启动后 `/agent pentest` |
| 主机安全基线检查与应急响应 | `host-defense` | 启动后 `/agent host-defense` |

### 场景 1: 代码审计（默认 Agent）

默认启动即为 `code-audit` Agent。Semgrep 规则已内嵌于二进制，首次运行自动提取到 `~/.aster/rules/`。

```bash
# 必需：安装 semgrep（SAST 扫描引擎）
pip install semgrep

# 推荐：安装 yak 引擎（数据流追踪，验证漏洞可达性）
# 安装后无需额外配置，默认 config.yaml 已包含 MCP 配置
bash <(curl -sS -L http://oss.yaklang.io/install-latest-yak.sh)
```

```
aster
> 对当前项目做一次全量安全审计
```

| 工具 | 状态 | 不安装时的影响 |
|------|------|---------------|
| `semgrep` | **必需** | `sast-scan` 技能不可用，退化为纯 AI 代码审查 |
| `yak` 引擎 | 推荐 | `dataflow-analysis` 退化为手动 checklist，漏洞缺少 source-to-sink 可达性验证 |
| `trivy` | 可选 | `dependency-audit` 退化为 AI 分析 manifest 文件，无 CVE 数据库匹配 |

支持语言：Go、Java、Python、JS/TS、PHP、C/C++。

> yak 引擎通过 MCP 接入，默认 `config.yaml` 已包含 `syntaxflow` 服务器配置：
> ```yaml
> mcp_servers:
>   syntaxflow:
>     type: stdio
>     command: yak
>     args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
> ```

### 场景 2: 渗透测试

`pentest` Agent 通过浏览器自动化对运行中的 Web 应用进行安全测试。

```bash
# 必需：安装 agent-browser（浏览器自动化），会自动下载 Chromium
npm install -g agent-browser && agent-browser install
```

```
aster
> /agent pentest
> /mode yolo
> 对 http://localhost:8080 做一次全面渗透测试
```

| 工具 | 状态 | 不安装时的影响 |
|------|------|---------------|
| `agent-browser` + Chrome/Chromium | **必需** | 浏览器自动化不可用；SQL 注入、IDOR 等技能仍可基于代码分析工作 |

> **权限模式**：渗透测试产生大量浏览器命令，推荐 `/mode yolo`（隔离环境全自动）。默认 `/mode manual` 需逐条确认，体验较差。

支持自签证书、SPA/MPA、需认证的站点。

### 场景 3: 主机防护

`host-defense` Agent 进行安全基线检查、入侵检测和应急响应，**无需额外安装外部工具**。

```
aster
> /agent host-defense
> /mode yolo
> 检查当前主机的安全基线配置
```

| 工具 | 状态 | 不安装时的影响 |
|------|------|---------------|
| `root` / `sudo` 权限 | 推荐 | 部分检查（shadow 文件、SUID 扫描、审计日志）需要权限，无权限时自动跳过 |
| `yara` / `chkrootkit` / `rkhunter` | 可选 | 恶意软件检测退化为 AI 启发式分析 + 内置 bash 检查 |

> **操作系统**：Linux 完整支持，macOS 部分支持，暂不支持 Windows。

---

## 致谢

| 项目 | 用途 | 许可证 |
|------|------|--------|
| [Yaklang](https://github.com/yaklang/yaklang) | SyntaxFlow SSA 数据流分析 | AGPL-3.0 |
| [Semgrep](https://github.com/semgrep/semgrep) | SAST 静态分析引擎 | LGPL-2.1 |
| [Bubbletea](https://github.com/charmbracelet/bubbletea) | 终端 TUI 框架 | MIT |
| [mcp-go](https://github.com/mark3labs/mcp-go) | Go MCP 协议实现 | MIT |

---

## 项目热度

[![Star History Chart](https://api.star-history.com/svg?repos=Q16G/aster&type=Date)](https://star-history.com/#Q16G/aster&Date)

---

## 重要安全声明

### 🔐 法律合规声明

1. 禁止任何未经授权的漏洞测试、渗透测试或安全评估
2. 本项目仅供网络空间安全学术研究、教学和学习使用
3. 严禁将本项目用于任何非法目的或未经授权的安全测试

### 漏洞上报责任

1. 发现任何安全漏洞时，请及时通过合法渠道上报
2. 严禁利用发现的漏洞进行非法活动
3. 遵守国家网络安全法律法规，维护网络空间安全

### 使用限制

- 仅限在授权环境下用于教育和研究目的
- 禁止用于对未授权系统进行安全测试
- 使用者需对自身行为承担全部法律责任

### 免责声明

作者不对任何因使用本项目而导致的直接或间接损失负责，使用者需对自身行为承担全部法律责任。

---

## License

本项目源代码基于 [MIT License](LICENSE) 开源。

### 外部工具声明

ASTER 通过子进程 / MCP 协议调用以下外部工具，**不引入、不链接、不修改**其源代码，也不随本项目分发这些工具的二进制文件——用户需自行安装：

| 工具 | 用途 | 工具自身许可证 | 集成方式 |
|------|------|---------------|---------|
| [Semgrep](https://github.com/semgrep/semgrep) | SAST 静态分析引擎 | LGPL-2.1 | CLI 子进程 |
| [Yaklang](https://github.com/yaklang/yaklang) | SyntaxFlow SSA 数据流分析 | AGPL-3.0 | MCP stdio |

> Semgrep（LGPL-2.1）和 Yaklang（AGPL-3.0）的 copyleft 条款不适用于本项目——它们作为独立程序通过进程间通信被调用，不构成衍生作品或组合作品（参见 [GNU GPL FAQ](https://www.gnu.org/licenses/gpl-faq.html#MereAggregation)）。

### 内置规则

`semgrep-rules/` 目录中的 SAST 规则由 ASTER 团队独立编写，随项目以 MIT 协议发布。
