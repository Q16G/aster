package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func stepFileExists(sharedDir, stepID string) bool {
	abs := stepFileAbs(sharedDir, stepID)
	if abs == "" {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && info.Size() > 0
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
func ensureStepFileScaffold(sharedDir, stepID, stepTitle string) error {
	abs := stepFileAbs(sharedDir, stepID)
	if abs == "" {
		return nil
	}
	if _, err := os.Stat(abs); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(stepFileScaffold(stepID, stepTitle)), 0o644)
}
