package builtin_tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var defaultIgnoredDirNames = map[string]struct{}{
	".git":           {},
	".idea":          {},
	".vscode":        {},
	".gocache":       {},
	".gomodcache":    {},
	".gopath":        {},
	".gotmp":         {},
	".gotmp_local":   {},
	".gocache_local": {},
	"node_modules":   {},
	"vendor":         {},
	"dist":           {},
	"build":          {},
	"target":         {},
}

func expandHomePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func resolveAbsoluteToolPath(path string) (string, error) {
	path = expandHomePath(path)
	path = strings.TrimSpace(path)
	if path == "" || path == "<nil>" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be an absolute path")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "." || part == ".." {
			return "", fmt.Errorf("path must not contain '.' or '..' segments")
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return filepath.Clean(abs), nil
}
