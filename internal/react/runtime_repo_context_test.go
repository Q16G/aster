package react

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

func TestDetectRuntimeRepoContext_NonGitDirGraceful(t *testing.T) {
	dir := t.TempDir()
	ctx := detectRuntimeRepoContext(context.Background(), dir)
	if got := ctx.SourceWorkingDir; got != dir {
		t.Fatalf("expected source working dir %q, got %q", dir, got)
	}
	if ctx.IsGitRepo {
		t.Fatalf("expected non-git temp dir to report IsGitRepo=false, got %+v", ctx)
	}
	if ctx.RepoRootDir != "" || ctx.Branch != "" || ctx.RemoteURL != "" || ctx.IsWorktree {
		t.Fatalf("expected empty git metadata for non-git dir, got %+v", ctx)
	}
}

func TestDetectRuntimeRepoContext_GitRepo(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "repo@test.local")
	runGit(t, repo, "config", "user.name", "repo-test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("repo\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	runGit(t, repo, "remote", "add", "origin", "git@github.com:example/project.git")

	ctx := detectRuntimeRepoContext(context.Background(), repo)
	if !ctx.IsGitRepo {
		t.Fatalf("expected git repo, got %+v", ctx)
	}
	if !samePath(t, ctx.RepoRootDir, repo) {
		t.Fatalf("expected repo root to resolve to %q, got %q", repo, ctx.RepoRootDir)
	}
	if ctx.Branch != "main" {
		t.Fatalf("expected branch main, got %q", ctx.Branch)
	}
	if ctx.RemoteURL != "git@github.com:example/project.git" {
		t.Fatalf("unexpected remote url: %q", ctx.RemoteURL)
	}
	if ctx.IsWorktree {
		t.Fatalf("expected normal repo not to be worktree, got %+v", ctx)
	}
}

func TestDetectRuntimeRepoContext_LinkedWorktree(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "repo@test.local")
	runGit(t, repo, "config", "user.name", "repo-test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("repo\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	worktreeDir := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "feature/test", worktreeDir)

	ctx := detectRuntimeRepoContext(context.Background(), worktreeDir)
	if !ctx.IsGitRepo {
		t.Fatalf("expected worktree dir to be git repo, got %+v", ctx)
	}
	if !ctx.IsWorktree {
		t.Fatalf("expected linked worktree, got %+v", ctx)
	}
	if ctx.Branch != "feature/test" {
		t.Fatalf("expected feature/test branch, got %q", ctx.Branch)
	}
	if !samePath(t, ctx.RepoRootDir, worktreeDir) {
		t.Fatalf("expected repo root to resolve to current worktree dir %q, got %q", worktreeDir, ctx.RepoRootDir)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res := builtin_tools.RunCommandLimited(context.Background(), dir, "git", args, 64*1024, 64*1024, 0)
	if res == nil || res.RunErr != nil || res.ExitCode != 0 {
		t.Fatalf("git %s failed: exit=%d err=%v stderr=%s", strings.Join(args, " "), res.ExitCode, res.RunErr, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout)
}

func samePath(t *testing.T, got, want string) bool {
	t.Helper()
	gotPath, err := filepath.EvalSymlinks(got)
	if err != nil {
		gotPath = filepath.Clean(got)
	}
	wantPath, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantPath = filepath.Clean(want)
	}
	return gotPath == wantPath
}
