package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
)

// step 过程文件：shared 目录下扁平的 step_<step_id>.md，由 runtime 在 step 入口
// 预创建骨架（路径 100% 稳定），think_act 按系统 prompt 的三节契约每轮维护。

func stepFileName(stepID string) string {
	return "step_" + strings.TrimSpace(stepID) + ".md"
}

func stepFileRelPath(stepID string) string {
	return "shared/" + stepFileName(stepID)
}

func stepFileAbs(sharedDir, stepID string) string {
	if strings.TrimSpace(sharedDir) == "" || strings.TrimSpace(stepID) == "" {
		return ""
	}
	return filepath.Join(sharedDir, stepFileName(stepID))
}

// legacyStepFileExists 检查旧布局 shared/<stepID>/step.md（老 session resume 兼容）。
func legacyStepFileExists(sharedDir, stepID string) bool {
	return stepSharedFileExists(sharedDir, stepID, "step.md")
}

// stepFileExists 判定新布局过程文件是否存在——只看是否能 Stat，不看大小。
// AI 中途经 bash 把过程文件短暂写为 0 字节（heredoc rewrite / mv 替换 / write 失败重试）
// 仍应被认作"存在"，否则 readSharedStepFileForPrompt 会跌回 legacy 路径读到老 session
// 残留的 shared/<stepID>/step.md。legacyStepFileExists 保留 size>0（旧文件价值在内容）。
func stepFileExists(sharedDir, stepID string) bool {
	abs := stepFileAbs(sharedDir, stepID)
	if abs == "" {
		return false
	}
	_, err := os.Stat(abs)
	return err == nil
}

func stepFileScaffold(stepID, stepTitle string) string {
	title := strings.TrimSpace(stepTitle)
	if title == "" {
		title = strings.TrimSpace(stepID)
	}
	return fmt.Sprintf(`# step_%s: %s

## 子步骤清单

## 进展记录

## 收尾产出
`, strings.TrimSpace(stepID), title)
}

// ensureStepFileScaffold 在 step 入口预创建过程文件骨架。仅当文件不存在时写入，
// 已存在则原样跳过（保护 resume / replan 重入同 step 的既有进展）。
// 写入经 WorkspaceRuntime.WriteFileRel 完成——其内置 resolveAbsPath 根逃逸防护，
// stepID 含 ".." 或 "/" 等异常字符时拒绝写出，避免骨架落在 sharedDir 之外。
func ensureStepFileScaffold(rt builtin_tools.WorkspaceRuntime, stepID, stepTitle string) error {
	if rt == nil {
		return nil
	}
	sharedDir := rt.SharedDir()
	if stepFileAbs(sharedDir, stepID) == "" {
		return nil
	}
	if stepFileExists(sharedDir, stepID) {
		return nil
	}
	return rt.WriteFileRel(stepFileRelPath(stepID), []byte(stepFileScaffold(stepID, stepTitle)))
}
