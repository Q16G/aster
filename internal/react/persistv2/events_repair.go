package persistv2

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// repairEventsTailLocked ensures events.jsonl contains only valid JSON objects per line.
//
// If the file tail is corrupted (e.g. crash mid-write), it truncates to the last
// known-good line boundary so future appends remain readable by scan/replay.
//
// Caller must hold s.mu.
func (s *Store) repairEventsTailLocked() (lastGoodSeq uint64, diag *SystemDiagnostics, err error) {
	if s == nil {
		return 0, nil, fmt.Errorf("store is nil")
	}
	path := s.eventsPath
	st, statErr := os.Stat(path)
	if statErr != nil {
		return 0, nil, nil
	}
	// Empty log is fine.
	if st.Size() == 0 {
		return 0, nil, nil
	}

	f, oerr := os.Open(path)
	if oerr != nil {
		return 0, nil, fmt.Errorf("open events.jsonl: %w", oerr)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var (
		offset         int64
		lastGoodOffset int64
		parseErr       error
	)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				var ev Event
				if jerr := json.Unmarshal([]byte(trimmed), &ev); jerr != nil {
					parseErr = jerr
					break
				}
				if ev.Seq > lastGoodSeq {
					lastGoodSeq = ev.Seq
				}
				lastGoodOffset = offset + int64(len(line))
			}
			offset += int64(len(line))
		}
		if rerr == nil {
			continue
		}
		if rerr == io.EOF {
			break
		}
		return lastGoodSeq, nil, fmt.Errorf("read events.jsonl: %w", rerr)
	}
	if parseErr == nil {
		return lastGoodSeq, nil, nil
	}

	// Tail corruption detected: truncate to lastGoodOffset.
	// If we failed on the very first line, lastGoodOffset will be 0, which truncates to empty.
	if terr := os.Truncate(path, lastGoodOffset); terr != nil {
		return lastGoodSeq, nil, fmt.Errorf("truncate corrupted events.jsonl: %w", terr)
	}

	diag = &SystemDiagnostics{
		Degraded:             true,
		EventsTailTruncated:  true,
		EventsLastGoodSeq:    lastGoodSeq,
		EventsLastParseError: parseErr.Error(),
		Notes: []string{
			"events.jsonl tail parse failed; truncated file to last good event boundary",
		},
	}

	// Best-effort durability for directory entry updates.
	_ = fsyncDir(filepath.Dir(path))
	return lastGoodSeq, diag, nil
}
