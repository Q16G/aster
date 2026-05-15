package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialStore_SetGetDelete(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "creds.yaml")
	store := NewCredentialStore(tmp)

	if got := store.Get("openai"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	if err := store.Set("openai", "sk-test-123"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if got := store.Get("openai"); got != "sk-test-123" {
		t.Fatalf("expected sk-test-123, got %q", got)
	}

	store2 := NewCredentialStore(tmp)
	if got := store2.Get("openai"); got != "sk-test-123" {
		t.Fatalf("expected persisted value, got %q", got)
	}

	if err := store2.Delete("openai"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if got := store2.Get("openai"); got != "" {
		t.Fatalf("expected empty after delete, got %q", got)
	}
}

func TestCredentialStore_FilePermissions(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "subdir", "creds.yaml")
	store := NewCredentialStore(tmp)
	if err := store.Set("test", "value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %04o", perm)
	}
}

func TestCredentialStore_MissingFile(t *testing.T) {
	store := NewCredentialStore("/nonexistent/path/creds.yaml")
	if got := store.Get("any"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
