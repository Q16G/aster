package skills

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	skillExtractOnce sync.Once
	skillExtractDir  string
	skillExtractErr  error
)

func ExtractSkillsDir() (string, error) {
	skillExtractOnce.Do(func() {
		skillExtractDir, skillExtractErr = doExtractSkills()
	})
	return skillExtractDir, skillExtractErr
}

func doExtractSkills() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	targetDir := filepath.Join(home, ".aster", "skills")

	hash, err := skillsContentHash()
	if err != nil {
		return "", fmt.Errorf("compute skills hash: %w", err)
	}

	marker := filepath.Join(targetDir, ".hash")
	if existing, err := os.ReadFile(marker); err == nil && string(existing) == hash {
		return targetDir, nil
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create skills dir: %w", err)
	}

	newPaths := collectEmbeddedPaths()
	cleanStaleFiles(targetDir, newPaths)

	err = fs.WalkDir(EmbeddedSkills, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(targetDir, path), 0o755)
		}
		if !strings.HasSuffix(strings.ToLower(path), "skill.md") {
			return nil
		}
		data, readErr := fs.ReadFile(EmbeddedSkills, path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		return os.WriteFile(filepath.Join(targetDir, path), data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("extract skills: %w", err)
	}

	writeManifest(filepath.Join(targetDir, ".manifest"), newPaths)
	_ = os.WriteFile(marker, []byte(hash), 0o644)
	return targetDir, nil
}

func collectEmbeddedPaths() map[string]struct{} {
	paths := make(map[string]struct{})
	_ = fs.WalkDir(EmbeddedSkills, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(strings.ToLower(path), "skill.md") {
			paths[filepath.ToSlash(path)] = struct{}{}
		}
		return nil
	})
	return paths
}

func cleanStaleFiles(targetDir string, newPaths map[string]struct{}) {
	manifestPath := filepath.Join(targetDir, ".manifest")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		rel := strings.TrimSpace(line)
		if rel == "" {
			continue
		}
		if _, ok := newPaths[rel]; ok {
			continue
		}
		abs := filepath.Join(targetDir, filepath.FromSlash(rel))
		_ = os.Remove(abs)
		_ = os.Remove(filepath.Dir(abs))
	}
}

func writeManifest(path string, paths map[string]struct{}) {
	lines := make([]string, 0, len(paths))
	for p := range paths {
		lines = append(lines, p)
	}
	sort.Strings(lines)
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func skillsContentHash() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(EmbeddedSkills, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(path), "skill.md") {
			return nil
		}
		data, readErr := fs.ReadFile(EmbeddedSkills, path)
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
