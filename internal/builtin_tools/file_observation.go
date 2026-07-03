package builtin_tools

import (
	"strings"
	"sync"
	"time"
)

// FileObservation records the latest text snapshot used by the read-before-write
// guard. Only snapshots originating from a full read_file call are credentials.
type FileObservation struct {
	Path           string
	Content        string
	ModTime        time.Time
	ModTimeMillis  int64
	Offset         *int64
	Limit          *int64
	Partial        bool
	ReadCredential bool
}

func (o FileObservation) IsFullRead() bool {
	return !o.Partial && o.Offset == nil && o.Limit == nil
}

// FileObservationStore is shared by read_file and write/edit/notebook_edit.
type FileObservationStore struct {
	mu    sync.RWMutex
	items map[string]FileObservation
}

func NewFileObservationStore() *FileObservationStore {
	return &FileObservationStore{items: make(map[string]FileObservation)}
}

func (s *FileObservationStore) Record(obs FileObservation) {
	if s == nil {
		return
	}
	path := strings.TrimSpace(obs.Path)
	if path == "" {
		return
	}
	obs.Path = path
	s.mu.Lock()
	if s.items == nil {
		s.items = make(map[string]FileObservation)
	}
	s.items[path] = obs
	s.mu.Unlock()
}

func (s *FileObservationStore) Get(path string) (FileObservation, bool) {
	if s == nil {
		return FileObservation{}, false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return FileObservation{}, false
	}
	s.mu.RLock()
	obs, ok := s.items[path]
	s.mu.RUnlock()
	return obs, ok
}

func cloneInt64Ptr(v int64) *int64 {
	out := v
	return &out
}
