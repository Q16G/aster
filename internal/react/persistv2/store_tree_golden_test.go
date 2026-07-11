package persistv2

// store_tree_golden_test.go —— M7（persistv2 IO 迁移到 workspacefs.Store）前的
// 树快照基线（V02 锚点）：固定输入驱动全部 persistv2 写点，对 workspaceRoot 产出
// rel(slash) -> sha256 的树快照，与 testdata/persistv2_tree_golden.json 逐字节比对。
// IO 迁移之后本基线必须不经 -update-persistv2-golden 通过，否则即为行为变化。
//
// 归一化规则（这些字段由代码内部盖 time.Now()，无法固定输入）：
//   - events.jsonl 逐行解析 JSON，删除 time / event_id 后再哈希；
//   - snapshot.json 整文件解析 JSON，删除 updated_at 后再哈希。
//
// 基线更新：go test ./internal/react/persistv2/ -run TreeGolden -update-persistv2-golden

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updatePersistv2Golden = flag.Bool("update-persistv2-golden", false, "重新生成 persistv2 树快照基线")

// buildPersistv2GoldenTree 用固定输入驱动全部 persistv2 写点：
// Open（blobs 目录创建）→ AppendEvent（不同 Type/StepID）→ SaveSnapshotAtomic
// → WriteBlob（两次同内容验证去重 + 一次不同内容）→ WriteStepAttemptResult。
func buildPersistv2GoldenTree(t *testing.T, root string) {
	t.Helper()
	const sessionID = "golden-persist-sess"

	store, err := Open(root, sessionID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	events := []*Event{
		{Type: "SESSION_CREATED"},
		{Type: "TURN_STARTED", TurnID: "turn-1", GroupID: "group-1", Payload: map[string]any{"input": "golden input"}},
		{Type: "STEP_STARTED", TurnID: "turn-1", StepID: "p1-s1", AttemptID: "call_gold01"},
		{Type: "STEP_FINISHED", TurnID: "turn-1", StepID: "p1-s2", AttemptID: "call_gold02", Payload: map[string]any{"status": "succeeded"}},
	}
	for i, ev := range events {
		if _, err := store.AppendEvent(ev); err != nil {
			t.Fatalf("AppendEvent[%d]: %v", i, err)
		}
	}

	if err := store.SaveSnapshotAtomic(&Snapshot{
		SessionID:    sessionID,
		SessionState: SessionStateIdle,
		CurrentTurn: &Turn{
			TurnID:     "turn-1",
			GroupID:    "group-1",
			Status:     TurnStatusSucceeded,
			Input:      "golden input",
			StartedAt:  1735786800000,
			FinishedAt: 1735786860000,
		},
		LastSeq: 4,
	}); err != nil {
		t.Fatalf("SaveSnapshotAtomic: %v", err)
	}

	// blob：同内容两次（去重，树上只应出现一个文件）+ 一次不同内容。
	ref1, err := store.WriteBlob([]byte("golden blob content\n"))
	if err != nil {
		t.Fatalf("WriteBlob(1): %v", err)
	}
	ref2, err := store.WriteBlob([]byte("golden blob content\n"))
	if err != nil {
		t.Fatalf("WriteBlob(2): %v", err)
	}
	if ref1 != ref2 {
		t.Fatalf("同内容 WriteBlob 的 ref 不一致: %q vs %q", ref1, ref2)
	}
	ref3, err := store.WriteBlob([]byte("golden blob other\n"))
	if err != nil {
		t.Fatalf("WriteBlob(3): %v", err)
	}
	if ref3 == ref1 {
		t.Fatalf("不同内容 WriteBlob 的 ref 不应相同: %q", ref3)
	}

	if _, err := store.WriteStepAttemptResult("p1-s1", "call_gold01", &StepAttemptResult{
		TurnID:          "turn-1",
		StepID:          "p1-s1",
		AttemptID:       "call_gold01",
		Status:          "succeeded",
		ShortSummary:    "golden short summary",
		LongSummary:     "golden long summary",
		ToolCallsDigest: []string{"rg pattern=foo -> 3 matches"},
		Display:         &StepAttemptDisplay{Title: "golden step", Summary: "golden display summary"},
		Timing:          &StepAttemptTiming{StartedAt: 1735786800000, FinishedAt: 1735786860000},
	}); err != nil {
		t.Fatalf("WriteStepAttemptResult: %v", err)
	}
}

// snapshotPersistv2Tree 产出 rel(slash) -> sha256 的树快照（含时间字段归一化）。
func snapshotPersistv2Tree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch {
		case strings.HasSuffix(rel, "/events.jsonl"):
			data = normalizeGoldenEventsJSONL(t, data)
		case strings.HasSuffix(rel, "/snapshot.json"):
			data = normalizeGoldenSnapshotJSON(t, data)
		}
		sum := sha256.Sum256(data)
		snap[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk persistv2 tree: %v", err)
	}
	return snap
}

// normalizeGoldenEventsJSONL 逐行删除内部盖 time.Now() 的 time 与派生的 event_id。
func normalizeGoldenEventsJSONL(t *testing.T, data []byte) []byte {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("events.jsonl 行不是合法 JSON: %v (%s)", err, line)
		}
		delete(row, "time")
		delete(row, "event_id")
		normalized, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("re-marshal events.jsonl 行失败: %v", err)
		}
		out = append(out, string(normalized))
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// normalizeGoldenSnapshotJSON 删除 SaveSnapshotAtomic 内部盖 time.Now() 的 updated_at。
func normalizeGoldenSnapshotJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatalf("snapshot.json 不是合法 JSON: %v", err)
	}
	delete(row, "updated_at")
	normalized, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("re-marshal snapshot.json 失败: %v", err)
	}
	return normalized
}

func assertPersistv2SnapshotEqual(t *testing.T, want, got map[string]string) {
	t.Helper()
	for key, wantDigest := range want {
		gotDigest, ok := got[key]
		if !ok {
			t.Errorf("golden 路径缺失: %s", key)
			continue
		}
		if gotDigest != wantDigest {
			t.Errorf("golden 内容漂移: %s\n  want %s\n  got  %s", key, wantDigest, gotDigest)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("golden 之外的新路径: %s", key)
		}
	}
}

func TestStore_TreeGolden(t *testing.T) {
	rootA := t.TempDir()
	buildPersistv2GoldenTree(t, rootA)
	snapA := snapshotPersistv2Tree(t, rootA)

	// 自证 harness 确定性：同一序列第二次构建必须逐字节一致。
	rootB := t.TempDir()
	buildPersistv2GoldenTree(t, rootB)
	snapB := snapshotPersistv2Tree(t, rootB)
	assertPersistv2SnapshotEqual(t, snapA, snapB)
	if t.Failed() {
		t.Fatalf("golden harness 自身不确定，先修 harness 再谈基线")
	}

	path := filepath.Join("testdata", "persistv2_tree_golden.json")
	if *updatePersistv2Golden {
		data, err := json.MarshalIndent(snapA, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("golden 已更新: %s（%d 条）", path, len(snapA))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 失败（首次生成请加 -update-persistv2-golden）: %v", err)
	}
	var want map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("解析 golden %s: %v", path, err)
	}
	assertPersistv2SnapshotEqual(t, want, snapA)
}

// TestStore_WriteBlob_DedupAndEmptyRef —— V04：blob 内容寻址去重与空内容空 ref。
func TestStore_WriteBlob_DedupAndEmptyRef(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "dedup-sess")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ref1, err := store.WriteBlob([]byte("dedup blob content"))
	if err != nil {
		t.Fatalf("WriteBlob(1): %v", err)
	}
	if !strings.HasPrefix(ref1, "sha256:") {
		t.Fatalf("unexpected blob ref: %q", ref1)
	}
	ref2, err := store.WriteBlob([]byte("dedup blob content"))
	if err != nil {
		t.Fatalf("WriteBlob(2): %v", err)
	}
	if ref1 != ref2 {
		t.Fatalf("同内容 WriteBlob 的 ref 不一致: %q vs %q", ref1, ref2)
	}

	countBlobs := func() int {
		t.Helper()
		entries, err := os.ReadDir(store.blobsDir)
		if err != nil {
			t.Fatalf("read blobs dir: %v", err)
		}
		return len(entries)
	}
	if got := countBlobs(); got != 1 {
		t.Fatalf("同内容两次 WriteBlob 后 blobs 目录应仅 1 个文件, got %d", got)
	}

	for _, data := range [][]byte{nil, {}} {
		ref, err := store.WriteBlob(data)
		if err != nil {
			t.Fatalf("WriteBlob(empty %v): %v", data == nil, err)
		}
		if ref != "" {
			t.Fatalf("空内容 WriteBlob 应返回空 ref, got %q", ref)
		}
	}
	if got := countBlobs(); got != 1 {
		t.Fatalf("空内容 WriteBlob 不应新增 blob 文件, got %d", got)
	}

	if fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("dedup blob content"))) != ref1 {
		t.Fatalf("blob ref 应为内容 sha256, got %q", ref1)
	}
}
