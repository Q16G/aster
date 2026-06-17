package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/golden/* with current outputs")

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name)
}

// diffGolden compares got against the golden file at name. When -update-golden
// is set on the test invocation, the golden file is rewritten with got and the
// comparison is skipped.
//
// Bytes are compared verbatim; callers must normalize trailing whitespace or
// line endings themselves if the producer is non-deterministic.
func diffGolden(t *testing.T, name string, got string) {
	t.Helper()
	path := goldenPath(name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-golden to create)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s\n--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s\n--- end ---\n(run with -update-golden to refresh)",
			name, len(want), string(want), len(got), got)
	}
}
