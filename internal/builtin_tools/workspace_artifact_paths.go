package builtin_tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"aster/internal/workspacefs"
)

func WorkspaceArtifactWritePath(workspaceRootDir string, namespace string, relPath string) (artifactPath string, absPath string, err error) {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	if workspaceRootDir == "" {
		return "", "", fmt.Errorf("workspace root dir is empty")
	}

	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if relPath == "" {
		return "", "", fmt.Errorf("artifact path is required")
	}
	if strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "\\") {
		return "", "", fmt.Errorf("artifact path must be relative")
	}
	if filepath.IsAbs(filepath.FromSlash(relPath)) {
		return "", "", fmt.Errorf("artifact path must be relative")
	}

	cleanRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	if cleanRel == "." || cleanRel == "" {
		return "", "", fmt.Errorf("artifact path is required")
	}
	if cleanRel == ".." || strings.HasPrefix(cleanRel, "../") {
		return "", "", fmt.Errorf("artifact path must stay within workspace artifacts root")
	}
	if strings.Contains(cleanRel, "artifacts/agents/") && strings.Count(cleanRel, "artifacts/") > 1 {
		return "", "", fmt.Errorf("artifact path contains duplicated structural segments: %s", cleanRel)
	}

	artifactPath = filepath.ToSlash(filepath.Join(workspacefs.New("", namespace).ArtifactsRootRel(), cleanRel))
	// 逃逸校验收口 Store.AbsPath（rel→abs 的唯一出口，含 root 内双重防穿越校验）。
	store, err := workspacefs.NewLocalStore(workspaceRootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root dir failed: %w", err)
	}
	absPath, err = store.AbsPath(artifactPath)
	if err != nil {
		return "", "", err
	}
	return artifactPath, filepath.ToSlash(absPath), nil
}
