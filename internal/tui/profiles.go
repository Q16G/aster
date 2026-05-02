package tui

import (
	"aster/internal/builtin_tools"
	"aster/internal/react"
)

type ProfileRegistry struct {
	profiles map[string]react.AgentDefinition
	order    []string
}

func NewProfileRegistry() *ProfileRegistry {
	return &ProfileRegistry{
		profiles: make(map[string]react.AgentDefinition),
	}
}

func (r *ProfileRegistry) Register(def react.AgentDefinition) {
	if _, exists := r.profiles[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.profiles[def.Name] = def
}

func (r *ProfileRegistry) Get(name string) (react.AgentDefinition, bool) {
	def, ok := r.profiles[name]
	return def, ok
}

func (r *ProfileRegistry) List() []react.AgentDefinition {
	result := make([]react.AgentDefinition, 0, len(r.order))
	for _, name := range r.order {
		if def, ok := r.profiles[name]; ok {
			result = append(result, def)
		}
	}
	return result
}

func (r *ProfileRegistry) Names() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

func defaultPolicies() react.AgentPolicies {
	return react.AgentPolicies{
		MaxIterations: 1000,
		AllowBash:     true,
		BashPermissionContext: &react.BashToolConfig{
			PermCtx: &builtin_tools.BashPermissionContext{
				Mode: builtin_tools.PermissionModeManual,
			},
		},
		EnableHistoryCompaction: true,
	}
}

var defaultToolNames = []string{"list_files", "read_file", "rg", "bash", "list_skills", "load_skills"}

func DefaultProfiles() []react.AgentDefinition {
	return []react.AgentDefinition{
		{
			Name:       "code-audit",
			Role:       "代码安全审计专家，擅长静态分析、漏洞模式识别和安全编码指导",
			Background: "精通多种编程语言和框架的安全漏洞模式。能够使用 Semgrep 进行自动化 SAST 扫描，结合人工分析给出精确的漏洞定位和修复建议。同时擅长依赖安全审计（SCA）和敏感信息检测。",
			SkillNames: []string{"semgrep-scan", "dependency-audit", "secret-detection"},
			ToolNames:  defaultToolNames,
			Policies:   defaultPolicies(),
		},
		{
			Name:       "pentest",
			Role:       "渗透测试专家，擅长信息收集、漏洞发现、漏洞利用和安全评估。核心能力为通过 agent-browser 控制浏览器进行 Web 安全测试",
			Background: "精通 Web 安全浏览器自动化测试，通过 agent-browser CLI 控制浏览器访问目标站点，主动探索页面结构、交互流程和 API 接口，捕获真实网络流量并进行深度安全分析。掌握 SQL 注入、XSS、IDOR、CORS、文件上传、JWT 等全面的 Web 安全检测技术。遵循 OWASP 测试指南和 PTES 标准。",
			SkillNames: []string{
				"agent-browser",
				"SQL注入-多策略综合检测",
				"越权访问-IDOR检测", "越权访问-垂直越权检测", "越权访问-未授权访问检测",
				"CORS-配置错误检测", "JWT-弱密钥与信息泄露检测",
				"文件上传-多策略综合检测",
				"认证安全综合检测",
				"通知滥用-邮箱短信轰炸检测",
				"隐私保护-敏感信息未脱敏检测",
				"注册机制-批量注册检测",
			},
			ToolNames: defaultToolNames,
			Policies:  defaultPolicies(),
		},
		{
			Name:       "host-defense",
			Role:       "主机安全防护专家，擅长安全基线检查、入侵检测、恶意软件分析和应急响应",
			Background: "精通 Linux/Windows 系统安全加固、入侵检测与响应、恶意软件分析。能够进行 CIS Benchmark 安全基线审计、多源日志关联分析、YARA 规则编写和 Rootkit 检测、应急响应全流程处置。",
			SkillNames: []string{"baseline-check", "intrusion-detection", "malware-detect", "emergency-response", "log-analysis"},
			ToolNames:  defaultToolNames,
			Policies:   defaultPolicies(),
		},
	}
}
