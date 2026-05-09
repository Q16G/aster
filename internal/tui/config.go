package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aster/internal/ai"
	"aster/internal/mcp"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	BaseURL      string                `yaml:"base_url"`
	APIKey       string                `yaml:"api_key"`
	DefaultModel string                `yaml:"default_model"`
	Headers      map[string]string     `yaml:"headers,omitempty"`
	PromptCache  *ai.PromptCacheConfig `yaml:"prompt_cache,omitempty"`
	Env          map[string]string     `yaml:"env,omitempty"`
}

type AppConfig struct {
	Providers       map[string]*ProviderConfig      `yaml:"providers"`
	DefaultProvider string                          `yaml:"default_provider"`
	MCPServers      map[string]*mcp.MCPServerConfig `yaml:"mcp_servers"`
}

type ProviderState struct {
	Name        string
	BaseURL     string
	APIKey      string
	ModelID     string
	Headers     map[string]string
	PromptCache *ai.PromptCacheConfig
	Env         map[string]string
	Proxy       string
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
  精通多种编程语言和框架的安全漏洞模式。先盘点攻击面与权限边界，再执行
  多介质 SAST 扫描（源码 + XML/配置/模板），对需要确认的链路使用
  SyntaxFlow MCP 做 topdef/bottomUse 数据流验证，并按需补充业务逻辑与
  认证授权复核，给出覆盖声明明确、分桶清晰的审计结论。
policies:
  result_source: latest_step_result
  publish_contract: sast-findings
skill_names:
  - security-code-analysis
  - sast-scan
  - dataflow-analysis
  - business-logic-auth-review
tool_names:
  - list_files
  - read_file
  - rg
  - list_skills
  - load_skills
output_contracts:
  sast-findings:
    schema: |
      {
        "type": "object",
        "required": ["total_findings", "severity_counts", "findings"],
        "properties": {
          "total_findings": { "type": "integer", "description": "发现总数" },
          "severity_counts": {
            "type": "object",
            "properties": {
              "critical": { "type": "integer" },
              "high":     { "type": "integer" },
              "medium":   { "type": "integer" },
              "low":      { "type": "integer" },
              "info":     { "type": "integer" }
            }
          },
          "scan_target": { "type": "string", "description": "扫描目标路径或仓库" },
          "assessment": {
            "type": "object",
            "description": "覆盖声明、噪声分桶与认证授权复核补充信息",
            "properties": {
              "coverage": {
                "type": "object",
                "properties": {
                  "languages": { "type": "array", "items": { "type": "string" } },
                  "framework_signals": { "type": "array", "items": { "type": "string" } },
                  "java_files": { "type": "integer" },
                  "xml_mappers": { "type": "integer" },
                  "config_files": { "type": "integer" },
                  "template_files": { "type": "integer" }
                }
              },
              "high_noise_findings": {
                "type": "array",
                "items": { "type": "string" }
              },
              "scan_gaps": {
                "type": "array",
                "items": { "type": "string" }
              },
              "fallback_mode": {
                "type": "object",
                "properties": {
                  "ssa_available": { "type": "boolean" },
                  "fallback_used": { "type": "boolean" },
                  "fallback_checklist_completed": { "type": "boolean" }
                }
              },
              "authn_authz_review": {
                "type": "object",
                "properties": {
                  "completed": { "type": "boolean" },
                  "notes": { "type": "array", "items": { "type": "string" } }
                }
              }
            }
          },
          "findings": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["id", "title", "severity", "file", "line"],
              "properties": {
                "id":             { "type": "string" },
                "title":          { "type": "string" },
                "severity":       { "type": "string", "enum": ["critical","high","medium","low","info"] },
                "file":           { "type": "string" },
                "line":           { "type": "integer" },
                "rule_id":        { "type": "string" },
                "cwe":            { "type": "string" },
                "description":    { "type": "string" },
                "snippet":        { "type": "string" },
                "recommendation": { "type": "string" },
                "dataflow_verified": { "type": "boolean" }
              }
            }
          }
        }
      }
    example: |
      {
        "total_findings": 3,
        "severity_counts": { "critical": 1, "high": 1, "medium": 1, "low": 0, "info": 0 },
        "scan_target": "./src",
        "assessment": {
          "coverage": { "languages": ["java"], "framework_signals": ["Spring", "MyBatis"], "java_files": 120, "xml_mappers": 8, "config_files": 6, "template_files": 4 },
          "high_noise_findings": ["Thymeleaf SSTI", "JNDI audit"],
          "scan_gaps": [],
          "fallback_mode": { "ssa_available": true, "fallback_used": false, "fallback_checklist_completed": false },
          "authn_authz_review": { "completed": true, "notes": ["management endpoints reviewed", "ownership checklist reviewed"] }
        },
        "findings": [
          { "id": "F-001", "title": "SQL Injection", "severity": "critical", "file": "src/db/query.go", "line": 42, "rule_id": "go.lang.security.audit.sqli", "cwe": "CWE-89", "description": "用户输入直接拼接到 SQL 查询", "snippet": "db.Raw(\"SELECT * FROM users WHERE id=\" + input)", "recommendation": "使用参数化查询", "dataflow_verified": true }
        ]
      }
    summary_policy: |
      禁止压缩或省略 findings 列表；total_findings 和 severity_counts 必须原样保留。
      long_summary 中必须逐条列出所有 finding 的 id、title、severity、file:line，并说明 assessment 中的 coverage、scan_gaps、high_noise_findings 与 fallback_mode。
      若 findings 超过 30 条，可按 severity 分组呈现，但不得省略任何条目。
`,

	"pentest.yaml": `name: pentest
role: 渗透测试专家，擅长信息收集、漏洞发现、漏洞利用和安全评估。核心能力为通过 agent-browser 控制浏览器进行 Web 安全测试
background: |
  精通 Web 安全浏览器自动化测试，通过 agent-browser CLI 控制浏览器访问目标站点，
  主动探索页面结构、交互流程和 API 接口，捕获真实网络流量并进行深度安全分析。
  掌握 SQL 注入、XSS、IDOR、CORS、文件上传、JWT 等全面的 Web 安全检测技术。
  遵循 OWASP 测试指南和 PTES 标准。
policies:
  result_source: latest_step_result
  publish_contract: pentest-findings
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
output_contracts:
  pentest-findings:
    schema: |
      {
        "type": "object",
        "required": ["total_findings", "severity_counts", "target", "findings"],
        "properties": {
          "total_findings": { "type": "integer", "description": "发现总数" },
          "severity_counts": {
            "type": "object",
            "properties": {
              "critical": { "type": "integer" },
              "high":     { "type": "integer" },
              "medium":   { "type": "integer" },
              "low":      { "type": "integer" },
              "info":     { "type": "integer" }
            }
          },
          "target": { "type": "string", "description": "测试目标 URL 或域名" },
          "findings": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["id", "title", "severity", "endpoint"],
              "properties": {
                "id":               { "type": "string" },
                "title":            { "type": "string" },
                "severity":         { "type": "string", "enum": ["critical","high","medium","low","info"] },
                "vulnerability_type": { "type": "string", "description": "漏洞类型，如 SQLi、XSS、IDOR 等" },
                "endpoint":         { "type": "string", "description": "受影响的 URL 或 API 端点" },
                "method":           { "type": "string", "description": "HTTP 方法" },
                "parameter":        { "type": "string", "description": "受影响的参数" },
                "cwe":              { "type": "string" },
                "description":      { "type": "string" },
                "proof_of_concept": { "type": "string", "description": "PoC 请求或复现步骤" },
                "recommendation":   { "type": "string" }
              }
            }
          }
        }
      }
    example: |
      {
        "total_findings": 2,
        "severity_counts": { "critical": 1, "high": 1, "medium": 0, "low": 0, "info": 0 },
        "target": "https://example.com",
        "findings": [
          { "id": "PT-001", "title": "SQL Injection in login", "severity": "critical", "vulnerability_type": "SQLi", "endpoint": "/api/login", "method": "POST", "parameter": "username", "cwe": "CWE-89", "description": "登录接口 username 参数存在 SQL 注入", "proof_of_concept": "POST /api/login {\"username\": \"admin' OR 1=1--\"}", "recommendation": "使用参数化查询" }
        ]
      }
    summary_policy: |
      禁止压缩或省略 findings 列表；total_findings 和 severity_counts 必须原样保留。
      long_summary 中必须逐条列出所有 finding 的 id、title、severity、endpoint。
      若 findings 超过 30 条，可按 vulnerability_type 分组呈现，但不得省略任何条目。
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
	if len(cfg.Providers) > 0 {
		cleanCfg.Providers = make(map[string]*ProviderConfig, len(cfg.Providers))
		for k, v := range cfg.Providers {
			if v == nil {
				continue
			}
			cp := *v
			cp.Headers = cloneStringMap(v.Headers)
			cp.PromptCache = v.PromptCache.Clone()
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
	state := c.ResolveProviderState(cliProvider, cliModel, cliBaseURL, cliAPIKey)
	if state == nil {
		return "", "", "", ""
	}
	return state.Name, state.BaseURL, state.APIKey, state.ModelID
}

func (c *AppConfig) ResolveProviderState(cliProvider, cliModel, cliBaseURL, cliAPIKey string) *ProviderState {
	var (
		providerName string
		baseURL      string
		apiKey       string
		model        string
	)
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

	resolvedEnv := expandProviderEnvMap(p.Env)
	headers := cloneStringMap(p.Headers)
	for key, value := range headers {
		headers[key] = expandProviderValue(value, resolvedEnv)
	}

	baseURL = firstNonEmpty(cliBaseURL, os.Getenv("ASTER_BASE_URL"), expandProviderValue(p.BaseURL, resolvedEnv), bpBaseURL, "https://api.openai.com/v1")
	apiKey = firstNonEmpty(cliAPIKey, os.Getenv("ASTER_API_KEY"), bpAPIKey, expandProviderValue(p.APIKey, resolvedEnv))
	model = firstNonEmpty(cliModel, os.Getenv("ASTER_MODEL"), p.DefaultModel, bpDefaultModel, "gpt-4o")
	return &ProviderState{
		Name:        providerName,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		ModelID:     model,
		Headers:     headers,
		PromptCache: p.PromptCache.Clone(),
		Env:         resolvedEnv,
		Proxy:       providerProxyFromEnv(resolvedEnv),
	}
}

func (c *AppConfig) ToMCPConfig() *mcp.Config {
	if len(c.MCPServers) == 0 {
		return nil
	}
	return &mcp.Config{MCPServers: c.MCPServers}
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
	for name, sc := range cfg.MCPServers {
		if sc == nil {
			delete(cfg.MCPServers, name)
			continue
		}
		sc.Name = name
		if expandHeaders {
			expandMCPHeaders(sc)
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
