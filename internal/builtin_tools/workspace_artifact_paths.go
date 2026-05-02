package builtin_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WorkspaceArtifactsRootRel(namespace string) string {
	ns := NormalizeWorkspaceNamespace(namespace)
	if ns == "" || ns == "root" {
		return "artifacts"
	}
	return filepath.ToSlash(filepath.Join("artifacts", ns))
}

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

	artifactPath = filepath.ToSlash(filepath.Join(WorkspaceArtifactsRootRel(namespace), cleanRel))
	absCandidate := filepath.Join(workspaceRootDir, filepath.FromSlash(artifactPath))
	absPath, err = filepath.Abs(absCandidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve artifact path failed: %w", err)
	}
	absPath = filepath.Clean(absPath)

	workspaceAbs, err := filepath.Abs(workspaceRootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root dir failed: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	relToRoot, err := filepath.Rel(workspaceAbs, absPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve artifact path rel failed: %w", err)
	}
	relToRoot = filepath.Clean(relToRoot)
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) || filepath.IsAbs(relToRoot) {
		return "", "", fmt.Errorf("artifact path escapes workspace root")
	}
	return artifactPath, filepath.ToSlash(absPath), nil
}
