package workspacefs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// localStore 是 Store 的本地文件系统实现。
//
// 并发语义：对所有 key 一律 per-key RWMutex（懒建，不回收，量级 = session 内文件数），
// Read/Stat 读锁，写族写锁——取代 localWorkspaceRuntime 时代仅 9 个文件的
// sharedFileLockKeys 白名单（超集覆盖）。锁只保证进程内互斥；跨进程不承诺
// （单 session 单进程是既定运行模型）。
type localStore struct {
	root string

	locksMu sync.Mutex
	locks   map[string]*sync.RWMutex
}

// NewLocalStore 构造本地 Store。root 必须非空；相对 root 会被解析为绝对路径。
// 不做目录创建（惰性：首次写时按需建）。
func NewLocalStore(root string) (Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("workspace root dir is empty")
	}
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root dir: %w", err)
	}
	return &localStore{
		root:  abs,
		locks: make(map[string]*sync.RWMutex),
	}, nil
}

func (s *localStore) Root() string { return s.root }

// normalizeRel 归一 rel 为 slash clean 形式（锁键与路径解析共用同一归一，
// 保证 "./a"、"a//b" 与 "a"、"a/b" 命中同一把锁）。非法（空/绝对/穿越）返回错误。
// 判定口径与 react resolveAbsPath / builtin_tools WorkspaceArtifactWritePath 一致。
func (s *localStore) normalizeRel(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return "", fmt.Errorf("workspace relative path is empty")
	}
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", fmt.Errorf("workspace relative path must be relative")
	}
	local := filepath.Clean(filepath.FromSlash(rel))
	if local == "." || local == "" {
		return "", fmt.Errorf("workspace relative path is empty")
	}
	if filepath.IsAbs(local) || local == ".." || strings.HasPrefix(local, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace relative path escapes root")
	}
	return filepath.ToSlash(local), nil
}

func (s *localStore) AbsPath(rel string) (string, error) {
	norm, err := s.normalizeRel(rel)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(s.root, filepath.FromSlash(norm)))
	if err != nil {
		return "", fmt.Errorf("resolve workspace file path: %w", err)
	}
	// Clean+Join 后二次校验：绝不允许越出 root（与 resolveAbsPath 的双重校验对齐）。
	relToRoot, err := filepath.Rel(s.root, abs)
	if err != nil {
		return "", fmt.Errorf("resolve workspace file rel: %w", err)
	}
	relToRoot = filepath.Clean(relToRoot)
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("workspace file path escapes root")
	}
	return abs, nil
}

// lockFor 按归一后的 rel 懒建并返回锁。
func (s *localStore) lockFor(normRel string) *sync.RWMutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if mu, ok := s.locks[normRel]; ok {
		return mu
	}
	mu := &sync.RWMutex{}
	s.locks[normRel] = mu
	return mu
}

// resolve 归一 + 防穿越 + 取锁，写族/读族共用入口。
func (s *localStore) resolve(rel string) (abs string, mu *sync.RWMutex, err error) {
	norm, err := s.normalizeRel(rel)
	if err != nil {
		return "", nil, err
	}
	abs = filepath.Join(s.root, filepath.FromSlash(norm))
	relToRoot, rerr := filepath.Rel(s.root, abs)
	if rerr != nil {
		return "", nil, fmt.Errorf("resolve workspace file rel: %w", rerr)
	}
	relToRoot = filepath.Clean(relToRoot)
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", nil, fmt.Errorf("workspace file path escapes root")
	}
	return abs, s.lockFor(norm), nil
}

func (s *localStore) Read(rel string) ([]byte, error) {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	mu.RLock()
	defer mu.RUnlock()
	return os.ReadFile(abs)
}

func (s *localStore) Write(rel string, data []byte) error {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0o644)
}

// WriteAtomic 语义逐字节承接 persistv2/atomic_write.go：
// temp（同目录）→ chmod → write → fsync → close → rename → fsync(dir, best-effort)。
func (s *localStore) WriteAtomic(rel string, data []byte) error {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(abs)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return err
	}
	// Best effort: 目录项持久化。
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	return nil
}

func (s *localStore) Append(rel string, data []byte, opts ...AppendOption) error {
	var o appendOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	var serr error
	if o.fsync {
		serr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if serr != nil {
		return serr
	}
	return cerr
}

func (s *localStore) Stat(rel string) (fs.FileInfo, error) {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	mu.RLock()
	defer mu.RUnlock()
	return os.Stat(abs)
}

func (s *localStore) List(rel string) ([]fs.DirEntry, error) {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	mu.RLock()
	defer mu.RUnlock()
	return os.ReadDir(abs)
}

func (s *localStore) Remove(rel string) error {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return os.Remove(abs)
}

func (s *localStore) EnsureDir(rel string) error {
	abs, mu, err := s.resolve(rel)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return os.MkdirAll(abs, 0o755)
}
