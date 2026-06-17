package react_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

// TestSessionArtifacts_SingleStepLandsAllFiles 启动 session 跑通一个单 step 任务，
// 断言所有期望的落盘文件都齐全（session 目录 + step 目录 + 共享区骨架），
// 并把整棵 session 目录树 + 关键文件正文摘要打印出来供人工 review。
//
// 落盘布局：
//
//	<workspace>/                                     ← 临时根
//	├── shared/                                       ← 共享工作区（task_context.md / open_items.md 等）
//	│   ├── task_context.md                           ← 贯穿事实板骨架
//	│   ├── open_items.md                             ← 未闭环账本骨架（单文件三区）
//	│   └── <step-id>/                                ← per-step 子目录
//	│       └── timeline.jsonl                        ← 逐条 tool_call 事件日志
//	└── workspace/sessions/<session-id>/              ← persistv2 session
//	    ├── events.jsonl                              ← append-only WAL
//	    ├── snapshot.json                             ← materialized view
//	    └── blobs/                                    ← runtime/history 大体量产物指针
//
// 该测试不依赖外部 LLM，用 stub client 驱动一次完整 plan → step → final_answer。
func TestSessionArtifacts_SingleStepLandsAllFiles(t *testing.T) {
	workspaceRoot := t.TempDir()
	sessionID := "session-artifacts-test"

	client := &executeModelTestClient{
		replies: []executeModelReply{
			// step phase: 模型调用 list_files 工具
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-list", "list_files", map[string]any{
						"path": filepath.Join(workspaceRoot, "shared"),
					}),
				},
			},
			// step phase: 模型再调一次 update_current_step 提交终态
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-finish", "update_current_step", map[string]any{
						"status":        "completed",
						"summary":       "已列出共享区文件",
						"short_summary": "list_files 完成",
						"key_facts":     []string{"shared 目录已枚举"},
						"coverage_checklist": []map[string]any{
							{"item": "枚举共享区", "status": "verified"},
						},
					}),
				},
			},
			// final_answer
			{
				content: `{"is_complete":true,"status":"completed","reason":"single-step task done","should_replan":false,"next_goal":"","incomplete_items":[],"depth_gaps":[],"new_surfaces":[],"warnings":[],"user_message":"已完成共享区文件枚举。","references":[]}`,
			},
		},
	}

	registry := NewDefaultToolRegistry()
	listTool, err := registry.Resolve("list_files", nil)
	if err != nil {
		t.Fatalf("resolve list_files: %v", err)
	}

	agent, err := NewReActAgent(
		"session-artifacts-test",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTools(listTool),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning:     true,
				GoalUnderstanding: "核心目标：枚举共享区文件。范围边界：仅列出。",
				Plan: []*builtin_tools.PlanItem{
					{ID: "s1", Step: "列出共享区文件", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "请列出共享区下的文件清单。",
		WithSkipIntentPrelude(),
		WithWorkspaceSession(sessionID, workspaceRoot),
	)
	if err != nil {
		t.Fatalf("agent.Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	// ── 落盘文件齐全性断言 ──
	sharedDir := filepath.Join(workspaceRoot, "shared")
	stepDir := filepath.Join(sharedDir, "s1")
	sessionDir := filepath.Join(workspaceRoot, "workspace", "sessions", sessionID)
	type check struct {
		path     string
		mustNote string
		mustDir  bool
	}
	checks := []check{
		{filepath.Join(sharedDir, "task_context.md"), "贯穿事实板骨架", false},
		{filepath.Join(sharedDir, "open_items.md"), "未闭环账本骨架（单文件三区）", false},
		{stepDir, "step 子目录", true},
		{filepath.Join(stepDir, "timeline.jsonl"), "step timeline 事件日志（逐条 tool_call）", false},
		{sessionDir, "persistv2 session 目录", true},
		{filepath.Join(sessionDir, "events.jsonl"), "事件流（append-only WAL）", false},
		{filepath.Join(sessionDir, "snapshot.json"), "状态快照（materialized view）", false},
		{filepath.Join(sessionDir, "blobs"), "blobs 目录（runtime/history 大体量产物指针存储）", true},
	}
	for _, c := range checks {
		info, err := os.Stat(c.path)
		if err != nil {
			t.Errorf("[missing] %s — %s: %v", c.mustNote, c.path, err)
			continue
		}
		if c.mustDir != info.IsDir() {
			t.Errorf("[type-mismatch] %s — want dir=%v got dir=%v", c.path, c.mustDir, info.IsDir())
			continue
		}
		if !info.IsDir() && info.Size() == 0 {
			t.Errorf("[empty] %s — %s", c.mustNote, c.path)
		}
	}

	// ── 把整棵 workspace 目录树打印出来供 review ──
	t.Logf("\n══════════════════════════════════════════════════════════")
	t.Logf("workspace root : %s", workspaceRoot)
	t.Logf("session id     : %s", sessionID)
	t.Logf("shared dir     : %s", sharedDir)
	t.Logf("step dir       : %s", stepDir)
	t.Logf("session dir    : %s", sessionDir)
	t.Logf("──────────────────────────────────────────────────────────")
	if err := walkAndLog(t, workspaceRoot); err != nil {
		t.Errorf("walk failed: %v", err)
	}
	t.Logf("══════════════════════════════════════════════════════════")

	// ── 关键文件正文摘要（便于直接读出来对照） ──
	logFileExcerpt(t, filepath.Join(sharedDir, "task_context.md"), 600)
	logFileExcerpt(t, filepath.Join(sharedDir, "open_items.md"), 600)
	logFileExcerpt(t, filepath.Join(stepDir, "timeline.jsonl"), 1600)
	logFileExcerpt(t, filepath.Join(sessionDir, "snapshot.json"), 1600)
	logEventsHead(t, filepath.Join(sessionDir, "events.jsonl"), 8)

	// ── 把绝对路径回填到测试输出顶部，方便 cd 进去手动 review ──
	t.Logf("\n[REVIEW] inspect with:")
	t.Logf("  ls -la %s", workspaceRoot)
	t.Logf("  cat  %s/snapshot.json | jq", sessionDir)
	t.Logf("  cat  %s/events.jsonl  | tail -n 20", sessionDir)
	t.Logf("  cat  %s/timeline.jsonl", stepDir)
	t.Logf("  cat  %s/task_context.md", sharedDir)
	t.Logf("  cat  %s/open_items.md", sharedDir)

	// ── 同时把整棵树复制到一个稳定路径，让你 t.TempDir 清理后还能看 ──
	persistDir := strings.TrimSpace(os.Getenv("SESSION_ARTIFACTS_PERSIST_DIR"))
	if persistDir == "" {
		persistDir = "/tmp/session_artifacts_review"
	}
	if err := os.RemoveAll(persistDir); err != nil {
		t.Logf("persist clean failed: %v", err)
	} else if err := copyDir(workspaceRoot, persistDir); err != nil {
		t.Logf("persist copy failed: %v", err)
	} else {
		t.Logf("\n[REVIEW] persisted snapshot of workspace at:\n  %s", persistDir)
		t.Logf("  ls -la %s", persistDir)
	}
}

// walkAndLog 递归打印目录树（相对路径 + size）。
func walkAndLog(t *testing.T, root string) error {
	type entry struct {
		rel  string
		size int64
		dir  bool
	}
	var entries []entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		info, _ := d.Info()
		entries = append(entries, entry{rel: rel, size: info.Size(), dir: d.IsDir()})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	for _, e := range entries {
		if e.dir {
			t.Logf("  📁 %s/", e.rel)
		} else {
			t.Logf("  📄 %s  (%dB)", e.rel, e.size)
		}
	}
	return nil
}

func logFileExcerpt(t *testing.T, path string, max int) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("\n── %s ──\n  (cannot read: %v)", path, err)
		return
	}
	text := string(data)
	if len(text) > max {
		text = text[:max] + fmt.Sprintf("\n…(%dB more)…", len(data)-max)
	}
	t.Logf("\n── %s (%dB) ──\n%s", path, len(data), text)
}

func logEventsHead(t *testing.T, path string, n int) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("\n── %s ──\n  (cannot read: %v)", path, err)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	t.Logf("\n── %s  (%d events) ──", path, len(lines))
	limit := n
	if limit > len(lines) {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		var compact map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &compact); err == nil {
			t.Logf("  event[%02d] type=%v seq=%v turn=%v",
				i, compact["type"], compact["seq"], compact["turn_id"])
		} else {
			t.Logf("  event[%02d] %s", i, truncateLine(lines[i], 200))
		}
	}
}

func truncateLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
