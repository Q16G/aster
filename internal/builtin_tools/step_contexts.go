package builtin_tools

import (
	"path/filepath"
	"strings"
)

// step_contexts.jsonl 的读写实现（AppendWorkspaceStepContextRecords /
// LoadWorkspaceStepContextRecords）在 react 包（M5b 迁移），本文件只保留
// namespace 归一与 artifact 路径纯函数（工具侧契约）。

func NormalizeWorkspaceNamespace(namespace string) string {
	namespace = filepath.ToSlash(strings.TrimSpace(namespace))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		return "root"
	}
	return namespace
}

func WorkspaceArtifactPath(workspaceRootDir string, filePath string) string {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return ""
	}
	localPath := filepath.Clean(filepath.FromSlash(filePath))
	if filepath.IsAbs(localPath) {
		return filepath.ToSlash(localPath)
	}
	if workspaceRootDir == "" {
		return filepath.ToSlash(localPath)
	}
	return filepath.ToSlash(filepath.Join(workspaceRootDir, localPath))
}
