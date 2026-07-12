package react

import "testing"

// TestAgentProfile_HasMethods 验证 role/background/instruction gate 方法与旧内联 `TrimSpace(x) != ""` 等价。
func TestAgentProfile_HasMethods(t *testing.T) {
	if (AgentProfile{}).HasRole() {
		t.Error("空 AgentRole 应 HasRole()=false")
	}
	if (AgentProfile{AgentRole: "   "}).HasRole() {
		t.Error("空白 AgentRole 应视为空")
	}
	if !(AgentProfile{AgentRole: "审计专家"}).HasRole() {
		t.Error("非空 AgentRole 应 HasRole()=true")
	}
	if (AgentProfile{}).HasBackground() {
		t.Error("空 AgentBackground 应 HasBackground()=false")
	}
	if !(AgentProfile{AgentBackground: "10 年经验"}).HasBackground() {
		t.Error("非空 AgentBackground 应 HasBackground()=true")
	}
	if (AgentProfile{}).HasInstruction() {
		t.Error("空 AgentInstruction 应 HasInstruction()=false")
	}
	if (AgentProfile{AgentInstruction: "   "}).HasInstruction() {
		t.Error("空白 AgentInstruction 应视为空")
	}
	if !(AgentProfile{AgentInstruction: "输出简洁的分析结论"}).HasInstruction() {
		t.Error("非空 AgentInstruction 应 HasInstruction()=true")
	}
}

// TestCapabilityIndex_HasMethods 验证四个能力 gate 方法与旧调用点内联逻辑逐一等价：
// HasSkillsTable = Skills!=nil && Skills.HasTable()；HasMCPTable = MCP!=nil && MCP.HasTable()；
// HasInjectedSkills = Skills!=nil && Skills.HasInjected()；HasAvailableTools = len>0。
func TestCapabilityIndex_HasMethods(t *testing.T) {
	// nil / 空：全 false。
	var empty CapabilityIndex
	if empty.HasSkillsTable() || empty.HasMCPTable() || empty.HasInjectedSkills() || empty.HasAvailableTools() {
		t.Error("空 CapabilityIndex 四方法应全 false")
	}

	// 填充：全 true。
	full := CapabilityIndex{
		SkillsContext:  &SkillsPromptContext{Table: "| skill | when | status |", Injected: "#### s\nbody"},
		MCPContext:     &MCPPromptContext{Table: "| server |"},
		AvailableTools: []AvailableToolInfo{{Name: "read_file"}},
	}
	if !full.HasSkillsTable() || !full.HasMCPTable() || !full.HasInjectedSkills() || !full.HasAvailableTools() {
		t.Error("填充后四方法应全 true")
	}

	// 与旧内联逻辑逐一等价（锁死方法体不漂移）。
	cases := []CapabilityIndex{
		empty, full,
		{SkillsContext: &SkillsPromptContext{Table: "  "}},                 // 空白表 → false
		{SkillsContext: &SkillsPromptContext{Injected: "x"}},               // 有注入无表
		{MCPContext: &MCPPromptContext{Table: ""}},                        // 空 MCP 表
		{AvailableTools: []AvailableToolInfo{}},                            // 空工具切片
	}
	for i, c := range cases {
		if c.HasSkillsTable() != (c.SkillsContext != nil && c.SkillsContext.HasTable()) {
			t.Errorf("case %d: HasSkillsTable 与内联逻辑不等价", i)
		}
		if c.HasMCPTable() != (c.MCPContext != nil && c.MCPContext.HasTable()) {
			t.Errorf("case %d: HasMCPTable 与内联逻辑不等价", i)
		}
		if c.HasInjectedSkills() != (c.SkillsContext != nil && c.SkillsContext.HasInjected()) {
			t.Errorf("case %d: HasInjectedSkills 与内联逻辑不等价", i)
		}
		if c.HasAvailableTools() != (len(c.AvailableTools) > 0) {
			t.Errorf("case %d: HasAvailableTools 与内联逻辑不等价", i)
		}
	}
}
