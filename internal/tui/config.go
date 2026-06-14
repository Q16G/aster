package tui

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aster/internal/ai"
	"aster/internal/mcp"
	"aster/internal/provider"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	BaseURL        string                    `yaml:"base_url"`
	APIKey         string                    `yaml:"api_key"`
	DefaultModel   string                    `yaml:"default_model"`
	Protocol       string                    `yaml:"protocol,omitempty"`
	Headers        map[string]string         `yaml:"headers,omitempty"`
	PromptCache    *ai.PromptCacheConfig     `yaml:"prompt_cache,omitempty"`
	Variants       map[string]map[string]any `yaml:"variants,omitempty"`
	Env            map[string]string         `yaml:"env,omitempty"`
	SupportsVision *bool                     `yaml:"supports_vision,omitempty"`
	SupportsAudio  *bool                     `yaml:"supports_audio,omitempty"`
	Timeout        *int                      `yaml:"timeout,omitempty"`
}

type AppConfig struct {
	Env              map[string]string               `yaml:"env,omitempty"`
	Providers        map[string]*ProviderConfig      `yaml:"providers"`
	DefaultProvider  string                          `yaml:"default_provider"`
	ProviderPriority []string                        `yaml:"provider_priority,omitempty"`
	MCPServers       map[string]*mcp.MCPServerConfig `yaml:"mcp_servers"`
}

var DefaultProviderPriority = []string{
	"openai", "anthropic", "deepseek", "groq", "openrouter", "together", "ollama",
}

func (c *AppConfig) effectiveProviderPriority() []string {
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority
	}
	return DefaultProviderPriority
}

type ProviderState struct {
	Name           string
	Protocol       string
	BaseURL        string
	APIKey         string
	ModelID        string
	Variant        string
	VariantOptions map[string]any
	Headers        map[string]string
	PromptCache    *ai.PromptCacheConfig
	Env            map[string]string
	Proxy          string
	SupportsVision *bool
	SupportsAudio  *bool
	Timeout        *int
}

const (
	AppName    = "ASTER"
	AppCLIName = "aster"
	AppDirName = ".aster"
)

func DefaultAppDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return AppDirName
	}
	return filepath.Join(home, AppDirName)
}

func DefaultConfigPath() string {
	return filepath.Join(DefaultAppDir(), "config.yaml")
}

func EnsureAppDefaults() error {
	appDir := DefaultAppDir()
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create app dir: %w", err)
	}

	configPath := filepath.Join(appDir, "config.yaml")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(configPath, []byte(defaultConfigYAML), 0o644); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	agentsDir := filepath.Join(appDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}
	if err := syncAgentDefaults(agentsDir); err != nil {
		return err
	}

	return nil
}

const agentHashMarker = ".hash"

func agentContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum)[:16]
}

// syncAgentDefaults reconciles ~/.aster/agents with the embedded defaults using
// a per-file content hash recorded in the .hash marker. A built-in file is
// (over)written only when it is missing on disk or its embedded content changed
// since the last sync; files whose built-in content is unchanged are left as-is
// (preserving local edits). Built-in files that were removed from the embedded
// set are deleted. User-authored agents never appear in the marker, so they are
// untouched.
func syncAgentDefaults(agentsDir string) error {
	oldHashes := readAgentHashMarker(agentsDir)
	newHashes := make(map[string]string, len(defaultAgentFiles))

	for name, content := range defaultAgentFiles {
		newHash := agentContentHash(content)
		newHashes[name] = newHash

		agentPath := filepath.Join(agentsDir, name)
		_, statErr := os.Stat(agentPath)
		missing := errors.Is(statErr, os.ErrNotExist)
		if missing || oldHashes[name] != newHash {
			if err := os.WriteFile(agentPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write agent %s: %w", name, err)
			}
		}
	}

	for name := range oldHashes {
		if _, ok := defaultAgentFiles[name]; ok {
			continue
		}
		_ = os.Remove(filepath.Join(agentsDir, name))
	}

	return writeAgentHashMarker(agentsDir, newHashes)
}

func readAgentHashMarker(agentsDir string) map[string]string {
	hashes := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(agentsDir, agentHashMarker))
	if err != nil {
		return hashes
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hashes[fields[0]] = fields[1]
	}
	return hashes
}

func writeAgentHashMarker(agentsDir string, hashes map[string]string) error {
	names := make([]string, 0, len(hashes))
	for name := range hashes {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('\t')
		b.WriteString(hashes[name])
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(agentsDir, agentHashMarker), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write agent hash marker: %w", err)
	}
	return nil
}

func ResetAgentDefaults() ([]string, error) {
	agentsDir := filepath.Join(DefaultAppDir(), "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create agents dir: %w", err)
	}
	names := DefaultAgentNames()
	reset := make([]string, 0, len(names))
	hashes := make(map[string]string, len(names))
	for _, name := range names {
		agentPath := filepath.Join(agentsDir, name)
		content := defaultAgentFiles[name]
		if err := os.WriteFile(agentPath, []byte(content), 0o644); err != nil {
			return reset, fmt.Errorf("write agent %s: %w", name, err)
		}
		hashes[name] = agentContentHash(content)
		reset = append(reset, name)
	}
	if err := writeAgentHashMarker(agentsDir, hashes); err != nil {
		return reset, err
	}
	return reset, nil
}

func DefaultAgentNames() []string {
	names := make([]string, 0, len(defaultAgentFiles))
	for name := range defaultAgentFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ParseDefaultAgentProfiles() []ProfileYAML {
	var profiles []ProfileYAML
	names := make([]string, 0, len(defaultAgentFiles))
	for name := range defaultAgentFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		content := defaultAgentFiles[name]
		var p ProfileYAML
		if err := yaml.Unmarshal([]byte(content), &p); err != nil {
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		}
		profiles = append(profiles, p)
	}
	return profiles
}

var defaultConfigYAML = `# ASTER Configuration
# https://github.com/... (项目文档)

# 全局环境变量（所有 MCP 服务器和 provider 共享）
# env:
#   HTTPS_PROXY: socks5://127.0.0.1:7890
#   MCP_TOKEN: ${MY_SECRET}

# 默认 AI provider
default_provider: openai

# Provider 配置
# API Key 支持直接填写或引用环境变量: ${ENV_VAR}
# 也可以在 TUI 中通过 /provider 命令在线配置
#
# protocol: 显式指定该 provider 的 API 风格，可选 openai / anthropic
#   - 不填时按内置注册表（provider 名字）自动推断，注册表也没有则默认 openai
#   - anthropic 风格会自动补全 /messages 路径
providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    default_model: gpt-4o
  # env:
  #   HTTPS_PROXY: http://127.0.0.1:7890

  # minimax-cn:                    # A社(anthropic)风格示例
  #   protocol: anthropic          # 可省略，注册表已知 minimax-cn 为 anthropic
  #   base_url: https://api.minimaxi.com/anthropic/v1  # 可省略，会自动补 /messages
  #   api_key: ${MINIMAX_API_KEY}
  #   default_model: MiniMax-M2

  # anthropic:
  #   base_url: https://api.anthropic.com/v1
  #   api_key: ${ANTHROPIC_API_KEY}
  #   default_model: claude-sonnet-4-20250514
  #   env:
  #     HTTPS_PROXY: ${ASTER_PROXY}
  #   headers:
  #     anthropic-version: "2023-06-01"
  #   prompt_cache:
  #     enabled: true
  #     retention: 5m
  #     families: ["think_act"]

  # deepseek:
  #   base_url: https://api.deepseek.com/v1
  #   api_key: ${DEEPSEEK_API_KEY}
  #   default_model: deepseek-chat

  # ollama:
  #   base_url: http://localhost:11434/v1
  #   default_model: qwen2.5:latest

# MCP 服务器配置
mcp_servers:
  syntaxflow:
    description: "SyntaxFlow 数据流分析引擎（yak SSA）"
    type: stdio
    command: yak
    args: ["mcp", "--transport", "stdio", "--tool", "ssa"]
    resident: false

  # example-server:
  #   type: stdio
  #   command: my-mcp-server
  #   args: ["--mode", "production"]
  #   resident: false
  #
  # remote-server:
  #   type: streamable-http
  #   url: https://mcp.example.com/api
  #   headers:
  #     Authorization: "Bearer ${MCP_TOKEN}"
`

var defaultAgentFiles = map[string]string{
	"example.yaml": `# 自定义 Agent 示例
# 将此文件放在 ~/.aster/agents/ 目录下，启动时自动加载
# 文件名（不含扩展名）即为 agent 名称，也可用 name 字段覆盖

name: example
# 三要素写法（详见 internal/react/prompts/README.md；反例与正确归位见 §2）：
#   role        — 身份/职业定位（"是什么人"），不写行为约束、不写路径/任务参数
#   background  — 背景知识/熟悉度（"懂什么/见过什么"），不写当前任务场景、不写 runtime 路径、不写指令
#   instruction — 微调指令/行为约束（"按什么规矩干"），不重复身份描述、不嵌固定编号流水线
role: 通用 AI 助手
background: |
  熟悉常见编程语言、主流框架与日常开发流程；
  对代码编写、调试、重构与技术问答有广泛实战经验。
instruction: |
  请用中文回答用户问题。优先给出简洁的解决方案，
  必要时提供详细解释。

# 可选：指定模型（覆盖 provider 默认模型）
# model_id: gpt-4o

# 可选：指定可用工具
# tool_names:
#   - list_files
#   - read_file
#   - rg

# 可选：指定可用技能
# skill_names:
#   - sast-scan
#   - dataflow-analysis

# 可选：运行策略
# policies:
#   max_iterations: 1000
#   allow_bash: true
#   enable_history_compaction: true

# 可选：为此 agent 专属的 MCP 服务器
# mcp_servers:
#   - name: my-tool
#     type: stdio
#     command: my-tool-server
`,

	"code-audit.yaml": `name: code-audit
role: 代码安全审计专家，专长 Web/后端应用源码漏洞挖掘与数据流验证
background: |
  精通多种编程语言和框架的安全漏洞模式，熟悉静态分析、模式识别、source→sink 数据流追踪与安全编码实践。审计范围覆盖但不限于以下类别：

  结构化漏洞：RCE、SQL 注入、XSS（反射/存储/DOM）、XXE、SSRF、命令注入、
  路径穿越、反序列化、模板注入、HTTP 响应头注入、不安全的文件操作

  认证与授权：认证绕过、水平/垂直越权（IDOR）、session 固定/劫持、
  CSRF、敏感操作缺少二次验证、OAuth/JWT 误用

  业务逻辑：竞态条件、支付/积分逻辑篡改、批量操作滥用、
  工作流跳步、敏感信息泄露（错误消息/调试接口/日志）

  配置与依赖：安全 header 缺失、CORS 配置不当、调试模式泄露、
  依赖已知漏洞（SCA）、敏感信息硬编码
instruction: |
  ## 审计模式
  - 数据流可达性驱动，source→sink 走通才下 confirmed
  - 候选漏洞通过分析验证后下结论，不留"需人工确认"出口

  ## 侦察前置
  - 开放式诉求先做项目结构识别 + 攻击面盘点；诉求半径有限时按指定边界审计

  ## 静态判定上限
  - 白盒结论上限为 static-confirmed（数据流可达性已证），非动态 confirmed；升级需黑盒侧验证

  ## 交付
  - 按严重度分级的审计报告，每条 confirmed 内嵌可复跑 POC
  - 覆盖账本：confirmed / static-confirmed / safe-with-evidence / n/a-with-reason 四态分别落值
policies:
  result_source: latest_step_result
skill_names:
  - project-framework-analysis
  - sast-scan
  - dataflow-analysis
  - stored-xss-detection
  - auth-authz
  - business-logic-auth-review
  - session-security
  - client-side-sec
  - csp-audit
  - client-js-audit
  - config-sec
  - secret-detection
  - security-header-audit
  - dangerous-config
  - dependency-audit
  - dependency-decompile
  - result-with-file
preload_skills:
  - project-framework-analysis
  - result-with-file
mcp_servers:
  - name: yak
    type: stdio
    command: yak
    args:
      - mcp
      - -t
      - ssa
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
`,

	"pentest.yaml": `name: pentest
role: 渗透测试专家，专长信息收集、漏洞发现、漏洞利用和安全评估
background: |
  精通 Web 安全黑盒动态测试方法与浏览器自动化测试模式；
  掌握 SQL 注入、XSS、IDOR、CORS、文件上传、JWT 等全面的 Web 安全检测技术。
  遵循 OWASP 测试指南和 PTES 标准。
  能够从前端页面和 HAR 流量中推断应用业务场景（电商、SaaS 多租户、金融、社交等），
  并据此定向识别高风险资源类型和逻辑漏洞优先级。
instruction: |
  ## 工作模式
  - 黑盒可观测驱动，不假设内部实现
  - 候选漏洞自己构造请求验证，不留"需人工确认"出口

  ## 页面分析前置
  - 新发现页面（登录 / 注册 / 后台 / 管理 / 列表 / 详情 / 上传 / API 入口 / SPA 路由切换等）先深挖页面结构、输入、关联资源、引用端点，再展开漏洞维度测试

  ## 漏洞维度横向覆盖
  - 单个入口常同时承载多个候选漏洞维度（如登录入口 = SQLi + auth + JWT + CORS + 注册反向利用 + ...）；每个候选维度独立覆盖，不以单维度结论代言整个入口
  - 同一维度内的形态收敛由该维度自判，不强求穷举
  - 覆盖判据：每个候选维度都留下 confirmed / safe-with-evidence / blocked-with-evidence / n/a-with-reason 其一的证据

  ## 阻塞 ≠ 安全
  - 防护机制阻断时按 blocked 状态记录，与 safe 区分；每个端点独立结论

  ## 交付
  - 按严重度分级的漏洞报告，每条 confirmed 内嵌可复跑 POC
  - 端点 / 页面覆盖账本：四态分别落值，未覆盖范围显式声明
skill_names:
  - agent-browser
  - recon-methodology
  - page-analysis
  - sql-injection-comprehensive
  - xss-testing
  - command-injection
  - ssrf-testing
  - xxe-testing
  - ssti-testing
  - auth-comprehensive
  - idor-detection
  - vertical-privilege-escalation
  - unauthorized-access
  - csrf-testing
  - file-upload
  - path-traversal-lfi
  - open-redirect-testing
  - cors-misconfiguration
  - jwt-weakness
  - notification-abuse
  - registration-abuse
  - race-condition
  - sensitive-info-exposure
  - vuln-reproduction
  - result-with-file
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
`,

	"host-defense.yaml": `name: host-defense
role: 主机安全防护专家，专长 Linux/Windows 主机加固、入侵检测与应急响应
background: |
  精通 Linux/Windows 系统安全加固、入侵检测与响应、恶意软件分析。
  熟练进行 CIS Benchmark 安全基线审计、多源日志关联分析、YARA 规则编写
  与 Rootkit 检测，并主导过应急响应的全流程处置。
instruction: |
  ## 检查策略
  - 优先按业界基线模板覆盖；自动化检测优先于人工逐项
  - 所有结论附可复核证据，禁止凭印象判定
  - 候选异常自己执行实际验证后下结论，不留"需人工确认"出口

  ## 应急处置
  - 应急响应场景按"发现 → 取证 → 遏制 → 根除 → 恢复"输出处置时间线

  ## 交付
  - 按严重度分级的检查报告，每条发现附证据来源、判定理由、修复建议
  - 应急场景额外给出遗留风险列表
skill_names:
  - baseline-check
  - intrusion-detection
  - malware-detect
  - emergency-response
  - log-analysis
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
`,

	"ctf.yaml": `name: ctf
role: Web CTF 解题专家，专长黑盒侦察、漏洞识别与链式利用
background: |
  精通 Web 方向 CTF 解题：SQLi/SSTI/命令注入/SSRF/XXE/文件上传/LFI/
  反序列化/JWT/信息泄露(.git)/越权/客户端等考点的黑盒探测与利用，
  熟练使用 curl、sqlmap、GitHack、agent-browser 等工具。
instruction: |
  ## 解题策略
  - 以拿 flag 为唯一目标，允许激进、链式利用
  - 优先排查常见信息泄露与注入类突破口
  - 所有探测与利用自行完成，构造真实请求验证

  ## 交付
  - flag + writeup（考点 / 利用链 / 关键 payload / 证据），Markdown
policies:
  result_source: latest_step_result
skill_names:
  - web-ctf
  - agent-browser
preload_skills:
  - web-ctf
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
`,

	"graybox-test.yaml": `name: graybox-test
role: 灰盒安全测试专家，融合白盒源码审计与黑盒动态验证的 Web 应用安全测试专家
background: |
  同时具备白盒（SAST / 数据流）与黑盒（浏览器动态测试）两套能力，习惯以源码定位候选 sink、以真实请求验证可达性与可利用性，两侧证据交叉印证以降低误报。

  白盒维度（读源码定位候选漏洞）：
  结构化漏洞：RCE、SQL 注入、XSS（反射/存储/DOM）、XXE、SSRF、命令注入、
  路径穿越、反序列化、模板注入、不安全文件操作
  认证与授权：认证绕过、水平/垂直越权(IDOR)、session 固定/劫持、CSRF、OAuth/JWT 误用
  业务逻辑：竞态、支付/积分篡改、批量操作滥用、工作流跳步、敏感信息泄露
  配置与依赖：安全 header 缺失、CORS 不当、调试模式泄露、依赖已知漏洞(SCA)、硬编码密钥

  黑盒维度（控浏览器对运行目标发真实请求）：
  通过 agent-browser CLI 控制浏览器主动探索页面结构、交互流程与 API 接口，
  捕获真实网络流量，对候选漏洞构造 payload 验证可达性与可利用性。
  掌握 SQLi/XSS/IDOR/越权/CORS/JWT/文件上传/SSRF/SSTI/命令注入/路径穿越等动态检测技术，
  遵循 OWASP 测试指南与 PTES 标准。
instruction: |
  ## 灰盒模式
  - 白盒定位候选 sink、黑盒验证可达性，两侧证据交叉印证
  - 候选漏洞通过黑白盒双侧验证后下结论，不留"需人工确认"出口

  ## 输入分流
  - 只有源码：按 sink 语义做白盒分析，结论上限 static-confirmed
  - 只有目标：按响应特征做黑盒探测，结论上限黑盒 confirmed
  - 双侧齐备：白盒定位 → 黑盒验证，结论可升 confirmed

  ## 取证底线
  - 所有证据须来自实际读取的源材料或实际发出的请求；推断、记忆、模板均不算证据

  ## 委派纪律
  - 独立可并行的测试方向或大体量探测可委派子 agent；委派不降低验证强度与覆盖声明

  ## 交付
  - 按严重度分级的灰盒审计报告，每条 confirmed 内嵌可复跑 POC 与对应源码引用
  - 覆盖账本：confirmed / static-confirmed / safe-with-evidence / blocked-with-evidence / n/a-with-reason 五态分别落值
policies:
  result_source: latest_step_result
skill_names:
  # 白盒 / SAST 侦察与原子能力
  - project-framework-analysis
  - sast-scan
  - dataflow-analysis
  - dependency-decompile
  - auth-authz
  - business-logic-auth-review
  - session-security
  - secret-detection
  - stored-xss-detection
  - client-side-sec
  - csp-audit
  - client-js-audit
  - config-sec
  - dangerous-config
  - security-header-audit
  - dependency-audit
  # 黑盒 / DAST 侦察与原子能力
  - agent-browser
  - recon-methodology
  - page-analysis
  - sql-injection-comprehensive
  - xss-testing
  - idor-detection
  - vertical-privilege-escalation
  - unauthorized-access
  - ssrf-testing
  - ssti-testing
  - command-injection
  - xxe-testing
  - path-traversal-lfi
  - cors-misconfiguration
  - jwt-weakness
  - file-upload
  - csrf-testing
  - auth-comprehensive
  - open-redirect-testing
  - race-condition
  - notification-abuse
  - registration-abuse
  - sensitive-info-exposure
  # 复现
  - vuln-reproduction
  # 输出
  - result-with-file
preload_skills:
  - recon-methodology
  - project-framework-analysis
  - result-with-file
mcp_servers:
  - name: yak
    type: stdio
    command: yak
    args:
      - mcp
      - -t
      - ssa
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
`,
}

func LoadConfig(path string) (*AppConfig, error) {
	return loadConfig(path, true)
}

func loadConfig(path string, expand bool) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AppConfig{}, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if expand {
		expandProviderEnv(&cfg)
	}
	populateMCPNames(&cfg, expand)
	return &cfg, nil
}

func SaveConfig(path string, updateFn func(cfg *AppConfig)) error {
	cfg, err := loadConfig(path, false)
	if err != nil {
		return fmt.Errorf("load existing config: %w", err)
	}
	if cfg == nil {
		cfg = &AppConfig{}
	}
	updateFn(cfg)

	cleanCfg := *cfg
	cleanCfg.Env = cloneStringMap(cfg.Env)
	if len(cfg.Providers) > 0 {
		cleanCfg.Providers = make(map[string]*ProviderConfig, len(cfg.Providers))
		for k, v := range cfg.Providers {
			if v == nil {
				continue
			}
			cp := *v
			cp.Headers = cloneStringMap(v.Headers)
			cp.PromptCache = v.PromptCache.Clone()
			cp.Variants = cloneVariantsMap(v.Variants)
			cp.Env = cloneStringMap(v.Env)
			cleanCfg.Providers[k] = &cp
		}
	}

	data, err := yaml.Marshal(&cleanCfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *AppConfig) ResolveProvider(cliProvider, cliModel, cliBaseURL, cliAPIKey string) (providerName, baseURL, apiKey, model string) {
	state := c.ResolveProviderState(cliProvider, cliModel, cliBaseURL, cliAPIKey, nil, nil)
	if state == nil {
		return "", "", "", ""
	}
	return state.Name, state.BaseURL, state.APIKey, state.ModelID
}

func (c *AppConfig) ResolveProviderState(cliProvider, cliModel, cliBaseURL, cliAPIKey string, reg *provider.Registry, creds *CredentialStore) *ProviderState {
	var (
		providerName string
		baseURL      string
		apiKey       string
		model        string
	)
	providerName = firstNonEmpty(cliProvider, os.Getenv("ASTER_PROVIDER"), c.DefaultProvider)
	if providerName == "" && reg != nil {
		providerName = c.autoDetectProvider(reg, creds)
	}
	if providerName == "" {
		providerName = "openai"
	}

	var p *ProviderConfig
	if c.Providers != nil {
		p = c.Providers[providerName]
	}

	var regBaseURL, regAPIKey, regProtocol string
	if reg != nil {
		if rp, ok := reg.GetProvider(providerName); ok {
			regBaseURL = rp.BaseURL
			regProtocol = rp.Protocol
		}
		cfgKey := ""
		if p != nil {
			cfgKey = p.APIKey
		}
		regAPIKey = reg.ResolveAPIKey(providerName, cfgKey)
	}

	if p == nil {
		p = &ProviderConfig{}
	}

	resolvedEnv := expandProviderEnvMap(p.Env)
	headers := cloneStringMap(p.Headers)
	for key, value := range headers {
		headers[key] = expandProviderValue(value, resolvedEnv)
	}

	var credKey string
	if creds != nil {
		credKey = creds.Get(providerName)
	}

	protocol := normalizeProtocol(p.Protocol)
	if protocol == "" {
		protocol = regProtocol
	}

	baseURL = firstNonEmpty(cliBaseURL, os.Getenv("ASTER_BASE_URL"), expandProviderValue(p.BaseURL, resolvedEnv), regBaseURL, "https://api.openai.com/v1")
	apiKey = firstNonEmpty(cliAPIKey, os.Getenv("ASTER_API_KEY"), credKey, regAPIKey, expandProviderValue(p.APIKey, resolvedEnv))
	model = firstNonEmpty(cliModel, os.Getenv("ASTER_MODEL"), p.DefaultModel, "gpt-4o")
	return &ProviderState{
		Name:           providerName,
		Protocol:       protocol,
		BaseURL:        baseURL,
		APIKey:         apiKey,
		ModelID:        model,
		Headers:        headers,
		PromptCache:    p.PromptCache.Clone(),
		Env:            resolvedEnv,
		Proxy:          providerProxyFromEnv(resolvedEnv),
		SupportsVision: p.SupportsVision,
		SupportsAudio:  p.SupportsAudio,
		Timeout:        p.Timeout,
	}
}

func (c *AppConfig) autoDetectProvider(reg *provider.Registry, creds *CredentialStore) string {
	priority := c.effectiveProviderPriority()
	isAvailable := func(id string) bool {
		if creds != nil && creds.Get(id) != "" {
			return true
		}
		return reg.IsProviderAvailable(id)
	}
	sorted := reg.ListProvidersSorted(priority, isAvailable)
	for _, p := range sorted {
		if isAvailable(p.ID) {
			return p.ID
		}
	}
	return ""
}

func (c *AppConfig) ToMCPConfig() *mcp.Config {
	if len(c.MCPServers) == 0 {
		return nil
	}
	return &mcp.Config{
		GlobalEnv:  expandProviderEnvMap(c.Env),
		MCPServers: c.MCPServers,
	}
}

func expandProviderEnv(cfg *AppConfig) {
	for _, p := range cfg.Providers {
		if p == nil {
			continue
		}
		p.Env = expandProviderEnvMap(p.Env)
		p.BaseURL = expandProviderValue(p.BaseURL, p.Env)
		p.APIKey = expandProviderValue(p.APIKey, p.Env)
		for key, value := range p.Headers {
			p.Headers[key] = expandProviderValue(value, p.Env)
		}
	}
}

func populateMCPNames(cfg *AppConfig, expandHeaders bool) {
	globalEnv := expandProviderEnvMap(cfg.Env)
	for name, sc := range cfg.MCPServers {
		if sc == nil {
			delete(cfg.MCPServers, name)
			continue
		}
		sc.Name = name
		if expandHeaders {
			expandMCPHeaders(sc)
			sc.URL = expandProviderValue(sc.URL, globalEnv)
		}
	}
}

func expandMCPHeaders(sc *mcp.MCPServerConfig) {
	for k, v := range sc.Headers {
		sc.Headers[k] = os.Expand(v, func(key string) string {
			if val, ok := os.LookupEnv(key); ok {
				return val
			}
			return "${" + key + "}"
		})
	}
}

// normalizeProtocol 将 config 中用户填写的 protocol 风格归一化为内部协议名。
// 空字符串表示用户未配置，由调用方回退到注册表推断。
func normalizeProtocol(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "":
		return ""
	case "anthropic", "claude":
		return "anthropic"
	case "native-openai", "openai-responses", "responses":
		return "native-openai"
	case "openai", "openai-compatible":
		return "openai-compatible"
	default:
		return "openai-compatible"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func cloneVariantsMap(src map[string]map[string]any) map[string]map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(src))
	for k, v := range src {
		if v == nil {
			out[k] = nil
			continue
		}
		inner := make(map[string]any, len(v))
		for ik, iv := range v {
			inner[ik] = iv
		}
		out[k] = inner
	}
	return out
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func expandProviderEnvMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}

	raw := cloneStringMap(src)
	resolved := make(map[string]string, len(raw))
	visiting := make(map[string]bool, len(raw))

	var resolveKey func(string) string
	resolveKey = func(key string) string {
		if value, ok := resolved[key]; ok {
			return value
		}
		rawValue, ok := raw[key]
		if !ok {
			return os.Getenv(key)
		}
		if visiting[key] {
			return rawValue
		}
		visiting[key] = true
		expanded := os.Expand(rawValue, func(inner string) string {
			if _, ok := raw[inner]; ok {
				return resolveKey(inner)
			}
			return os.Getenv(inner)
		})
		visiting[key] = false
		resolved[key] = expanded
		return expanded
	}

	for key := range raw {
		resolved[key] = resolveKey(key)
	}
	return resolved
}

func expandProviderValue(value string, env map[string]string) string {
	if value == "" {
		return ""
	}
	return os.Expand(value, func(key string) string {
		if val, ok := env[key]; ok {
			return val
		}
		return os.Getenv(key)
	})
}

func providerProxyFromEnv(env map[string]string) string {
	for _, key := range []string{
		"HTTPS_PROXY",
		"https_proxy",
		"HTTP_PROXY",
		"http_proxy",
		"ALL_PROXY",
		"all_proxy",
	} {
		if value := strings.TrimSpace(env[key]); value != "" {
			return value
		}
	}
	return ""
}
