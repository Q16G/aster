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
	"time"

	. "aster/internal/react"
	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
)

// TestSessionArtifacts_LiveZenSingleStep 用真实 LLM（opencode zen，OpenAI 风格）
// 启动一个真正的 session 跑通一个完整 plan → step → final_answer，
// 然后断言落盘文件齐全，并把 workspace 目录树 + 关键文件正文打印出来供 review。
//
// 任务设计：固定 3 个 fixture 文件 → 列目录 → 逐个读取 → 汇总。
// 模型与 key 读取与 zen_cache 测试一致（ZEN_LIVE_API_KEY / ZEN_LIVE_KEY_FILE）。
// workspace 路径固定写入 /tmp/session_artifacts_review_live（每次重跑清空），
// 让你测试结束后还能 cd 进去看。
func TestSessionArtifacts_LiveZenSingleStep(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("ZEN_LIVE_API_KEY"))
	if key == "" {
		keyFile := strings.TrimSpace(os.Getenv("ZEN_LIVE_KEY_FILE"))
		if keyFile == "" {
			keyFile = "/tmp/zen_live_key"
		}
		if raw, err := os.ReadFile(keyFile); err == nil {
			key = strings.TrimSpace(string(raw))
		}
	}
	if key == "" {
		t.Skip("live test disabled; set ZEN_LIVE_API_KEY or ZEN_LIVE_KEY_FILE")
	}
	model := firstNonEmptyStr(strings.TrimSpace(os.Getenv("ZEN_LIVE_MODEL")), "deepseek-v4-flash-free")
	baseURL := firstNonEmptyStr(strings.TrimSpace(os.Getenv("ZEN_LIVE_BASE_URL")), "https://opencode.ai/zen/v1")
	proxy := firstNonEmptyStr(strings.TrimSpace(os.Getenv("ZEN_LIVE_PROXY")), "socks5://127.0.0.1:7890")

	// 持久化路径——每次重跑清空，让你随时进去 review。
	workspaceRoot := firstNonEmptyStr(strings.TrimSpace(os.Getenv("SESSION_LIVE_WORKSPACE")), "/tmp/session_artifacts_review_live")
	if err := os.RemoveAll(workspaceRoot); err != nil {
		t.Fatalf("clean workspace: %v", err)
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	sessionID := "live-session-artifacts"

	// 固定 fixture 文件，避免随机临时目录污染 review。
	fixturesDir := filepath.Join(workspaceRoot, "fixtures")
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	fixtures := map[string]string{
		"alpha.txt": "alpha 服务监听 8081 端口，负责用户认证。",
		"beta.txt":  "beta 服务监听 8082 端口，负责订单处理。",
		"gamma.txt": "gamma 服务监听 8083 端口，负责消息推送。",
	}
	for name, content := range fixtures {
		if err := os.WriteFile(filepath.Join(fixturesDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	rawClient := openai.NewClient(
		openai.WithURL(baseURL),
		openai.WithURLAutoComplete(true),
		openai.WithAPIKey(key),
		openai.WithModel(model),
		openai.WithProxy(proxy),
		openai.WithStream(false),
		openai.WithTimeout(180*time.Second),
		openai.WithMaxRetries(2),
		openai.WithContextWindowTokens(128000),
	)

	registry := NewDefaultToolRegistry()
	listTool, err := registry.Resolve("list_files", nil)
	if err != nil {
		t.Fatalf("resolve list_files: %v", err)
	}
	readTool, err := registry.Resolve("read_file", nil)
	if err != nil {
		t.Fatalf("resolve read_file: %v", err)
	}

	emitter := NewEmitter("session-live", "session-live", func(ev *AgentOutputEvent) error {
		if ev == nil {
			return nil
		}
		if ev.Type == EventTypeStepFinish && ev.Payload != nil {
			t.Logf("[live] step_finish family=%v cache_hit=%v",
				ev.Payload["prompt_family"], ev.Payload["cache_hit"])
		}
		return nil
	})

	agent, err := NewReActAgent(
		"session-live",
		rawClient,
		WithEmitter(emitter),
		WithMaxIterations(20),
		WithTools(listTool, readTool),
		// 写能力是 agent 的设计前提（共享区维护职责依赖 bash），live 测试同样必须具备。
		WithBashTool(&BashToolConfig{
			PermCtx: &builtin_tools.BashPermissionContext{
				Mode:        builtin_tools.PermissionModeYOLO,
				ProjectPath: workspaceRoot,
			},
		}),
		WithInstruction("你是文件分析助手，使用提供的文件工具完成任务，结论保持简洁。"),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	input := fmt.Sprintf("这是一个需要正式规划并分步执行的任务，请先 submit_plan 拆为多个步骤再逐步执行：1) 列出 %s 目录下的全部文件清单；2) 逐个读取每个文件的内容并提取服务名与端口号；3) 汇总输出一张「文件 → 服务 → 端口」对照表。要求逐文件核对。", fixturesDir)
	runResult, err := agent.Execute(ctx, input,
		WithSkipIntentPrelude(),
		WithWorkspaceSession(sessionID, workspaceRoot),
	)
	if err != nil {
		t.Fatalf("agent.Execute failed: %v", err)
	}
	if runResult != nil {
		t.Logf("[live] success=%v final=%s", runResult.Success, firstNonEmptyStr(runResult.Result, runResult.Error))
	}

	// ── 落盘文件齐全性断言（与 stub 版本相同的清单） ──
	sharedDir := filepath.Join(workspaceRoot, "shared")
	sessionDir := filepath.Join(workspaceRoot, "workspace", "sessions", sessionID)

	type check struct {
		path    string
		note    string
		mustDir bool
	}
	checks := []check{
		{filepath.Join(sharedDir, "task_context.md"), "贯穿事实板骨架", false},
		{filepath.Join(sharedDir, "open_items.md"), "未闭环账本骨架", false},
		{filepath.Join(sharedDir, "open_items_archive.md"), "已闭环归档骨架", false},
		{sessionDir, "persistv2 session 目录", true},
		{filepath.Join(sessionDir, "events.jsonl"), "事件流（append-only WAL）", false},
		{filepath.Join(sessionDir, "snapshot.json"), "状态快照（materialized view）", false},
		{filepath.Join(sessionDir, "blobs"), "blobs 目录", true},
	}
	for _, c := range checks {
		info, err := os.Stat(c.path)
		if err != nil {
			t.Errorf("[missing] %s — %s: %v", c.note, c.path, err)
			continue
		}
		if c.mustDir != info.IsDir() {
			t.Errorf("[type-mismatch] %s — want dir=%v got dir=%v", c.path, c.mustDir, info.IsDir())
		}
		if !info.IsDir() && info.Size() == 0 {
			t.Errorf("[empty] %s — %s", c.note, c.path)
		}
	}

	// ── 每个 step 子目录都得有 timeline.jsonl ──
	sharedEntries, err := os.ReadDir(sharedDir)
	if err != nil {
		t.Fatalf("read shared dir: %v", err)
	}
	stepIDs := []string{}
	for _, e := range sharedEntries {
		if !e.IsDir() {
			continue
		}
		stepIDs = append(stepIDs, e.Name())
		tlPath := filepath.Join(sharedDir, e.Name(), "timeline.jsonl")
		info, err := os.Stat(tlPath)
		if err != nil {
			t.Errorf("[missing] step timeline %s: %v", tlPath, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("[empty] step timeline %s", tlPath)
		}
	}
	if len(stepIDs) == 0 {
		t.Errorf("expected at least one step subdirectory under %s", sharedDir)
	}

	// ── 树状打印整棵 workspace ──
	t.Logf("\n══════════════════════════════════════════════════════════")
	t.Logf("workspace root : %s", workspaceRoot)
	t.Logf("session id     : %s", sessionID)
	t.Logf("shared dir     : %s", sharedDir)
	t.Logf("session dir    : %s", sessionDir)
	t.Logf("step ids       : %v", stepIDs)
	t.Logf("model          : %s", model)
	t.Logf("──────────────────────────────────────────────────────────")
	if err := walkAndLogLimited(t, workspaceRoot, workspaceRoot); err != nil {
		t.Errorf("walk: %v", err)
	}
	t.Logf("══════════════════════════════════════════════════════════")

	// ── 关键文件摘要（只看几条；详细内容直接进 workspaceRoot 看） ──
	logFileExcerpt(t, filepath.Join(sharedDir, "task_context.md"), 1200)
	logFileExcerpt(t, filepath.Join(sharedDir, "open_items.md"), 800)
	for _, stepID := range stepIDs {
		logFileExcerpt(t, filepath.Join(sharedDir, stepID, "timeline.jsonl"), 1200)
	}
	logFileExcerpt(t, filepath.Join(sessionDir, "snapshot.json"), 1600)
	logEventsHead(t, filepath.Join(sessionDir, "events.jsonl"), 12)

	t.Logf("\n[REVIEW] inspect with:")
	t.Logf("  ls -la %s", workspaceRoot)
	t.Logf("  cat  %s/snapshot.json | jq", sessionDir)
	t.Logf("  cat  %s/events.jsonl  | jq -s 'map({type,seq,turn_id})'", sessionDir)
	for _, stepID := range stepIDs {
		t.Logf("  cat  %s/%s/timeline.jsonl | jq -s '.'", sharedDir, stepID)
	}
	t.Logf("  cat  %s/task_context.md", sharedDir)
	t.Logf("  cat  %s/open_items.md", sharedDir)
}

// TestSessionArtifacts_LivePlannerInputFacts 验证 plan 阶段具备 bash 写能力时，
// planner 在用户输入回合按「共享区终态」契约把输入中的确定事实落进
// task_context.md 的 `## 输入事实` 节（提交计划前完成）。
// 模型与 key 读取与 zen_cache 测试一致（ZEN_LIVE_API_KEY / ZEN_LIVE_KEY_FILE）。
func TestSessionArtifacts_LivePlannerInputFacts(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("ZEN_LIVE_API_KEY"))
	if key == "" {
		keyFile := strings.TrimSpace(os.Getenv("ZEN_LIVE_KEY_FILE"))
		if keyFile == "" {
			keyFile = "/tmp/zen_live_key"
		}
		if raw, err := os.ReadFile(keyFile); err == nil {
			key = strings.TrimSpace(string(raw))
		}
	}
	if key == "" {
		t.Skip("live test disabled; set ZEN_LIVE_API_KEY or ZEN_LIVE_KEY_FILE")
	}
	model := firstNonEmptyStr(strings.TrimSpace(os.Getenv("ZEN_LIVE_MODEL")), "deepseek-v4-flash-free")
	baseURL := firstNonEmptyStr(strings.TrimSpace(os.Getenv("ZEN_LIVE_BASE_URL")), "https://opencode.ai/zen/v1")
	proxy := firstNonEmptyStr(strings.TrimSpace(os.Getenv("ZEN_LIVE_PROXY")), "socks5://127.0.0.1:7890")

	workspaceRoot := "/tmp/planner_input_facts_review_live"
	if err := os.RemoveAll(workspaceRoot); err != nil {
		t.Fatalf("clean workspace: %v", err)
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	sessionID := "live-planner-input-facts"

	fixturesDir := filepath.Join(workspaceRoot, "fixtures")
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixturesDir, "alpha.txt"),
		[]byte("alpha 服务监听 8081 端口，负责用户认证。"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rawClient := openai.NewClient(
		openai.WithURL(baseURL),
		openai.WithURLAutoComplete(true),
		openai.WithAPIKey(key),
		openai.WithModel(model),
		openai.WithProxy(proxy),
		openai.WithStream(false),
		openai.WithTimeout(180*time.Second),
		openai.WithMaxRetries(2),
		openai.WithContextWindowTokens(128000),
	)
	client := newDumpingChatClient(rawClient, 2000)

	registry := NewDefaultToolRegistry()
	listTool, err := registry.Resolve("list_files", nil)
	if err != nil {
		t.Fatalf("resolve list_files: %v", err)
	}
	readTool, err := registry.Resolve("read_file", nil)
	if err != nil {
		t.Fatalf("resolve read_file: %v", err)
	}

	agent, err := NewReActAgent(
		"planner-facts-live",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(20),
		WithTools(listTool, readTool),
		WithBashTool(&BashToolConfig{
			PermCtx: &builtin_tools.BashPermissionContext{
				Mode:        builtin_tools.PermissionModeYOLO,
				ProjectPath: workspaceRoot,
			},
		}),
		WithInstruction("你是文件分析助手，使用提供的工具完成任务，结论保持简洁。"),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	input := fmt.Sprintf("这是一个需要正式规划并分步执行的任务，请先 submit_plan 再执行：读取 %s 目录下的 alpha.txt，提取其中的服务名与监听端口，输出一行「服务 → 端口」结论。", fixturesDir)
	runResult, err := agent.Execute(ctx, input,
		WithSkipIntentPrelude(),
		WithWorkspaceSession(sessionID, workspaceRoot),
	)
	if err != nil {
		t.Fatalf("agent.Execute failed: %v", err)
	}
	if runResult != nil {
		t.Logf("[live] success=%v final=%s", runResult.Success, firstNonEmptyStr(runResult.Result, runResult.Error))
	}

	contextPath := filepath.Join(workspaceRoot, "shared", "task_context.md")
	raw, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read task_context.md: %v", err)
	}
	board := string(raw)
	t.Logf("[live] task_context.md:\n%s", board)

	inputFacts := extractMarkdownSection(board, "## 输入事实")
	if strings.TrimSpace(inputFacts) == "" {
		t.Errorf("planner did not maintain `## 输入事实`; board still scaffold:\n%s", board)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}
}

// extractMarkdownSection 返回 heading 与下一个同级 heading（或 EOF）之间的正文。
func extractMarkdownSection(content, heading string) string {
	idx := strings.Index(content, heading)
	if idx < 0 {
		return ""
	}
	body := content[idx+len(heading):]
	if next := strings.Index(body, "\n## "); next >= 0 {
		body = body[:next]
	}
	return body
}

// walkAndLogLimited 在 walkAndLog 基础上跳过 blob 内部细节（按 sha 命名的二进制视图无意义）。
func walkAndLogLimited(t *testing.T, root, base string) error {
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
		rel, _ := filepath.Rel(base, path)
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

// 兼容性引导：保证 encoding/json 引用不被 vet 拒掉。
var _ = json.Marshal
