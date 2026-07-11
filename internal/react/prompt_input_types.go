package react

import "strings"

// AgentProfile 是各 phase Input 共享的身份三要素之 role/background（instruction 属 identity_env
// 块，不在此）。嵌入各 Input 结构体，消除 AgentRole/AgentBackground 的跨结构体重复，并以方法
// 替代成对的 HAS_AGENT_ROLE/HAS_AGENT_BACKGROUND 布尔。字段名沿用 AgentRole/AgentBackground
// 以经字段提升保持 Build 方法访问不变。
type AgentProfile struct {
	AgentRole       string
	AgentBackground string
}

func (p AgentProfile) HasRole() bool       { return strings.TrimSpace(p.AgentRole) != "" }
func (p AgentProfile) HasBackground() bool { return strings.TrimSpace(p.AgentBackground) != "" }

// CapabilityIndex 是能力索引三件套（Skills / MCP / 可用工具）的共享投影，嵌入需要能力索引的
// phase Input。以方法替代成对的 HasSkillsTable/HasMCPTable/HasAvailableTools/HasInjectedSkills
// 布尔，方法体与原调用点计算逻辑逐字等价。
type CapabilityIndex struct {
	SkillsContext      *SkillsPromptContext
	MCPContext         *MCPPromptContext
	AvailableTools     []AvailableToolInfo
	SkillsOverflowPath string
	MCPOverflowPath    string
}

func (c CapabilityIndex) HasSkillsTable() bool    { return c.SkillsContext != nil && c.SkillsContext.HasTable() }
func (c CapabilityIndex) HasMCPTable() bool       { return c.MCPContext != nil && c.MCPContext.HasTable() }
func (c CapabilityIndex) HasAvailableTools() bool { return len(c.AvailableTools) > 0 }
func (c CapabilityIndex) HasInjectedSkills() bool { return c.SkillsContext != nil && c.SkillsContext.HasInjected() }

// RunFlags 是单 run 稳定的执行形态标志，嵌入需要它们的 phase Input（各 phase 按需消费子集）。
type RunFlags struct {
	IsSubAgent       bool
	CanSpawnSubAgent bool
	SupportsVision   bool
}
