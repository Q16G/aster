package workspacefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	st, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return st
}

// S01-S03 Read 三态。
func TestStoreRead(t *testing.T) {
	st := newTestStore(t)
	if err := st.Write("a/b.txt", []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := st.Read("a/b.txt")
	if err != nil || string(data) != "hello" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	if _, err := st.Read("missing.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read(missing) err = %v, want fs.ErrNotExist", err)
	}
	if _, err := st.Read("a"); err == nil {
		t.Fatalf("Read(目录) 应报错")
	}
}

// S04 AbsPath 合法 rel。
func TestStoreAbsPathValid(t *testing.T) {
	st := newTestStore(t)
	abs, err := st.AbsPath("shared/task_context.md")
	if err != nil {
		t.Fatalf("AbsPath: %v", err)
	}
	want := filepath.Join(st.Root(), "shared", "task_context.md")
	if abs != want {
		t.Fatalf("AbsPath = %q, want %q", abs, want)
	}
}

// S05 AbsPath 防穿越表：前四类拒绝，后四类归一。
func TestStoreAbsPathTraversal(t *testing.T) {
	st := newTestStore(t)
	rejects := []string{"../x", "a/../../x", "/etc/passwd", ".", "", "   ", "..", "\\evil"}
	for _, rel := range rejects {
		if _, err := st.AbsPath(rel); err == nil {
			t.Errorf("AbsPath(%q) 应拒绝", rel)
		}
	}
	normalized := map[string]string{
		"./a":      "a",
		"a//b":     filepath.Join("a", "b"),
		"a/./b":    filepath.Join("a", "b"),
		"a/b/":     filepath.Join("a", "b"),
		"a/x/../b": filepath.Join("a", "b"), // 内部 .. 折叠后仍在 root 内 → 合法
	}
	for rel, wantTail := range normalized {
		abs, err := st.AbsPath(rel)
		if err != nil {
			t.Errorf("AbsPath(%q) 意外拒绝: %v", rel, err)
			continue
		}
		if want := filepath.Join(st.Root(), wantTail); abs != want {
			t.Errorf("AbsPath(%q) = %q, want %q", rel, abs, want)
		}
	}
}

// S07-S09 Write：深层建目录/覆盖/空内容。
func TestStoreWrite(t *testing.T) {
	st := newTestStore(t)
	if err := st.Write("x/y/z/deep.txt", []byte("v1")); err != nil {
		t.Fatalf("Write 深层: %v", err)
	}
	if err := st.Write("x/y/z/deep.txt", []byte("v2")); err != nil {
		t.Fatalf("Write 覆盖: %v", err)
	}
	data, _ := st.Read("x/y/z/deep.txt")
	if string(data) != "v2" {
		t.Fatalf("覆盖后 = %q", data)
	}
	if err := st.Write("empty.txt", nil); err != nil {
		t.Fatalf("Write 空: %v", err)
	}
	if info, err := st.Stat("empty.txt"); err != nil || info.Size() != 0 {
		t.Fatalf("空文件 stat: %v %v", info, err)
	}
}

// S10 WriteAtomic 完整性 + 无 tmp 残留。
func TestStoreWriteAtomic(t *testing.T) {
	st := newTestStore(t)
	if err := st.WriteAtomic("state/state.json", []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	data, err := st.Read("state/state.json")
	if err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("读回 = %q, %v", data, err)
	}
	entries, err := st.List("state")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("tmp 残留: %s", e.Name())
		}
	}
}

// S11 WriteAtomic 失败注入：父目录只读 → 报错且不产生半截文件。
func TestStoreWriteAtomicFailureCleanup(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("依赖 POSIX 权限语义")
	}
	st := newTestStore(t)
	if err := st.WriteAtomic("ro/target.json", []byte("old")); err != nil {
		t.Fatalf("预置: %v", err)
	}
	roDir := filepath.Join(st.Root(), "ro")
	if err := os.Chmod(roDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	if err := st.WriteAtomic("ro/target.json", []byte("new")); err == nil {
		t.Fatalf("只读目录写入应失败")
	}
	_ = os.Chmod(roDir, 0o755)
	data, err := st.Read("ro/target.json")
	if err != nil || string(data) != "old" {
		t.Fatalf("失败后原文件受损: %q, %v", data, err)
	}
	entries, _ := st.List("ro")
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("失败后 tmp 残留: %s", e.Name())
		}
	}
}

// S13-S15 Append：自动创建/保序/WithFsync。
func TestStoreAppend(t *testing.T) {
	st := newTestStore(t)
	if err := st.Append("logs/a.jsonl", []byte("line1\n")); err != nil {
		t.Fatalf("Append 新文件: %v", err)
	}
	if err := st.Append("logs/a.jsonl", []byte("line2\n")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := st.Append("logs/a.jsonl", []byte("line3\n"), WithFsync()); err != nil {
		t.Fatalf("Append fsync: %v", err)
	}
	data, _ := st.Read("logs/a.jsonl")
	if string(data) != "line1\nline2\nline3\n" {
		t.Fatalf("追加内容 = %q", data)
	}
}

// S16-S19 Stat/List/Remove/EnsureDir 三态。
func TestStoreStatListRemoveEnsureDir(t *testing.T) {
	st := newTestStore(t)
	_ = st.Write("d/f.txt", []byte("x"))

	if info, err := st.Stat("d/f.txt"); err != nil || info.IsDir() {
		t.Fatalf("Stat 文件: %v %v", info, err)
	}
	if info, err := st.Stat("d"); err != nil || !info.IsDir() {
		t.Fatalf("Stat 目录: %v %v", info, err)
	}
	if _, err := st.Stat("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat 不存在: %v", err)
	}

	if entries, err := st.List("d"); err != nil || len(entries) != 1 || entries[0].Name() != "f.txt" {
		t.Fatalf("List: %v %v", entries, err)
	}
	if err := st.EnsureDir("d/empty"); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if entries, err := st.List("d/empty"); err != nil || len(entries) != 0 {
		t.Fatalf("List 空目录: %v %v", entries, err)
	}
	if _, err := st.List("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("List 不存在: %v", err)
	}

	if err := st.Remove("d/f.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := st.Remove("d/f.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Remove 不存在: %v", err)
	}
	if err := st.EnsureDir("d/empty"); err != nil {
		t.Fatalf("EnsureDir 幂等: %v", err)
	}
	if err := st.EnsureDir("a/b/c/deep"); err != nil {
		t.Fatalf("EnsureDir 深层: %v", err)
	}
}

// S20 键归一：变体写法命中同一文件（也即同一把锁）。
func TestStoreKeyNormalization(t *testing.T) {
	st := newTestStore(t)
	if err := st.Write("a/b.txt", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"./a/b.txt", "a//b.txt", "a/./b.txt"} {
		data, err := st.Read(variant)
		if err != nil || string(data) != "v1" {
			t.Errorf("Read(%q) = %q, %v", variant, data, err)
		}
	}
	ls := st.(*localStore)
	ls.locksMu.Lock()
	n := len(ls.locks)
	ls.locksMu.Unlock()
	if n != 1 {
		t.Fatalf("锁表应只有 1 个 key（变体归一），got %d", n)
	}
}

// S21 并发 Append 同 key：无交错半行。
func TestStoreConcurrentAppend(t *testing.T) {
	st := newTestStore(t)
	const goroutines, perG = 8, 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				line := fmt.Sprintf(`{"g":%d,"i":%d,"pad":%q}`+"\n", g, i, strings.Repeat("x", 200))
				if err := st.Append("conc/append.jsonl", []byte(line)); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	data, err := st.Read("conc/append.jsonl")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != goroutines*perG {
		t.Fatalf("行数 = %d, want %d", len(lines), goroutines*perG)
	}
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("第 %d 行交错损坏: %q", i, line)
		}
	}
}

// S22 并发 WriteAtomic + Read：读侧永远看到某次完整写入。
func TestStoreConcurrentAtomicWriteRead(t *testing.T) {
	st := newTestStore(t)
	payloads := make([][]byte, 4)
	for i := range payloads {
		payloads[i] = []byte(fmt.Sprintf(`{"version":%d,"pad":%q}`, i, strings.Repeat("p", 1000+i)))
	}
	if err := st.WriteAtomic("conc/doc.json", payloads[0]); err != nil {
		t.Fatal(err)
	}
	valid := make(map[string]bool, len(payloads))
	for _, p := range payloads {
		valid[string(p)] = true
	}

	var writers, readers sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 2; w++ {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			for i := 0; i < 50; i++ {
				_ = st.WriteAtomic("conc/doc.json", payloads[(w*50+i)%len(payloads)])
			}
		}(w)
	}
	var readErr error
	var readErrOnce sync.Once
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := st.Read("conc/doc.json")
				if err != nil {
					readErrOnce.Do(func() { readErr = err })
					return
				}
				if !valid[string(data)] {
					readErrOnce.Do(func() { readErr = fmt.Errorf("读到拼接/半截内容: %q", data) })
					return
				}
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	if readErr != nil {
		t.Fatal(readErr)
	}
}

// S23 并发首次访问同 key：锁表懒建竞态（-race 守护）。
func TestStoreConcurrentLockCreation(t *testing.T) {
	st := newTestStore(t)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			_ = st.Append("lock/new.jsonl", []byte(fmt.Sprintf("{\"g\":%d}\n", g)))
		}(g)
	}
	wg.Wait()
	data, err := st.Read("lock/new.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != 16 {
		t.Fatalf("行数 = %d, want 16", n)
	}
}

// S24 并发不同 key 互不阻塞（正确性由 -race 与结果断言保证）。
func TestStoreConcurrentDistinctKeys(t *testing.T) {
	st := newTestStore(t)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			rel := fmt.Sprintf("multi/file-%d.txt", g)
			for i := 0; i < 20; i++ {
				if err := st.Write(rel, []byte(fmt.Sprintf("g%d-i%d", g, i))); err != nil {
					t.Errorf("Write: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	entries, err := st.List("multi")
	if err != nil || len(entries) != 8 {
		t.Fatalf("List = %d, %v", len(entries), err)
	}
}

// NewLocalStore 入参校验。
func TestNewLocalStoreValidation(t *testing.T) {
	if _, err := NewLocalStore(""); err == nil {
		t.Fatalf("空 root 应报错")
	}
	if _, err := NewLocalStore("   "); err == nil {
		t.Fatalf("空白 root 应报错")
	}
}

// 全方法对非法 rel 的拒绝分支（防穿越判定在每个 IO 入口生效）。
func TestStoreAllMethodsRejectTraversal(t *testing.T) {
	st := newTestStore(t)
	const bad = "../escape"
	if _, err := st.Read(bad); err == nil {
		t.Errorf("Read 应拒绝")
	}
	if err := st.Write(bad, []byte("x")); err == nil {
		t.Errorf("Write 应拒绝")
	}
	if err := st.WriteAtomic(bad, []byte("x")); err == nil {
		t.Errorf("WriteAtomic 应拒绝")
	}
	if err := st.Append(bad, []byte("x")); err == nil {
		t.Errorf("Append 应拒绝")
	}
	if _, err := st.Stat(bad); err == nil {
		t.Errorf("Stat 应拒绝")
	}
	if _, err := st.List(bad); err == nil {
		t.Errorf("List 应拒绝")
	}
	if err := st.Remove(bad); err == nil {
		t.Errorf("Remove 应拒绝")
	}
	if err := st.EnsureDir(bad); err == nil {
		t.Errorf("EnsureDir 应拒绝")
	}
	if _, err := st.Stat(filepath.Join(st.Root(), "abs")); err == nil {
		t.Errorf("绝对路径应拒绝")
	}
}
