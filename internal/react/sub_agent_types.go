package react

import "strings"

// SubAgentType 是子 Agent 的类型。用户决策（2026-07-12 · U1）：类型**只有描述上的差异**——
// 不差异化挂载工具、不设 per-type 超时、不设 per-type 迭代上限、不选 per-type 模型。所有子 Agent
// 是机制完全相同的单循环实例（同一套工具、同一循环、由 parent ctx + submit_result 收尾兜底），
// 唯一区别是注入 prompt `## 本次委派类型` 的这段描述文本。
type SubAgentType struct {
	Name              string
	SystemPromptExtra string
}

const (
	subAgentTypeExplore        = "explore"
	subAgentTypeGeneralPurpose = "general-purpose"
)

// exploreTypeExtra / generalPurposeTypeExtra 是两个内置类型的描述文本。explore 的「只读、不改
// 被审对象」纯靠这段描述约束（工具集与 general-purpose 相同，见 U1）——即「类型只差在描述」。
const exploreTypeExtra = "你是检索取证型子 Agent：只做只读的查找、定位与取证——读文件、搜索代码、列目录、顺线索回读原文，把「找到了什么、在哪里（绝对路径 / 符号 / 行号）、具体值是什么」作为结论返回。除把过程产物落进自己的子工作区草稿目录外，不对被审对象做任何改动型动作，不产出实现方案或建议，不对未观测的部分外推。达成检索目标即收尾。"

const generalPurposeTypeExtra = "你是通用执行型子 Agent：可在注入的角色与指令许可范围内完成需要动手的委派——查找、分析、必要的改动型动作与验证。以「做了什么、结果的具体值、产物的绝对路径」作为结论返回；改动型动作前遵循角色边界，未获许可的改动只观测不执行。"

var builtinSubAgentTypes = map[string]SubAgentType{
	subAgentTypeExplore:        {Name: subAgentTypeExplore, SystemPromptExtra: exploreTypeExtra},
	subAgentTypeGeneralPurpose: {Name: subAgentTypeGeneralPurpose, SystemPromptExtra: generalPurposeTypeExtra},
}

// resolveSubAgentType 解析子 Agent 类型；未知/空名回退 general-purpose。
func resolveSubAgentType(name string) SubAgentType {
	if spec, ok := builtinSubAgentTypes[strings.TrimSpace(name)]; ok {
		return spec
	}
	return builtinSubAgentTypes[subAgentTypeGeneralPurpose]
}
