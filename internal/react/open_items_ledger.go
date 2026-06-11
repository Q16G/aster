package react

// 共享区文件名常量。账本/事实板的"两区 + 归档/补充"节内由 think_act / planning 阶段
// AI 直接维护（包括子 Agent 产出归并，见 think_act_system.prompt 子 Agent 委派段的
// "产出归并"原则）；runtime 不再写共享区，仅持有文件名以供 prompt 注入与按需读取。
const (
	openItemsFileName        = "open_items.md"
	openItemsArchiveFileName = "open_items_archive.md"
	taskContextFileName      = "task_context.md"
)
