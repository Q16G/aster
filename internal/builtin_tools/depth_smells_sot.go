package builtin_tools

// DepthSmellsEnumeration 是维度②（depth_gaps / 轴②）深度气味判据的单一真相源。
// schema description 与 planning_system.prompt 维度② 段均从此常量渲染，
// 避免同一组判据在 prompt 与 schema 两侧手工同步漂移。
//
// 修改本常量等同修改 axis② 判据语义，需评估 step_replan / final_answer 两侧行为。
const DepthSmellsEnumeration = "结论停在半路（shallow_only / provisional / 需确认未落定）；分析链条断裂（定位到终点未回溯触发点，每对\"触发点—终点\"独立成行）；悬而未决判断未给定论；低价值项占位挤掉高价值分析；抽样冒充全量；产出推进未承接（本 step 确认的产出让此前未触达的下游对象 / 入口变可达，未由任何 pending 步骤承接深化）；确认型发现未按角色职责链推深（确认入口 / 漏洞 / 线索 / 可达性后，未按 agent role 职责语义推进到更深决策层——如利用 / 影响评估 / 根因 / 验证等角色相关深度终点，停在浅层认定）"
