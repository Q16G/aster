package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TimelineMemoryDiffer 用于计算 TimelineMemory 的增量 diff
type TimelineMemoryDiffer struct {
	timeline *TimelineMemory
	lastDump string
	lastAt   time.Time
	mux      sync.RWMutex
}

// TimelineMemoryDifferBaseline diff 基准快照
type TimelineMemoryDifferBaseline struct {
	Dump string
	At   time.Time
}

// NewTimelineMemoryDiffer 创建 diff 计算器
func NewTimelineMemoryDiffer(timeline *TimelineMemory) *TimelineMemoryDiffer {
	return &TimelineMemoryDiffer{
		timeline: timeline,
	}
}

// SnapshotBaseline 获取当前基准快照
func (d *TimelineMemoryDiffer) SnapshotBaseline() TimelineMemoryDifferBaseline {
	if d == nil {
		return TimelineMemoryDifferBaseline{}
	}
	d.mux.RLock()
	out := TimelineMemoryDifferBaseline{
		Dump: d.lastDump,
		At:   d.lastAt,
	}
	d.mux.RUnlock()
	return out
}

// RestoreBaseline 恢复基准快照
func (d *TimelineMemoryDiffer) RestoreBaseline(b TimelineMemoryDifferBaseline) {
	if d == nil {
		return
	}
	d.mux.Lock()
	d.lastDump = b.Dump
	d.lastAt = b.At
	d.mux.Unlock()
}

// Diff 计算当前状态与基准的差异，并更新基准
func (d *TimelineMemoryDiffer) Diff() (string, error) {
	if d == nil || d.timeline == nil {
		return "", nil
	}

	now := time.Now().UTC()
	currentDump := d.timeline.Dump()

	d.mux.RLock()
	lastDump := d.lastDump
	lastAt := d.lastAt
	d.mux.RUnlock()

	if currentDump == lastDump {
		d.mux.Lock()
		d.lastDump = currentDump
		d.lastAt = now
		d.mux.Unlock()
		return "", nil
	}

	diff, err := diffDumpAsGitPatch(lastDump, currentDump, lastAt, now)
	if err != nil {
		return "", err
	}

	d.mux.Lock()
	d.lastDump = currentDump
	d.lastAt = now
	d.mux.Unlock()

	return diff, nil
}

// PeekDiff 计算差异但不更新基准
func (d *TimelineMemoryDiffer) PeekDiff() (string, error) {
	if d == nil || d.timeline == nil {
		return "", nil
	}

	now := time.Now().UTC()
	currentDump := d.timeline.Dump()

	d.mux.RLock()
	lastDump := d.lastDump
	lastAt := d.lastAt
	d.mux.RUnlock()

	if currentDump == lastDump {
		return "", nil
	}

	return diffDumpAsGitPatch(lastDump, currentDump, lastAt, now)
}

// SetBaseline 手动设置基准为当前状态
func (d *TimelineMemoryDiffer) SetBaseline() {
	if d == nil || d.timeline == nil {
		return
	}
	d.mux.Lock()
	defer d.mux.Unlock()
	d.lastDump = d.timeline.Dump()
	d.lastAt = time.Now().UTC()
}

// Reset 重置基准
func (d *TimelineMemoryDiffer) Reset() {
	if d == nil {
		return
	}
	d.mux.Lock()
	defer d.mux.Unlock()
	d.lastDump = ""
	d.lastAt = time.Time{}
}

func diffDumpAsGitPatch(oldDump, newDump string, oldAt, newAt time.Time) (string, error) {
	if oldDump == newDump {
		return "", nil
	}

	oldLines := splitLinesKeepEOL(oldDump)
	newLines := splitLinesKeepEOL(newDump)
	ops := myersDiffLines(oldLines, newLines)
	if len(ops) == 0 {
		return "", nil
	}

	var buf strings.Builder
	buf.WriteString("diff --git a/timeline_memory b/timeline_memory\n")
	buf.WriteString("--- a/timeline_memory")
	if ts := formatRFC3339OrEmpty(oldAt); ts != "" {
		buf.WriteString("\t")
		buf.WriteString(ts)
	}
	buf.WriteString("\n")
	buf.WriteString("+++ b/timeline_memory")
	if ts := formatRFC3339OrEmpty(newAt); ts != "" {
		buf.WriteString("\t")
		buf.WriteString(ts)
	}
	buf.WriteString("\n")
	buf.WriteString("@@\n")

	wrote := false
	for _, op := range ops {
		if op == nil {
			continue
		}
		switch op.kind {
		case '+', '-':
			wrote = true
		default:
			continue
		}
		buf.WriteByte(op.kind)
		buf.WriteString(op.line)
	}
	if !wrote {
		return "", nil
	}
	return buf.String(), nil
}

type diffOp struct {
	kind byte
	line string
}

func myersDiffLines(a, b []string) []*diffOp {
	n := len(a)
	m := len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		ops := make([]*diffOp, 0, m)
		for _, line := range b {
			ops = append(ops, &diffOp{kind: '+', line: line})
		}
		return ops
	}
	if m == 0 {
		ops := make([]*diffOp, 0, n)
		for _, line := range a {
			ops = append(ops, &diffOp{kind: '-', line: line})
		}
		return ops
	}

	max := n + m
	offset := max

	v := make([]int, 2*max+1)
	for i := range v {
		v[i] = -1
	}
	v[offset+1] = 0

	trace := make([][]int, 0, max+1)

	for d := 0; d <= max; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)

		for k := -d; k <= d; k += 2 {
			idx := offset + k

			var x int
			if k == -d || (k != d && v[idx-1] < v[idx+1]) {
				x = v[idx+1]
			} else {
				x = v[idx-1] + 1
			}
			y := x - k

			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}

			v[idx] = x
			if x >= n && y >= m {
				return backtrackMyers(trace, a, b, d, offset)
			}
		}
	}

	return []*diffOp{{kind: '-', line: fmt.Sprintf("diff computation exceeded max=%d\n", max)}}
}

func backtrackMyers(trace [][]int, a, b []string, dFound int, offset int) []*diffOp {
	x := len(a)
	y := len(b)

	edits := make([]*diffOp, 0, len(a)+len(b))

	for d := dFound; d > 0; d-- {
		v := trace[d]
		k := x - y

		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := v[offset+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			edits = append(edits, &diffOp{kind: ' ', line: a[x-1]})
			x--
			y--
		}

		if x == prevX {
			edits = append(edits, &diffOp{kind: '+', line: b[prevY]})
		} else {
			edits = append(edits, &diffOp{kind: '-', line: a[prevX]})
		}

		x = prevX
		y = prevY
	}

	for x > 0 && y > 0 {
		edits = append(edits, &diffOp{kind: ' ', line: a[x-1]})
		x--
		y--
	}
	for x > 0 {
		edits = append(edits, &diffOp{kind: '-', line: a[x-1]})
		x--
	}
	for y > 0 {
		edits = append(edits, &diffOp{kind: '+', line: b[y-1]})
		y--
	}

	reverseDiffOps(edits)
	return edits
}

func reverseDiffOps(ops []*diffOp) {
	if len(ops) < 2 {
		return
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
}

func splitLinesKeepEOL(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func formatRFC3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
