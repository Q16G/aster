package react_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

// TestZenLive_PromptSplitCacheProfile 用真实 LLM（opencode zen，OpenAI 请求风格）
// 验证 prompt 拆分后的端到端行为：
//  1. 新管线（system×2 + 首条 user message + stepHistory，openai client 合并 leading system）
//     能跑通完整 plan→step→replan→final_answer 流程；
//  2. 同一 prompt_family 的多轮调用 cache_key_hash 稳定（system 字节稳定的外显）；
//  3. 观测每次调用的 usage 与 cache_hit（DeepSeek 类后端经
//     prompt_tokens_details.cached_tokens 透传前缀缓存命中）。
//
// 运行方式（key 不入源码）：
//
//	ZEN_LIVE_API_KEY=sk-xxx go test ./internal/react/tests/ -run TestZenLive -v -timeout 600s
//
// 或把 key 写入 ZEN_LIVE_KEY_FILE 指向的文件（默认 /tmp/zen_live_key）。
func TestZenLive_PromptSplitCacheProfile(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1 (then 也需 ZEN_LIVE_API_KEY 或 ZEN_LIVE_KEY_FILE)")
	}
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
	model := strings.TrimSpace(os.Getenv("ZEN_LIVE_MODEL"))
	if model == "" {
		model = "deepseek-v4-flash-free"
	}
	baseURL := strings.TrimSpace(os.Getenv("ZEN_LIVE_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://opencode.ai/zen/v1"
	}
	proxy := os.Getenv("ZEN_LIVE_PROXY")
	if proxy == "" {
		proxy = "socks5://127.0.0.1:7890"
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
	// dump 包装：每次模型调用前把出站 messages/tools/options 渲染成
	// OpenAI body 紧凑视图写入 ZEN_LIVE_DUMP_FILE（默认 /tmp/zen_live_dump.log），
	// 便于人工 review 多轮请求的差异（system 是否稳定、user 是否冻结、history 增长）。
	maxBody := 1200
	if raw := strings.TrimSpace(os.Getenv("ZEN_LIVE_DUMP_MAX_BODY")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxBody = n
		}
	}
	client := newDumpingChatClient(rawClient, maxBody)
	t.Logf("[zen-live] dump file=%s max_body=%d", firstNonEmptyStr(os.Getenv("ZEN_LIVE_DUMP_FILE"), "/tmp/zen_live_dump.log"), maxBody)

	// 准备一个需要多步执行的小任务现场。
	workDir := t.TempDir()
	files := map[string]string{
		"alpha.txt": "alpha 服务监听 8081 端口，负责用户认证。",
		"beta.txt":  "beta 服务监听 8082 端口，负责订单处理。",
		"gamma.txt": "gamma 服务监听 8083 端口，负责消息推送。",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	// 捕获每次模型调用的 step_finish profile（prompt_family / cache_key_hash / usage / cache_hit）。
	type callProfile struct {
		family   string
		keyHash  string
		cacheHit bool
		usage    map[string]int
	}
	var mu sync.Mutex
	var calls []callProfile
	emitter := NewEmitter("zen-cache-live", "zen-cache-live", func(ev *AgentOutputEvent) error {
		if ev == nil || ev.Type != EventTypeStepFinish || ev.Payload == nil {
			return nil
		}
		profile := callProfile{}
		profile.family, _ = ev.Payload["prompt_family"].(string)
		profile.keyHash, _ = ev.Payload["cache_key_hash"].(string)
		profile.cacheHit, _ = ev.Payload["cache_hit"].(bool)
		profile.usage, _ = ev.Payload["usage"].(map[string]int)
		mu.Lock()
		calls = append(calls, profile)
		mu.Unlock()
		return nil
	})

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
		"zen-cache-live",
		client,
		WithEmitter(emitter),
		WithMaxIterations(28),
		WithTools(listTool, readTool),
		// 写入能力是 agent 的设计前提，live 测试同样必须具备。
		WithBashTool(&BashToolConfig{
			PermCtx: &builtin_tools.BashPermissionContext{
				Mode:        builtin_tools.PermissionModeYOLO,
				ProjectPath: workDir,
			},
		}),
		WithInstruction("你是文件分析助手，使用提供的文件工具完成任务，结论保持简洁。"),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	input := fmt.Sprintf("这是一个需要正式规划并分步执行的任务，请先 submit_plan 拆为多个步骤再逐步执行：1) 列出 %s 目录下的全部文件清单；2) 逐个读取每个文件的内容并提取服务名与端口号；3) 汇总输出一张「文件 → 服务 → 端口」对照表。要求逐文件核对，不要在规划阶段直接给出答案。", workDir)
	runResult, err := agent.Execute(ctx, input, WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("agent.Execute failed: %v", err)
	}
	// 先输出观测数据，再做成功性断言（避免预算耗尽时丢失缓存统计）。
	if runResult != nil {
		t.Logf("[zen-live] success=%v final=%s", runResult.Success, firstNonEmptyStr(runResult.Result, runResult.Error))
	}

	// ── 验证与观测 ──
	mu.Lock()
	defer mu.Unlock()
	if len(calls) < 3 {
		t.Fatalf("expected >=3 model calls across phases, got %d", len(calls))
	}

	hashByFamily := map[string]map[string]int{}
	cacheHits := 0
	for i, c := range calls {
		raw, _ := json.Marshal(c.usage)
		t.Logf("[zen-live] call#%02d family=%-13s key_hash=%.12s cache_hit=%v usage=%s",
			i, c.family, c.keyHash, c.cacheHit, string(raw))
		if c.family == "" || c.keyHash == "" {
			t.Errorf("call#%d missing prompt_family/cache_key_hash", i)
			continue
		}
		if hashByFamily[c.family] == nil {
			hashByFamily[c.family] = map[string]int{}
		}
		hashByFamily[c.family][c.keyHash]++
		if c.cacheHit {
			cacheHits++
		}
	}

	// 同一 family 的 cache_key_hash 必须稳定（system 全文哈希；工具集不变时唯一）。
	for family, hashes := range hashByFamily {
		if len(hashes) != 1 {
			t.Errorf("family %s has %d distinct cache key hashes (system prefix unstable): %v", family, len(hashes), hashes)
		}
	}

	t.Logf("[zen-live] total_calls=%d cache_hits=%d families=%d", len(calls), cacheHits, len(hashByFamily))
	if cacheHits == 0 {
		// 后端缓存命中受网关/模型支持度影响，不作硬断言，但在日志中显式标注。
		t.Logf("[zen-live] WARNING: no backend cache hit observed (gateway may not propagate cached_tokens)")
	}

	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
