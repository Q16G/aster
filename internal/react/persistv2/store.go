package persistv2

import (
	"aster/internal/workspacefs"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"
)

type Store struct {
	workspaceRoot string
	sessionID     string
	sessionDir    string

	eventsPath   string
	snapshotPath string
	blobsDir     string

	// fsStore/layout：M7 起全部常规 IO 经 workspacefs.Store（rel 经 layout 的
	// Session* 方法族取 Rel 形态）；events_repair.go 的截断修复是唯一 os 直连豁免。
	fsStore workspacefs.Store
	layout  workspacefs.Layout

	mu sync.Mutex

	// lastDiagnostics captures any degraded recovery / tail corruption info discovered
	// while reading or repairing events.jsonl. It is materialized into snapshot.json
	// by SaveSnapshotAtomic so callers don't have to remember to propagate it.
	lastDiagnostics *SystemDiagnostics

	// eventsCache avoids full scans of events.jsonl when snapshot reconciliation is needed.
	// It is keyed by (size, modTime). This assumes a single-writer process in the common case.
	eventsCacheValid   bool
	eventsCacheSize    int64
	eventsCacheModTime time.Time
	eventsCacheLastSeq uint64
}

type openOptions struct {
	fsStore workspacefs.Store
}

// Option 配置 Open 行为。
type Option func(*openOptions)

// WithStore 注入既有 workspacefs.Store（如 WorkspaceRuntime.Store()），
// 缺省时 Open 自建 workspacefs.NewLocalStore(workspaceRoot)。
func WithStore(st workspacefs.Store) Option {
	return func(o *openOptions) { o.fsStore = st }
}

func Open(workspaceRoot, sessionID string, opts ...Option) (*Store, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	sessionID = strings.TrimSpace(sessionID)
	if workspaceRoot == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is empty")
	}

	var o openOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	fsStore := o.fsStore
	if fsStore == nil {
		st, err := workspacefs.NewLocalStore(workspaceRoot)
		if err != nil {
			return nil, err
		}
		fsStore = st
	}

	l := workspacefs.New(workspaceRoot, "")
	s := &Store{
		workspaceRoot: workspaceRoot,
		sessionID:     sessionID,
		sessionDir:    l.SessionDir(sessionID),
		eventsPath:    l.SessionEvents(sessionID),
		snapshotPath:  l.SessionSnapshot(sessionID),
		blobsDir:      l.SessionBlobsDir(sessionID),
		fsStore:       fsStore,
		layout:        l,
	}
	if err := s.fsStore.EnsureDir(l.SessionBlobsDirRel(sessionID)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) SessionID() string {
	if s == nil {
		return ""
	}
	return s.sessionID
}

func (s *Store) SessionDir() string {
	if s == nil {
		return ""
	}
	return s.sessionDir
}

func (s *Store) EventsPath() string {
	if s == nil {
		return ""
	}
	return s.eventsPath
}

func (s *Store) SnapshotPath() string {
	if s == nil {
		return ""
	}
	return s.snapshotPath
}

func (s *Store) LoadSnapshot() (*Snapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	raw, err := s.fsStore.Read(s.layout.SessionSnapshotRel(s.sessionID))
	snap := (*Snapshot)(nil)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		// snapshot.json missing is recoverable: treat it as an empty materialized view
		// and reconcile from events.jsonl.
		snap = &Snapshot{
			FormatVersion: FormatVersion,
			SessionID:     s.sessionID,
			SessionState:  SessionStateIdle,
			LastSeq:       0,
			UpdatedAt:     time.Now(),
		}
	} else {
		var parsed Snapshot
		if err := json.Unmarshal(raw, &parsed); err != nil {
			// Do not silent-fail: attempt to rebuild from events.jsonl so we can still
			// recover a durable WAITING_FOR_HUMAN session even if snapshot.json is damaged.
			rebuilt, rerr := s.rebuildSnapshotFromEvents(fmt.Errorf("unmarshal snapshot: %w", err))
			if rerr != nil {
				return nil, rerr
			}
			return s.reconcileSnapshotAgainstEvents(rebuilt)
		}
		snap = &parsed
	}
	if snap.FormatVersion == 0 {
		snap.FormatVersion = FormatVersion
	}
	if strings.TrimSpace(snap.SessionID) == "" {
		snap.SessionID = s.sessionID
	}
	return s.reconcileSnapshotAgainstEvents(snap)
}

func (s *Store) reconcileSnapshotAgainstEvents(snap *Snapshot) (*Snapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	if snap == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}

	eventsLastSeq, diag, err := s.eventsLastSeqLocked()
	if err != nil {
		return nil, err
	}
	if diag != nil {
		s.mu.Lock()
		s.lastDiagnostics = diag
		s.mu.Unlock()
	}
	// Nothing to reconcile.
	if eventsLastSeq == 0 || eventsLastSeq <= snap.LastSeq {
		return snap, nil
	}

	prevLastSeq := snap.LastSeq
	replayDiag, err := s.ReplayEvents(func(ev *Event) error {
		if ev == nil {
			return nil
		}
		// Incremental reconcile: only apply missing events.
		if ev.Seq <= prevLastSeq {
			return nil
		}
		return ReduceSnapshot(snap, ev)
	})
	if err != nil {
		return nil, err
	}
	if replayDiag != nil {
		s.mu.Lock()
		s.lastDiagnostics = replayDiag
		s.mu.Unlock()
	}
	// Self-heal: persist the reconciled snapshot to avoid permanent divergence after crashes.
	if err := s.SaveSnapshotAtomic(snap); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *Store) eventsLastSeqLocked() (uint64, *SystemDiagnostics, error) {
	if s == nil {
		return 0, nil, fmt.Errorf("store is nil")
	}
	st, err := s.fsStore.Stat(s.layout.SessionEventsRel(s.sessionID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.mu.Lock()
			s.eventsCacheValid = false
			s.mu.Unlock()
			return 0, nil, nil
		}
		return 0, nil, err
	}
	size := st.Size()
	modTime := st.ModTime()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.eventsCacheValid &&
		s.eventsCacheSize == size &&
		s.eventsCacheModTime.Equal(modTime) {
		return s.eventsCacheLastSeq, nil, nil
	}

	lastSeq, diag := s.scanLastSeqLocked()
	// Refresh the cache with the most recent stat (best-effort).
	if st2, serr := s.fsStore.Stat(s.layout.SessionEventsRel(s.sessionID)); serr == nil {
		size = st2.Size()
		modTime = st2.ModTime()
	}
	s.eventsCacheValid = true
	s.eventsCacheSize = size
	s.eventsCacheModTime = modTime
	s.eventsCacheLastSeq = lastSeq
	return lastSeq, diag, nil
}

func (s *Store) SaveSnapshotAtomic(snap *Snapshot) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	if snap == nil {
		return fmt.Errorf("snapshot is nil")
	}
	s.mu.Lock()
	s.mergeDiagnosticsLocked(snap)
	s.mu.Unlock()
	snap.FormatVersion = FormatVersion
	if strings.TrimSpace(snap.SessionID) == "" {
		snap.SessionID = s.sessionID
	}
	snap.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	data = append(data, '\n')
	// Retry idempotent atomic writes: if a transient IO error happens we can safely
	// retry the whole temp+rename sequence.
	return withIOWriteRetry(func() error {
		return s.fsStore.WriteAtomic(s.layout.SessionSnapshotRel(s.sessionID), data)
	})
}

func (s *Store) AppendEvent(ev *Event) (*Event, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	if ev == nil {
		return nil, fmt.Errorf("event is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	lastSeq, diag, repairErr := s.repairEventsTailLocked()
	if repairErr != nil {
		return nil, repairErr
	}
	if diag != nil {
		s.lastDiagnostics = diag
	}
	nextSeq := lastSeq + 1

	out := *ev
	out.FormatVersion = FormatVersion
	out.Seq = nextSeq
	out.TimeUnixMs = time.Now().UnixMilli()
	out.SessionID = firstNonEmpty(strings.TrimSpace(out.SessionID), s.sessionID)
	if strings.TrimSpace(out.EventID) == "" {
		// Deterministic and globally unique enough for our use: session_id + seq.
		out.EventID = fmt.Sprintf("%s:%d", out.SessionID, out.Seq)
	}
	if strings.TrimSpace(out.Type) == "" {
		return nil, fmt.Errorf("event type is empty")
	}

	line, err := json.Marshal(&out)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')

	// 崩溃安全语义：O_APPEND + 写后 fsync（WithFsync），与迁移前一致；
	// append 写有意不走 withIOWriteRetry（盲重试可能重复事件，见 io_retry.go）。
	if err := s.fsStore.Append(s.layout.SessionEventsRel(s.sessionID), line, workspacefs.WithFsync()); err != nil {
		return nil, fmt.Errorf("append events.jsonl: %w", err)
	}

	return &out, nil
}

func (s *Store) WriteBlob(data []byte) (string, error) {
	if s == nil {
		return "", fmt.Errorf("store is nil")
	}
	if len(data) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:])
	rel := s.layout.SessionBlobRel(s.sessionID, name)
	if _, err := s.fsStore.Stat(rel); err == nil {
		return "sha256:" + name, nil
	}
	if err := withIOWriteRetry(func() error {
		return s.fsStore.WriteAtomic(rel, data)
	}); err != nil {
		return "", err
	}
	return "sha256:" + name, nil
}

func (s *Store) ReadBlob(ref string) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	// layout.SessionBlobRel 承接裸 hex 与 "sha256:" 前缀两种形态的归一。
	rel := s.layout.SessionBlobRel(s.sessionID, ref)
	if rel == "" {
		return nil, nil
	}
	return s.fsStore.Read(rel)
}

func (s *Store) BlobPath(ref string) string {
	if s == nil {
		return ""
	}
	return s.layout.SessionBlob(s.sessionID, ref)
}

// ReplayEvents scans events.jsonl and calls apply for each valid event.
// It tolerates tail truncation: the first invalid line causes replay to stop,
// and the returned diagnostics describe the degraded recovery.
func (s *Store) ReplayEvents(apply func(ev *Event) error) (*SystemDiagnostics, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	if apply == nil {
		return nil, fmt.Errorf("apply func is nil")
	}

	raw, err := s.fsStore.Read(s.layout.SessionEventsRel(s.sessionID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)

	var (
		lastGoodSeq uint64
		parseErr    error
	)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			parseErr = err
			break
		}
		if ev.Seq > lastGoodSeq {
			lastGoodSeq = ev.Seq
		}
		if err := apply(&ev); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan events.jsonl: %w", err)
	}

	if parseErr == nil {
		return nil, nil
	}
	return &SystemDiagnostics{
		Degraded:             true,
		EventsTailTruncated:  true,
		EventsLastGoodSeq:    lastGoodSeq,
		EventsLastParseError: parseErr.Error(),
		Notes: []string{
			"events.jsonl tail parse failed; ignoring trailing corrupted bytes",
		},
	}, nil
}

// scanLastSeqLocked reads the last good event sequence.
// Caller must hold s.mu.
func (s *Store) scanLastSeqLocked() (uint64, *SystemDiagnostics) {
	raw, err := s.fsStore.Read(s.layout.SessionEventsRel(s.sessionID))
	if err != nil {
		return 0, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)

	var (
		lastGoodSeq uint64
		parseErr    error
	)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			parseErr = err
			break
		}
		if ev.Seq > lastGoodSeq {
			lastGoodSeq = ev.Seq
		}
	}
	if parseErr == nil {
		return lastGoodSeq, nil
	}
	return lastGoodSeq, &SystemDiagnostics{
		Degraded:             true,
		EventsTailTruncated:  true,
		EventsLastGoodSeq:    lastGoodSeq,
		EventsLastParseError: parseErr.Error(),
		Notes: []string{
			"events.jsonl tail parse failed during seq scan; next seq derived from last good event",
		},
	}
}

func (s *Store) mergeDiagnosticsLocked(snap *Snapshot) {
	if s == nil || snap == nil {
		return
	}
	diag := s.lastDiagnostics
	if diag == nil {
		return
	}
	if snap.System == nil {
		c := *diag
		snap.System = &c
		return
	}
	if diag.Degraded {
		snap.System.Degraded = true
	}
	if diag.EventsTailTruncated {
		snap.System.EventsTailTruncated = true
	}
	if diag.EventsLastGoodSeq > 0 {
		snap.System.EventsLastGoodSeq = diag.EventsLastGoodSeq
	}
	if strings.TrimSpace(diag.EventsLastParseError) != "" {
		snap.System.EventsLastParseError = diag.EventsLastParseError
	}
	if len(diag.Notes) > 0 {
		snap.System.Notes = append(snap.System.Notes, diag.Notes...)
	}
}

func (s *Store) rebuildSnapshotFromEvents(snapshotErr error) (*Snapshot, error) {
	events := make([]*Event, 0, 128)
	diag, err := s.ReplayEvents(func(ev *Event) error {
		if ev == nil {
			return nil
		}
		c := *ev
		events = append(events, &c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot load failed (%v) and replay events failed: %w", snapshotErr, err)
	}

	// Mark degraded due to snapshot damage even if events replay is clean.
	if diag == nil {
		diag = &SystemDiagnostics{
			Degraded: true,
			Notes: []string{
				"snapshot.json parse failed; rebuilt snapshot from events.jsonl",
			},
		}
	} else {
		diag.Degraded = true
		diag.Notes = append(diag.Notes, "snapshot.json parse failed; rebuilt snapshot from events.jsonl")
	}
	s.mu.Lock()
	s.lastDiagnostics = diag
	s.mu.Unlock()

	snap, berr := BuildSnapshotFromEvents(s.sessionID, events, diag)
	if berr != nil {
		return nil, fmt.Errorf("rebuild snapshot from events failed: %w", berr)
	}
	// Best-effort self-heal: persist rebuilt snapshot. If this fails we must fail fast.
	if err := s.SaveSnapshotAtomic(snap); err != nil {
		return nil, fmt.Errorf("save rebuilt snapshot failed: %w", err)
	}
	return snap, nil
}

func firstNonEmpty(items ...string) string {
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			return strings.TrimSpace(it)
		}
	}
	return ""
}
