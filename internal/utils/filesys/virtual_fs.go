package filesys

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// VirtualFS is a tiny in-memory filesystem used to migrate yaklang tests.
//
// It intentionally implements the standard library io/fs interfaces:
//   - fs.FS (Open)
//   - fs.ReadFileFS (ReadFile)
//   - fs.ReadDirFS (ReadDir)
//
// Path semantics:
//   - Internally normalized to forward slashes (/)
//   - Leading "/" is trimmed
//   - Windows-style "\\" in added file paths is accepted and normalized
type VirtualFS struct {
	files map[string]*virtualFileData // normalized path -> file content
}

type virtualFileData struct {
	name    string
	content []byte
	modTime time.Time
}

func NewVirtualFs() *VirtualFS {
	return &VirtualFS{
		files: make(map[string]*virtualFileData),
	}
}

func (v *VirtualFS) AddFile(name, content string) {
	if v == nil {
		return
	}
	name = normalizeVFSPath(name)
	if name == "" || name == "." {
		return
	}
	if v.files == nil {
		v.files = make(map[string]*virtualFileData)
	}
	v.files[name] = &virtualFileData{
		name:    path.Base(name),
		content: []byte(content),
		modTime: time.Now(),
	}
}

func (v *VirtualFS) ReadFile(name string) ([]byte, error) {
	f, err := v.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (v *VirtualFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if v == nil {
		return nil, fs.ErrInvalid
	}
	dir := normalizeVFSPath(name)
	if dir == "." {
		dir = ""
	}

	entries := make(map[string]fs.DirEntry)
	addEntry := func(entryPath string, isDir bool, size int64, mod time.Time) {
		base := path.Base(entryPath)
		if base == "" || base == "." {
			return
		}
		if _, ok := entries[base]; ok {
			return
		}
		mode := fs.FileMode(0)
		if isDir {
			mode = fs.ModeDir
		}
		entries[base] = virtualDirEntry{fi: virtualFileInfo{
			name:    base,
			size:    size,
			mode:    mode,
			modTime: mod,
		}}
	}

	hasAny := false
	for filePath, fd := range v.files {
		filePath = normalizeVFSPath(filePath)
		if dir != "" {
			prefix := dir + "/"
			if filePath != dir && !strings.HasPrefix(filePath, prefix) {
				continue
			}
			if filePath == dir {
				// a file shadowing a directory (rare); treat as exists.
				hasAny = true
				continue
			}
			rest := strings.TrimPrefix(filePath, prefix)
			if rest == "" {
				continue
			}
			seg, after, _ := strings.Cut(rest, "/")
			if seg == "" {
				continue
			}
			hasAny = true
			if after != "" {
				addEntry(prefix+seg, true, 0, fd.modTime)
				continue
			}
			addEntry(prefix+seg, false, int64(len(fd.content)), fd.modTime)
			continue
		}

		// root dir
		rest := filePath
		seg, after, _ := strings.Cut(rest, "/")
		if seg == "" {
			continue
		}
		hasAny = true
		if after != "" {
			addEntry(seg, true, 0, fd.modTime)
			continue
		}
		addEntry(seg, false, int64(len(fd.content)), fd.modTime)
	}

	if dir != "" && !hasAny {
		return nil, fs.ErrNotExist
	}

	out := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (v *VirtualFS) Open(name string) (fs.File, error) {
	if v == nil {
		return nil, fs.ErrInvalid
	}
	name = normalizeVFSPath(name)
	if name == "." {
		name = ""
	}

	if name == "" {
		return newVirtualDirFile(v, ""), nil
	}
	if fd, ok := v.files[name]; ok && fd != nil {
		return newVirtualRegularFile(name, fd), nil
	}
	if v.dirExists(name) {
		return newVirtualDirFile(v, name), nil
	}
	return nil, fs.ErrNotExist
}

func (v *VirtualFS) dirExists(dir string) bool {
	dir = normalizeVFSPath(dir)
	if dir == "" || dir == "." {
		return true
	}
	prefix := dir + "/"
	for filePath := range v.files {
		filePath = normalizeVFSPath(filePath)
		if strings.HasPrefix(filePath, prefix) {
			return true
		}
	}
	return false
}

func normalizeVFSPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "."
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "."
	}
	return p
}

// ---------------- fs.File implementations

type virtualFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (fi virtualFileInfo) Name() string       { return fi.name }
func (fi virtualFileInfo) Size() int64        { return fi.size }
func (fi virtualFileInfo) Mode() fs.FileMode  { return fi.mode }
func (fi virtualFileInfo) ModTime() time.Time { return fi.modTime }
func (fi virtualFileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi virtualFileInfo) Sys() any           { return nil }

type virtualDirEntry struct {
	fi virtualFileInfo
}

func (de virtualDirEntry) Name() string               { return de.fi.Name() }
func (de virtualDirEntry) IsDir() bool                { return de.fi.IsDir() }
func (de virtualDirEntry) Type() fs.FileMode          { return de.fi.Mode().Type() }
func (de virtualDirEntry) Info() (fs.FileInfo, error) { return de.fi, nil }

type virtualRegularFile struct {
	name string
	fi   virtualFileInfo
	r    *bytes.Reader
}

func newVirtualRegularFile(fullPath string, fd *virtualFileData) *virtualRegularFile {
	if fd == nil {
		fd = &virtualFileData{name: path.Base(fullPath)}
	}
	content := append([]byte(nil), fd.content...)
	return &virtualRegularFile{
		name: fullPath,
		fi: virtualFileInfo{
			name:    path.Base(fullPath),
			size:    int64(len(content)),
			mode:    0,
			modTime: fd.modTime,
		},
		r: bytes.NewReader(content),
	}
}

func (f *virtualRegularFile) Stat() (fs.FileInfo, error) { return f.fi, nil }
func (f *virtualRegularFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *virtualRegularFile) Close() error               { return nil }

type virtualDirFile struct {
	vfs     *VirtualFS
	dir     string
	entries []fs.DirEntry
	offset  int
}

func newVirtualDirFile(vfs *VirtualFS, dir string) *virtualDirFile {
	return &virtualDirFile{vfs: vfs, dir: dir}
}

func (d *virtualDirFile) Stat() (fs.FileInfo, error) {
	name := path.Base(d.dir)
	if d.dir == "" {
		name = "."
	}
	return virtualFileInfo{
		name:    name,
		size:    0,
		mode:    fs.ModeDir,
		modTime: time.Time{},
	}, nil
}

func (d *virtualDirFile) Read(p []byte) (int, error) { return 0, io.EOF }
func (d *virtualDirFile) Close() error               { return nil }

func (d *virtualDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.vfs == nil {
		return nil, fs.ErrInvalid
	}
	if d.entries == nil {
		entries, err := d.vfs.ReadDir(d.dir)
		if err != nil {
			return nil, err
		}
		d.entries = entries
	}

	if n <= 0 {
		if d.offset >= len(d.entries) {
			return nil, io.EOF
		}
		out := append([]fs.DirEntry(nil), d.entries[d.offset:]...)
		d.offset = len(d.entries)
		return out, nil
	}

	if d.offset >= len(d.entries) {
		return nil, io.EOF
	}
	end := d.offset + n
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := append([]fs.DirEntry(nil), d.entries[d.offset:end]...)
	d.offset = end
	if d.offset >= len(d.entries) {
		return out, io.EOF
	}
	return out, nil
}

// Ensure interface satisfaction.
var (
	_ fs.FS          = (*VirtualFS)(nil)
	_ fs.ReadFileFS  = (*VirtualFS)(nil)
	_ fs.ReadDirFS   = (*VirtualFS)(nil)
	_ fs.File        = (*virtualRegularFile)(nil)
	_ fs.ReadDirFile = (*virtualDirFile)(nil)
)

func (v *VirtualFS) String() string {
	if v == nil {
		return "<nil>"
	}
	paths := make([]string, 0, len(v.files))
	for p := range v.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return fmt.Sprintf("VirtualFS(%d files): %s", len(paths), strings.Join(paths, ", "))
}
