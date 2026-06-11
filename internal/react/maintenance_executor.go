package react

import (
	"fmt"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
)

const (
	openItemsArchiveFileName = "open_items_archive.md"
	openItemsArchiveHeading  = "## 已闭环"
	taskContextFileName      = "task_context.md"
)

// executeMaintenanceDirectives 机械执行 step_replan 输出的落盘维护指令（见 step_replan 账本复核与维护段）：
// 时机在 step_replan 返回后、进入下一节点之前，should_replan=false 同样执行，保证
// 下游（planner / final_answer）读到的共享区是最新状态。单条失败不阻塞，返回
// warnings 由调用方注入下游。merge_staging 的语义归并无法机械完成，降级为下游提示。
func (a *Agent) executeMaintenanceDirectives(stepID string, directives []*builtin_tools.MaintenanceDirective) []string {
	if a == nil || a.workspaceRuntime == nil || len(directives) == 0 {
		return nil
	}
	sharedDir := strings.TrimSpace(a.workspaceRuntime.SharedDir())
	if sharedDir == "" {
		return nil
	}
	ledgerPath := filepath.Join(sharedDir, openItemsFileName)
	archivePath := filepath.Join(sharedDir, openItemsArchiveFileName)
	contextPath := filepath.Join(sharedDir, taskContextFileName)

	var warnings []string
	warn := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	executed := 0

	for _, d := range directives {
		if d == nil {
			continue
		}
		target := strings.TrimSpace(d.Target)
		content := strings.TrimSpace(d.Content)
		evidence := strings.TrimSpace(d.Evidence)

		switch strings.TrimSpace(d.Type) {
		case builtin_tools.MaintenanceDirectiveLedgerAdd:
			if content == "" {
				warn("维护指令 ledger_add 缺 content，已跳过")
				continue
			}
			id := target
			if id == "" {
				allocated, err := allocateLedgerID(ledgerPath)
				if err != nil {
					warn("维护指令 ledger_add 取号失败: %v", err)
					continue
				}
				id = allocated
			}
			line := fmt.Sprintf("- [%s] %s（来源: %s", id, content, stepID)
			if evidence != "" {
				line += "，证据: " + evidence
			}
			line += "）"
			if err := appendToMarkdownSection(ledgerPath, openItemsUnresolvedHeading, line); err != nil {
				warn("维护指令 ledger_add [%s] 写入失败: %v", id, err)
				continue
			}
			executed++

		case builtin_tools.MaintenanceDirectiveLedgerUpdate:
			if target == "" || content == "" {
				warn("维护指令 ledger_update 缺 target/content，已跳过")
				continue
			}
			ok, err := annotateLedgerLine(ledgerPath, target, content)
			if err != nil {
				warn("维护指令 ledger_update [%s] 失败: %v", target, err)
				continue
			}
			if !ok {
				warn("维护指令 ledger_update 未找到账本条目 [%s]", target)
				continue
			}
			executed++

		case builtin_tools.MaintenanceDirectiveArchiveItem:
			if target == "" {
				warn("维护指令 archive_item 缺 target（OI-id），已跳过")
				continue
			}
			removed, found, err := removeLedgerLine(ledgerPath, target)
			if err != nil {
				warn("维护指令 archive_item [%s] 失败: %v", target, err)
				continue
			}
			if !found {
				warn("维护指令 archive_item 未找到账本条目 [%s]", target)
				continue
			}
			block := removed
			if evidence != "" {
				block += "\n  闭环证据: " + evidence
			}
			if err := appendToMarkdownSection(archivePath, openItemsArchiveHeading, block); err != nil {
				// 归档失败回滚：条目放回未解决区，避免丢失。
				_ = appendToMarkdownSection(ledgerPath, openItemsUnresolvedHeading, removed)
				warn("维护指令 archive_item [%s] 归档写入失败（已回滚）: %v", target, err)
				continue
			}
			executed++

		case builtin_tools.MaintenanceDirectiveContextBake:
			if content == "" {
				warn("维护指令 context_bake 缺 content，已跳过")
				continue
			}
			heading := target
			if heading == "" {
				heading = taskContextRuntimeAddHeading
			}
			line := "- " + content
			if evidence != "" {
				line += "（证据: " + evidence + "）"
			}
			if err := appendToMarkdownSection(contextPath, heading, line); err != nil {
				warn("维护指令 context_bake 写入失败: %v", err)
				continue
			}
			executed++

		case builtin_tools.MaintenanceDirectiveMergeStaging:
			// 语义归并（取号/去重/裁剪）无法机械完成，按设计降级为下游 think_act 前置任务。
			warn("暂存区待归并：%s（语义归并由下一 step 收尾完成）", firstNonEmpty(content, "「## 待复核（子agent）」存在未归并条目"))

		default:
			warn("未知维护指令类型 %q，已跳过", d.Type)
		}
	}

	if executed > 0 || len(warnings) > 0 {
		runtimelog.LogJSON("info", map[string]any{
			"event":    "maintenance_directives_executed",
			"step_id":  stepID,
			"executed": executed,
			"warnings": len(warnings),
		})
	}
	return warnings
}
