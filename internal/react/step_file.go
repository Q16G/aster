package react

import (
	"fmt"
	"os"
	"strings"

	"aster/internal/workspacefs"
)

// step 过程文件：shared 目录下扁平的 step_<step_id>.md，由 runtime 在 step 入口
// 预创建骨架（路径 100% 稳定），think_act 按系统 prompt 的三节契约每轮维护。
// 路径知识统一委托 workspacefs.Layout（rel 形态不依赖 Root）。

func stepFileRelPath(stepID string) string {
	return workspacefs.Layout{}.StepFileRel(stepID)
}

// statNonEmpty 判定路径存在且非空（size>0）；空路径直接判否。
func statNonEmpty(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// legacyStepFileExists 检查旧布局 shared/<stepID>/step.md（老 session resume 兼容）。
func legacyStepFileExists(l workspacefs.Layout, stepID string) bool {
	return statNonEmpty(l.LegacyStepFile(stepID))
}

// stepFileExists 判定新布局过程文件是否存在——只看是否能 Stat，不看大小。
// 写入工具中途把过程文件短暂写为 0 字节（rewrite / mv 替换 / 写入失败重试）
// 仍应被认作"存在"，否则 readSharedStepFileForPrompt 会跌回 legacy 路径读到老 session
// 残留的 shared/<stepID>/step.md。legacyStepFileExists 保留 size>0（旧文件价值在内容）。
func stepFileExists(l workspacefs.Layout, stepID string) bool {
	abs := l.StepFile(stepID)
	if abs == "" {
		return false
	}
	_, err := os.Stat(abs)
	return err == nil
}

// markdownSectionLines 返回 raw 中指定标题（整行精确匹配，如 "## 输入事实"）节内的
// 非空内容行（已 TrimSpace）；节区边界为下一个以 "#" 开头的标题行或文件尾。
func markdownSectionLines(raw, heading string) []string {
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return nil
	}
	var out []string
	inSection := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if inSection {
			if strings.HasPrefix(trimmed, "#") {
				break
			}
			if trimmed != "" {
				out = append(out, trimmed)
			}
			continue
		}
		if trimmed == heading {
			inSection = true
		}
	}
	return out
}

// taskContextInputFactsPresent 判定事实板 `## 输入事实` 节是否已有至少一条 `- ` 条目。
func taskContextInputFactsPresent(raw string) bool {
	for _, line := range markdownSectionLines(raw, "## 输入事实") {
		if strings.HasPrefix(line, "- ") && strings.TrimSpace(line[2:]) != "" {
			return true
		}
	}
	return false
}

// stepFileProgressPresent 判定 step 过程文件的 `## 进展记录` / `## 收尾产出`
// 至少一节已有内容（update_current_step completed 闸门判据）。
func stepFileProgressPresent(raw string) bool {
	return len(markdownSectionLines(raw, "## 进展记录")) > 0 ||
		len(markdownSectionLines(raw, "## 收尾产出")) > 0
}

func stepFileScaffold(stepID, stepTitle string) string {
	title := strings.TrimSpace(stepTitle)
	if title == "" {
		title = strings.TrimSpace(stepID)
	}
	return fmt.Sprintf(`# step_%s: %s

## 未覆盖待补（实时维护）

## 进展记录

## 收尾产出
`, strings.TrimSpace(stepID), title)
}

// maxStepFileGateRejections 是 step 过程文件闸门对同一 step 的最大拒绝次数：
// 闸门用于逼模型补写，不应阻断任务——超限降级放行并记 warning。
const maxStepFileGateRejections = 1

// stepFileGateMinToolCalls 是闸门生效的最小执行量（timeline tool_call 条数）：
// 短步（执行量小）允许卡片字段直接承载产出、过程文件可空；执行量达阈值的 step
// 过程文件仍为空才视为「实时维护」契约失守。
const stepFileGateMinToolCalls = 5

// checkStepFileProgress 是 update_current_step 的 completed 闸门：step 过程文件的
// `## 进展记录` / `## 收尾产出` 须至少一节非空（think_act「每轮执行结束时文件与实际
// 进度一致」契约的机械兜底）。workspaceRuntime 缺位或文件不存在时放行——不把基础
// 设施异常转嫁给模型；failed 提交不经此闸（调用方只在 completed 时调用）。
func (a *Agent) checkStepFileProgress(stepID string) error {
	if a == nil || a.workspaceRuntime == nil {
		return nil
	}
	l := a.wsLayout()
	abs := l.StepFile(stepID)
	if abs == "" {
		return nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	if stepFileProgressPresent(string(data)) {
		return nil
	}
	if stepTimelineToolCallCount(l, stepID, stepFileGateMinToolCalls) < stepFileGateMinToolCalls {
		return nil
	}
	a.stepFileGateMu.Lock()
	if a.stepFileGateRejections == nil {
		a.stepFileGateRejections = make(map[string]int)
	}
	a.stepFileGateRejections[stepID]++
	rejections := a.stepFileGateRejections[stepID]
	a.stepFileGateMu.Unlock()
	if rejections > maxStepFileGateRejections {
		a.emitRuntimeLog("warning", "step file progress still missing after gate rejection", a.state.Snapshot(), map[string]any{
			"event":   "step_file_progress_missing",
			"step_id": stepID,
		})
		return nil
	}
	return fmt.Errorf("step 过程文件 `## 进展记录` 与 `## 收尾产出` 均为空，与实际执行不符。请先把进展与收尾产出写入 %s 再提交 completed", abs)
}

// ensureStepFileScaffold 在 step 入口预创建过程文件骨架。仅当文件不存在时写入，
// 已存在则原样跳过（保护 resume / replan 重入同 step 的既有进展）。
// 写入经 WorkspaceRuntime.WriteFileRel 完成——其内置 resolveAbsPath 根逃逸防护，
// stepID 含 ".." 或 "/" 等异常字符时拒绝写出，避免骨架落在 sharedDir 之外。
func ensureStepFileScaffold(rt WorkspaceRuntime, stepID, stepTitle string) error {
	if rt == nil {
		return nil
	}
	l := workspacefs.New(rt.RootDir(), rt.Namespace())
	if l.StepFile(stepID) == "" {
		return nil
	}
	if stepFileExists(l, stepID) {
		return nil
	}
	return rt.WriteFileRel(stepFileRelPath(stepID), []byte(stepFileScaffold(stepID, stepTitle)))
}
