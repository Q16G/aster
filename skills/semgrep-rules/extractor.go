package semgrep_rules

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	extractOnce sync.Once
	extractDir  string
	extractErr  error
)

func ExtractRulesDir() (string, error) {
	extractOnce.Do(func() {
		extractDir, extractErr = doExtract()
	})
	return extractDir, extractErr
}

func doExtract() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	targetDir := filepath.Join(home, ".aster", "rules")

	hash, err := contentHash()
	if err != nil {
		return "", fmt.Errorf("compute content hash: %w", err)
	}

	marker := filepath.Join(targetDir, ".hash")
	if existing, err := os.ReadFile(marker); err == nil && string(existing) == hash {
		return targetDir, nil
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create rules dir: %w", err)
	}

	err = fs.WalkDir(EmbeddedRules, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(targetDir, path), 0o755)
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, readErr := fs.ReadFile(EmbeddedRules, path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		return os.WriteFile(filepath.Join(targetDir, path), data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("extract rules: %w", err)
	}

	_ = os.WriteFile(marker, []byte(hash), 0o644)
	return targetDir, nil
}

func contentHash() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(EmbeddedRules, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, readErr := fs.ReadFile(EmbeddedRules, path)
		if readErr != nil {
			return readErr
		}
		h.Write([]byte(path))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16], nil
}
