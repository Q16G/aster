package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"aster/internal/mcp"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	BaseURL      string `yaml:"base_url"`
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
}

type AppConfig struct {
	Providers       map[string]*ProviderConfig      `yaml:"providers"`
	DefaultProvider string                          `yaml:"default_provider"`
	MCPServers      map[string]*mcp.MCPServerConfig `yaml:"mcp_servers"`
}

type ProviderState struct {
	Name    string
	BaseURL string
	APIKey  string
	ModelID string
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
	for name, content := range defaultAgentFiles {
		agentPath := filepath.Join(agentsDir, name)
		if _, err := os.Stat(agentPath); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(agentPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write agent %s: %w", name, err)
			}
		}
	}

	return nil
}

var defaultConfigYAML = `# ASTER Configuration
# https://github.com/... (项目文档)

# 默认 AI provider
default_provider: openai

# Provider 配置
# API Key 支持直接填写或引用环境变量: ${ENV_VAR}
# 也可以在 TUI 中通过 /provider 命令在线配置
providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    default_model: gpt-4o

  # anthropic:
  #   base_url: https://api.anthropic.com/v1
  #   api_key: ${ANTHROPIC_API_KEY}
  #   default_model: claude-sonnet-4-20250514

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
role: 通用 AI 助手
background: |
  你是一个通用的 AI 编程助手，能够帮助用户完成代码编写、
  调试、重构和技术问题解答等任务。
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
role: 代码安全审计专家，擅长静态分析、漏洞模式识别、数据流追踪和安全编码指导
background: |
  精通多种编程语言和框架的安全漏洞模式。使用 Semgrep 进行多通道 SAST 扫描
  （本地嵌入规则 + 社区注册表 + OWASP），通过 SyntaxFlow MCP 进行
  topdef/bottomUse 数据流追踪验证，结合 AI 补充鉴权缺失和业务逻辑分析，
  给出精确的漏洞定位和修复建议。
skill_names:
  - sast-scan
  - dataflow-analysis
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
  - load_skills
`,

	"pentest.yaml": `name: pentest
role: 渗透测试专家，擅长信息收集、漏洞发现、漏洞利用和安全评估。核心能力为通过 agent-browser 控制浏览器进行 Web 安全测试
background: |
  精通 Web 安全浏览器自动化测试，通过 agent-browser CLI 控制浏览器访问目标站点，
  主动探索页面结构、交互流程和 API 接口，捕获真实网络流量并进行深度安全分析。
  掌握 SQL 注入、XSS、IDOR、CORS、文件上传、JWT 等全面的 Web 安全检测技术。
  遵循 OWASP 测试指南和 PTES 标准。
skill_names:
  - agent-browser
  - SQL注入-多策略综合检测
  - 越权访问-IDOR检测
  - 越权访问-垂直越权检测
  - 越权访问-未授权访问检测
  - CORS-配置错误检测
  - JWT-弱密钥与信息泄露检测
  - 文件上传-多策略综合检测
  - 认证安全综合检测
  - 通知滥用-邮箱短信轰炸检测
  - 隐私保护-敏感信息未脱敏检测
  - 注册机制-批量注册检测
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
  - load_skills
`,

	"host-defense.yaml": `name: host-defense
role: 主机安全防护专家，擅长安全基线检查、入侵检测、恶意软件分析和应急响应
background: |
  精通 Linux/Windows 系统安全加固、入侵检测与响应、恶意软件分析。
  能够进行 CIS Benchmark 安全基线审计、多源日志关联分析、YARA 规则编写
  和 Rootkit 检测、应急响应全流程处置。
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
  - load_skills
`,
}

func LoadConfig(path string) (*AppConfig, error) {
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

	expandProviderEnv(&cfg)
	populateMCPNames(&cfg)
	return &cfg, nil
}

func SaveConfig(path string, updateFn func(cfg *AppConfig)) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return fmt.Errorf("load existing config: %w", err)
	}
	if cfg == nil {
		cfg = &AppConfig{}
	}
	updateFn(cfg)

	cleanCfg := *cfg
	if len(cfg.Providers) > 0 {
		cleanCfg.Providers = make(map[string]*ProviderConfig, len(cfg.Providers))
		for k, v := range cfg.Providers {
			cp := *v
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
	providerName = firstNonEmpty(cliProvider, os.Getenv("ASTER_PROVIDER"), c.DefaultProvider, "openai")

	var p *ProviderConfig
	if c.Providers != nil {
		p = c.Providers[providerName]
	}

	var bpBaseURL, bpAPIKey, bpDefaultModel string
	if bp, ok := GetBuiltinProvider(providerName); ok {
		bpBaseURL = bp.BaseURL
		bpDefaultModel = bp.DefaultModel
		bpAPIKey = resolveAPIKey(bp, "")
		if p != nil {
			bpAPIKey = resolveAPIKey(bp, p.APIKey)
		}
	}

	if p == nil {
		p = &ProviderConfig{}
	}

	baseURL = firstNonEmpty(cliBaseURL, os.Getenv("ASTER_BASE_URL"), p.BaseURL, bpBaseURL, "https://api.openai.com/v1")
	apiKey = firstNonEmpty(cliAPIKey, os.Getenv("ASTER_API_KEY"), bpAPIKey, p.APIKey)
	model = firstNonEmpty(cliModel, os.Getenv("ASTER_MODEL"), p.DefaultModel, bpDefaultModel, "gpt-4o")
	return
}

func (c *AppConfig) ToMCPConfig() *mcp.Config {
	if len(c.MCPServers) == 0 {
		return nil
	}
	return &mcp.Config{MCPServers: c.MCPServers}
}

func expandProviderEnv(cfg *AppConfig) {
	expand := func(s string) string {
		return os.Expand(s, os.Getenv)
	}
	for _, p := range cfg.Providers {
		if p == nil {
			continue
		}
		p.BaseURL = expand(p.BaseURL)
		p.APIKey = expand(p.APIKey)
	}
}

func populateMCPNames(cfg *AppConfig) {
	for name, sc := range cfg.MCPServers {
		if sc == nil {
			delete(cfg.MCPServers, name)
			continue
		}
		sc.Name = name
		expandMCPHeaders(sc)
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
