package workspacefs

import "io/fs"

// Store 是 workspace 的本地文件系统 IO 原语，Go 侧全部 workspace IO 的唯一入口。
// 所有 rel 为相对 workspace root 的 slash 路径（Layout 的 XxxRel() 产出）。
// 实现必须防路径穿越（AbsPath 是 rel→abs 的唯一出口）。
//
// 方法集按现存调用点证据推导（见 refactor-docs/01-workspace-storage.md「接口推导」）：
// 不含流式 Open（现存全是整读整写）、不含 Glob（List 后调用方过滤）、
// 不含 Update 持锁读改写（ledger lost-update 防护走 prompt 纪律，属既定原则）。
type Store interface {
	// Root 返回根目录绝对路径（prompt 指针注入与子进程依赖真实路径，永不为空）。
	Root() string
	// AbsPath 将 rel 解析为防穿越校验后的绝对路径。
	AbsPath(rel string) (string, error)

	// Read 整读；不存在时 errors.Is(err, fs.ErrNotExist)。
	Read(rel string) ([]byte, error)
	// Write 整体覆盖写，自动创建父目录。
	Write(rel string, data []byte) error
	// WriteAtomic 原子覆盖写：temp + fsync + rename + fsync(dir)。
	// 语义承接 persistv2/atomic_write.go，崩溃时读侧只见旧内容或新内容。
	WriteAtomic(rel string, data []byte) error
	// Append 追加写（O_APPEND），自动创建父目录；WithFsync() 供崩溃安全流。
	Append(rel string, data []byte, opts ...AppendOption) error
	Stat(rel string) (fs.FileInfo, error)
	// List 枚举 rel 目录的直接子项；目录不存在时 errors.Is(err, fs.ErrNotExist)。
	List(rel string) ([]fs.DirEntry, error)
	Remove(rel string) error
	EnsureDir(rel string) error
}

type appendOptions struct {
	fsync bool
}

// AppendOption 配置 Append 行为。
type AppendOption func(*appendOptions)

// WithFsync 使 Append 在写后执行 f.Sync()（persistv2 events.jsonl 崩溃安全要求）。
func WithFsync() AppendOption {
	return func(o *appendOptions) { o.fsync = true }
}
