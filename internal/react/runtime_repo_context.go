package react

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
)

type RuntimeRepoContext struct {
	SourceWorkingDir string
	RepoRootDir      string
	IsGitRepo        bool
	Branch           string
	RemoteURL        string
	IsWorktree       bool
}

func normalizeSourceWorkingDir(dir string) string {
	return normalizeWorkspaceRootDir(dir)
}

func detectRuntimeRepoContext(ctx context.Context, sourceWorkingDir string) RuntimeRepoContext {
	repoCtx := RuntimeRepoContext{
		SourceWorkingDir: normalizeSourceWorkingDir(sourceWorkingDir),
	}
	if strings.TrimSpace(repoCtx.SourceWorkingDir) == "" {
		return repoCtx
	}
	if _, err := exec.LookPath("git"); err != nil {
		return repoCtx
	}

	gitOut := func(args ...string) string {
		res := builtin_tools.RunCommandLimited(ctx, repoCtx.SourceWorkingDir, "git", args, 32*1024, 32*1024, 0)
		if res == nil || res.RunErr != nil || res.ExitCode != 0 {
			return ""
		}
		return strings.TrimSpace(res.Stdout)
	}

	repoRoot := normalizeWorkspaceRootDir(gitOut("rev-parse", "--show-toplevel"))
	if repoRoot == "" {
		return repoCtx
	}
	repoCtx.IsGitRepo = true
	repoCtx.RepoRootDir = repoRoot

	branch := strings.TrimSpace(gitOut("branch", "--show-current"))
	if branch == "" {
		head := strings.TrimSpace(gitOut("rev-parse", "--short", "HEAD"))
		if head != "" {
			branch = "detached@" + head
		}
	}
	repoCtx.Branch = branch
	repoCtx.RemoteURL = strings.TrimSpace(gitOut("config", "--get", "remote.origin.url"))

	gitDir := strings.TrimSpace(gitOut("rev-parse", "--git-dir"))
	commonDir := strings.TrimSpace(gitOut("rev-parse", "--git-common-dir"))
	if gitDir != "" && commonDir != "" {
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(repoCtx.SourceWorkingDir, filepath.FromSlash(gitDir))
		}
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(repoCtx.SourceWorkingDir, filepath.FromSlash(commonDir))
		}
		repoCtx.IsWorktree = normalizeWorkspaceRootDir(gitDir) != normalizeWorkspaceRootDir(commonDir)
	}

	return repoCtx
}
