package builtin_tools

const (
	UpdateCurrentStepToolName = "update_current_step"
	UpdateTaskStatusToolName  = "update_task_status"
	TaskStatusQueryToolName   = "task_status"
	TaskPlannerToolName       = "task_planner"
	HumanConfirmToolName      = "human_confirm"
	SubmitFinalAnswerToolName = "submit_final_answer"
	SubmitIntentToolName      = "submit_intent"
	// SubmitResultToolName 是子 Agent 单循环（sub_agent）专属终止工具：子 Agent 调用它提交
	// 「结论 + 关键事实 + 产物路径指针」即收尾。由 RunSubAgentLoop 每轮 append schema 并拦截，
	// 不进普通工具注册表 / childDef.ToolNames（子 Agent 无「相位」概念，见 U4）。
	SubmitResultToolName = "submit_result"

	ListFilesToolName      = "list_files"
	ReadFileToolName       = "read_file"
	WriteToolName          = "write"
	EditToolName           = "edit"
	NotebookEditToolName   = "notebook_edit"
	RgToolName             = "rg"
	BashToolName           = "bash"
	PowerShellToolName     = "powershell"
	ListSkillsToolName     = "list_skills"
	EjectSkillToolName     = "eject_skill"
	SubAgentToolName       = "sub_agent"
	SubAgentStatusToolName = "sub_agent_status"
	AwaitSubAgentsToolName = "await_subagents"
	SkillToolName          = "skill"
)
