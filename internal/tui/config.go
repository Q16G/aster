package tui

import (
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
	BaseURL        string                       `yaml:"base_url"`
	APIKey         string                       `yaml:"api_key"`
	DefaultModel   string                       `yaml:"default_model"`
	Headers        map[string]string            `yaml:"headers,omitempty"`
	PromptCache    *ai.PromptCacheConfig        `yaml:"prompt_cache,omitempty"`
	Variants       map[string]map[string]any    `yaml:"variants,omitempty"`
	Env            map[string]string            `yaml:"env,omitempty"`
	SupportsVision *bool                        `yaml:"supports_vision,omitempty"`
	SupportsAudio  *bool                        `yaml:"supports_audio,omitempty"`
	Timeout        *int                         `yaml:"timeout,omitempty"`
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

func ResetAgentDefaults() ([]string, error) {
	agentsDir := filepath.Join(DefaultAppDir(), "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create agents dir: %w", err)
	}
	names := DefaultAgentNames()
	reset := make([]string, 0, len(names))
	for _, name := range names {
		agentPath := filepath.Join(agentsDir, name)
		if err := os.WriteFile(agentPath, []byte(defaultAgentFiles[name]), 0o644); err != nil {
			return reset, fmt.Errorf("write agent %s: %w", name, err)
		}
		reset = append(reset, name)
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
providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    default_model: gpt-4o
  # env:
  #   HTTPS_PROXY: http://127.0.0.1:7890

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
  精通多种编程语言和框架的安全漏洞模式。审计范围覆盖但不限于以下类别：

  结构化漏洞：RCE、SQL 注入、XSS（反射/存储/DOM）、XXE、SSRF、命令注入、
  路径穿越、反序列化、模板注入、HTTP 响应头注入、不安全的文件操作

  认证与授权：认证绕过、水平/垂直越权（IDOR）、session 固定/劫持、
  CSRF、敏感操作缺少二次验证、OAuth/JWT 误用

  业务逻辑：竞态条件、支付/积分逻辑篡改、批量操作滥用、
  工作流跳步、敏感信息泄露（错误消息/调试接口/日志）

  配置与依赖：安全 header 缺失、CORS 配置不当、调试模式泄露、
  依赖已知漏洞（SCA）、敏感信息硬编码
instruction: |
  ## 审计策略
  - 首先加载 security-code-analysis（P0 总控路由），它定义了信号路由表和覆盖维度，指导后续 skill 的加载和编排
  - **用户意图优先**：当用户明确指定审计方向时，计划和执行必须聚焦在用户指定的方向内，不要主动扩展到用户未提及的维度。MUST 覆盖维度仅在全量审计（用户未指定方向）时才作为强制要求
  - 全量审计时，分析手段和顺序根据项目实际情况和可用工具集灵活安排，必须满足 P0 Router 定义的 MUST 覆盖维度
  - 工具能自动化完成的检测不要用纯人工逐文件审查替代
  - 给出覆盖声明明确、分桶清晰的审计结论

  ## 自主完成原则
  - 所有分析任务必须由你自主完成，禁止将你能力范围内的分析工作推给人工
  - 所有候选漏洞必须经过数据流分析验证（source-to-sink 可达性确认）。若前序步骤未覆盖数据流验证，应主动规划补充分析步骤，而不是标记为 needs_review 留给人工
  - needs_review 仅用于：已执行数据流分析但因复杂业务逻辑导致结果不确定、或纯语义判断超出工具能力的情况。每个 needs_review 条目必须附注具体原因
  - 禁止出现以下措辞：「建议人工检查数据流」「需要人工验证是否可达」「留待人工复核」等将你应完成的工作推给人类的表述

  ## 标准交付物
  - 审计报告：通过 result-with-file 持久化所有发现，按严重度分级的完整安全报告（Markdown 格式）
  - POC：每条 confirmed 漏洞必须在报告中内嵌 POC（原始 HTTP 数据包或 Python 脚本），POC 是报告的组成部分而非独立步骤
policies:
  result_source: latest_step_result
skill_names:
  - security-code-analysis
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
  - result-with-file
preload_skills:
  - security-code-analysis
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
  - load_skills
`,

	"pentest.yaml": `name: pentest
role: 渗透测试专家，擅长信息收集、漏洞发现、漏洞利用和安全评估。核心能力为通过 agent-browser 控制浏览器进行 Web 安全测试
background: |
  精通 Web 安全浏览器自动化测试，通过 agent-browser CLI 控制浏览器访问目标站点，
  主动探索页面结构、交互流程和 API 接口，捕获真实网络流量并进行深度安全分析。
  掌握 SQL 注入、XSS、IDOR、CORS、文件上传、JWT 等全面的 Web 安全检测技术。
  遵循 OWASP 测试指南和 PTES 标准。
instruction: |
  ## 测试策略
  - 首先加载 web-security-testing（P0 总控路由），它定义了信号路由表和覆盖维度，指导后续 skill 的加载和编排
  - **用户意图优先**：当用户明确指定测试方向时，计划和执行必须聚焦在用户指定的方向内，不要主动扩展到用户未提及的维度。MUST 覆盖维度仅在全面渗透测试（用户未指定方向）时才作为强制要求
  - 通过侦察阶段收集目标信号，按信号路由表加载对应 P1 Router 和 Topic Skill
  - 所有发现必须形成完整证据链（前置条件/输入 → 系统处理 → 实际效果/危害 → 可复核证据）
  - 给出覆盖声明明确的测试结论

  ## 自主完成原则
  - 所有测试任务必须由你自主完成，禁止将你能力范围内的测试工作推给人工
  - 所有候选漏洞必须经过实际验证（构造 POC 请求、对比响应、确认可利用性）。若前序步骤未覆盖验证，应主动规划补充验证步骤，而不是标记为 needs_review 留给人工
  - needs_review 仅用于：已执行验证但因目标环境限制（如 WAF 拦截、需要特定权限）导致无法确认的情况。每个 needs_review 条目必须附注具体原因
  - 禁止出现以下措辞：「建议人工验证」「需要人工确认可利用性」「留待人工复核」等将你应完成的工作推给人类的表述

  ## 标准交付物
  - 测试报告：通过 result-with-file 持久化所有发现，按严重度分级的完整安全报告（Markdown 格式）
  - POC：每条 confirmed 漏洞必须在报告中内嵌 POC（原始 HTTP 数据包或 Python 脚本），POC 是报告的组成部分而非独立步骤
policies:
  result_source: latest_step_result
skill_names:
  - web-security-testing
  - agent-browser
  - recon-methodology
  - injection-testing
  - SQL注入-多策略综合检测
  - xss-testing
  - command-injection
  - ssrf-testing
  - xxe-testing
  - ssti-testing
  - access-control
  - 认证安全综合检测
  - 越权访问-IDOR检测
  - 越权访问-垂直越权检测
  - 越权访问-未授权访问检测
  - csrf-testing
  - file-and-path-sec
  - 文件上传-多策略综合检测
  - path-traversal-lfi
  - http-protocol-sec
  - open-redirect-testing
  - api-token-sec
  - CORS-配置错误检测
  - JWT-弱密钥与信息泄露检测
  - business-logic-testing
  - 通知滥用-邮箱短信轰炸检测
  - 注册机制-批量注册检测
  - race-condition
  - 隐私保护-敏感信息未脱敏检测
preload_skills:
  - web-security-testing
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

	baseURL = firstNonEmpty(cliBaseURL, os.Getenv("ASTER_BASE_URL"), expandProviderValue(p.BaseURL, resolvedEnv), regBaseURL, "https://api.openai.com/v1")
	apiKey = firstNonEmpty(cliAPIKey, os.Getenv("ASTER_API_KEY"), credKey, regAPIKey, expandProviderValue(p.APIKey, resolvedEnv))
	model = firstNonEmpty(cliModel, os.Getenv("ASTER_MODEL"), p.DefaultModel, "gpt-4o")
	return &ProviderState{
		Name:           providerName,
		Protocol:       regProtocol,
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
