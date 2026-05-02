package service

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	skillspkg "aster/skills"
)

func (s *SkillService) ImportSkillsFromFS(ctx context.Context, root fs.FS) (int, error) {
	return s.ImportSkillsFromFSWithSource(ctx, root, "", "")
}

func (s *SkillService) ImportSkillsFromFSWithSource(ctx context.Context, root fs.FS, source string, baseDir string) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("skill service is nil")
	}
	if root == nil {
		return 0, fmt.Errorf("skills fs is nil")
	}

	paths := make([]string, 0, 32)
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d == nil || d.IsDir() {
			return nil
		}
		slashPath := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
		if strings.HasSuffix(strings.ToLower(slashPath), "/skill.md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	sort.Strings(paths)

	imported := 0
	for _, path := range paths {
		raw, readErr := fs.ReadFile(root, path)
		if readErr != nil {
			return imported, fmt.Errorf("read skill %s failed: %w", path, readErr)
		}
		skill, parseErr := ParseSkillMarkdown(string(raw))
		if parseErr != nil {
			return imported, fmt.Errorf("parse skill %s failed: %w", path, parseErr)
		}

		if source != "" {
			skill.Source = source
		}
		if skill.Name == "" {
			skill.Name = inferSkillNameFromPath(path)
		}
		if baseDir != "" {
			skill.SkillDir = filepath.Join(baseDir, filepath.Dir(path))
		}

		if err := s.ImportSkill(ctx, skill); err != nil {
			return imported, fmt.Errorf("import skill %s failed: %w", skill.Name, err)
		}
		imported++
	}
	return imported, nil
}

func (s *SkillService) ImportSkillsFromDir(ctx context.Context, dir string) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return 0, fmt.Errorf("skills dir is empty")
	}
	absDir, _ := filepath.Abs(dir)
	return s.ImportSkillsFromFSWithSource(ctx, os.DirFS(filepath.Clean(dir)), "", absDir)
}

func (s *SkillService) ImportSkillsFromDirWithSource(ctx context.Context, dir string, source string) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return 0, fmt.Errorf("skills dir is empty")
	}
	absDir, _ := filepath.Abs(dir)
	return s.ImportSkillsFromFSWithSource(ctx, os.DirFS(filepath.Clean(dir)), source, absDir)
}

func (s *SkillService) ImportEmbeddedSkills(ctx context.Context) (int, error) {
	return s.ImportSkillsFromFSWithSource(ctx, skillspkg.EmbeddedSkills, "builtin", "")
}

func (s *SkillService) ImportSkillsFromMultipleSources(ctx context.Context, workspaceRoot string, homeDir string) (int, error) {
	total := 0

	n, err := s.ImportEmbeddedSkills(ctx)
	if err != nil {
		return total, fmt.Errorf("import builtin skills: %w", err)
	}
	total += n

	if homeDir != "" {
		userSkillsDir := filepath.Join(homeDir, ".agent", "skills")
		if info, statErr := os.Stat(userSkillsDir); statErr == nil && info.IsDir() {
			n, err = s.ImportSkillsFromDirWithSource(ctx, userSkillsDir, "user")
			if err != nil {
				return total, fmt.Errorf("import user skills: %w", err)
			}
			total += n
		}
	}

	if workspaceRoot != "" {
		projectSkillsDir := filepath.Join(workspaceRoot, ".agent", "skills")
		if info, statErr := os.Stat(projectSkillsDir); statErr == nil && info.IsDir() {
			n, err = s.ImportSkillsFromDirWithSource(ctx, projectSkillsDir, "project")
			if err != nil {
				return total, fmt.Errorf("import project skills: %w", err)
			}
			total += n
		}
	}

	return total, nil
}

func inferSkillNameFromPath(path string) string {
	slashPath := strings.ReplaceAll(path, "\\", "/")
	dir := filepath.Dir(slashPath)
	parts := strings.Split(dir, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		name := strings.TrimSpace(parts[i])
		if name != "" && name != "." {
			return name
		}
	}
	return ""
}
