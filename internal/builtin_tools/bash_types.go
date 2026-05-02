package builtin_tools

import "encoding/json"

// PermissionMode 权限决策模式
type PermissionMode string

const (
	PermissionModeYOLO   PermissionMode = "YOLO"
	PermissionModeManual PermissionMode = "MANUAL"
	PermissionModeAI     PermissionMode = "AI"
)

// RiskLevel 模型声明的风险等级
type RiskLevel string

const (
	RiskLevelLow       RiskLevel = "low"
	RiskLevelHigh      RiskLevel = "high"
	RiskLevelUncertain RiskLevel = "uncertain"
)

// GrantType 授权类型
type GrantType string

const (
	GrantTypeAuto                GrantType = "auto"
	GrantTypeAllowOnce           GrantType = "allow_once"
	GrantTypeAllowSession        GrantType = "allow_session"
	GrantTypeAllowRule           GrantType = "allow_rule"
	GrantTypeReject              GrantType = "reject"
	GrantTypePendingConfirmation GrantType = "pending_confirmation"
)

// ShellFamily shell 家族
type ShellFamily string

const (
	ShellFamilyPosix      ShellFamily = "posix"
	ShellFamilyPowerShell ShellFamily = "powershell"
	ShellFamilyCmd        ShellFamily = "cmd"
)

// AllowlistRuleKind 规则类型
type AllowlistRuleKind string

const (
	AllowlistRuleExact  AllowlistRuleKind = "exact"
	AllowlistRulePrefix AllowlistRuleKind = "prefix"
)

// AllowlistRule allowlist 规则结构体
type AllowlistRule struct {
	Tool        string            `json:"tool"`
	ShellFamily ShellFamily       `json:"shell_family"`
	Kind        AllowlistRuleKind `json:"kind"`
	Pattern     string            `json:"pattern"`
}

// BashToolInput bash 工具输入参数
type BashToolInput struct {
	Command     string `json:"command"`
	Cwd         string `json:"cwd,omitempty"`
	TimeoutMs   int64  `json:"timeout_ms,omitempty"`
	Description string `json:"description,omitempty"`
	Risk        string `json:"risk"`
	Reason      string `json:"reason"`
}

// BashToolOutput bash 工具结构化输出
type BashToolOutput struct {
	Executed                 bool           `json:"executed"`
	Stdout                   string         `json:"stdout"`
	Stderr                   string         `json:"stderr"`
	ExitCode                 int            `json:"exit_code"`
	Interrupted              bool           `json:"interrupted"`
	Shell                    string         `json:"shell"`
	ShellFamily              ShellFamily    `json:"shell_family"`
	Cwd                      string         `json:"cwd"`
	PermissionMode           PermissionMode `json:"permission_mode"`
	RiskLevel                RiskLevel      `json:"risk_level"`
	RiskReasons              []string       `json:"risk_reasons"`
	RequiresUserConfirmation bool           `json:"requires_user_confirmation"`
	GrantType                GrantType      `json:"grant_type"`
	MatchedRule              *AllowlistRule `json:"matched_rule"`
	Truncated                bool           `json:"truncated"`
	ReturnCodeInterpretation string         `json:"return_code_interpretation"`
}

// JSON 序列化为工具返回值
func (o *BashToolOutput) JSON() string {
	b, _ := json.Marshal(o)
	return string(b)
}

// BashPermissionContext 权限决策上下文，从 session 传入
type BashPermissionContext struct {
	Mode        PermissionMode
	ProjectPath string
}
