package persistv2

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	mu sync.Mutex
}

func Open(workspaceRoot, sessionID string) (*Store, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	sessionID = strings.TrimSpace(sessionID)
	if workspaceRoot == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is empty")
	}

	sessionDir := filepath.Join(workspaceRoot, "workspace", "sessions", sessionID)
	s := &Store{
		workspaceRoot: workspaceRoot,
		sessionID:     sessionID,
		sessionDir:    sessionDir,
		eventsPath:    filepath.Join(sessionDir, "events.jsonl"),
		snapshotPath:  filepath.Join(sessionDir, "snapshot.json"),
		blobsDir:      filepath.Join(sessionDir, "blobs"),
	}
	if err := os.MkdirAll(s.blobsDir, 0o755); err != nil {
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
	raw, err := os.ReadFile(s.snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Snapshot{
				FormatVersion: FormatVersion,
				SessionID:     s.sessionID,
				SessionState:  SessionStateIdle,
				UpdatedAt:     time.Now(),
			}, nil
		}
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	if snap.FormatVersion == 0 {
		snap.FormatVersion = FormatVersion
	}
	if strings.TrimSpace(snap.SessionID) == "" {
		snap.SessionID = s.sessionID
	}
	return &snap, nil
}

func (s *Store) SaveSnapshotAtomic(snap *Snapshot) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	if snap == nil {
		return fmt.Errorf("snapshot is nil")
	}
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
	return writeFileAtomic(s.snapshotPath, data, 0o644)
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

	lastSeq, diag := s.scanLastSeqLocked()
	nextSeq := lastSeq + 1

	out := *ev
	out.FormatVersion = FormatVersion
	out.Seq = nextSeq
	out.TimeUnixMs = time.Now().UnixMilli()
	out.SessionID = firstNonEmpty(strings.TrimSpace(out.SessionID), s.sessionID)
	if strings.TrimSpace(out.Type) == "" {
		return nil, fmt.Errorf("event type is empty")
	}

	line, err := json.Marshal(&out)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')

	if err := os.MkdirAll(filepath.Dir(s.eventsPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events.jsonl: %w", err)
	}
	_, werr := f.Write(line)
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil {
		return nil, fmt.Errorf("append events.jsonl: %w", werr)
	}
	if serr != nil {
		return nil, fmt.Errorf("fsync events.jsonl: %w", serr)
	}
	if cerr != nil {
		return nil, cerr
	}

	// Best-effort: if the log was degraded, keep that info in snapshot diagnostics
	// when caller chooses to persist a snapshot.
	_ = diag
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
	path := filepath.Join(s.blobsDir, name)
	if _, err := os.Stat(path); err == nil {
		return "sha256:" + name, nil
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return "", err
	}
	return "sha256:" + name, nil
}

func (s *Store) ReadBlob(ref string) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if strings.HasPrefix(ref, "sha256:") {
		ref = strings.TrimPrefix(ref, "sha256:")
	}
	if ref == "" {
		return nil, nil
	}
	path := filepath.Join(s.blobsDir, ref)
	return os.ReadFile(path)
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

	raw, err := os.ReadFile(s.eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
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
	raw, err := os.ReadFile(s.eventsPath)
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

func firstNonEmpty(items ...string) string {
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			return strings.TrimSpace(it)
		}
	}
	return ""
}
