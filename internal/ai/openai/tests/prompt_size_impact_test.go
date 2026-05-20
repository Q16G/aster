package openai_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/openai"

	"gopkg.in/yaml.v3"
)

type trialResult struct {
	Label   string
	Attempt int
	TTFT    time.Duration
	Total   time.Duration
	Err     error
}

func TestPromptSizeImpact_OpenCodeGo(t *testing.T) {
	baseURL, apiKey, proxy := resolveOpenCodeGoConfig(t)
	if baseURL == "" || apiKey == "" {
		t.Skip("opencode-go config not found, set ASTER_BASE_URL + ASTER_API_KEY or configure ~/.aster/")
	}

	t.Logf("Base URL: %s", baseURL)
	t.Logf("Proxy:    %s", proxy)

	smallSystemPrompt := "你是一个代码审计助手。请简洁回答。"

	browserSkillContent := `你是专业的 Web 安全浏览器自动化测试专家。你通过 agent-browser CLI 控制浏览器访问目标站点。

## 核心工具

### agent-browser CLI（通过 bash 调用）

浏览器控制：
` + "```bash" + `
agent-browser open <url> --ignore-https-errors
agent-browser snapshot -i --urls
agent-browser snapshot -i -c
agent-browser screenshot --annotate
agent-browser get url
agent-browser get title
` + "```" + `

页面交互：
` + "```bash" + `
agent-browser click @e1
agent-browser fill @e2 "test"
agent-browser find role button click --name "登录"
agent-browser find label "用户名" fill "admin"
agent-browser select @e3 "option_value"
agent-browser press Enter
agent-browser upload @e4 /path/to/file
` + "```" + `

网络流量捕获：
` + "```bash" + `
agent-browser network har start
agent-browser network har stop output.har
agent-browser network requests --json
agent-browser network requests --filter api --json
agent-browser network requests --type xhr,fetch
agent-browser network requests --method POST
agent-browser network request <requestId> --json
` + "```" + `

JS 执行与信息提取：
` + "```bash" + `
agent-browser eval "document.cookie"
agent-browser eval "JSON.stringify(localStorage)"
` + "```" + `

Cookie 与认证：
` + "```bash" + `
agent-browser cookies --json
agent-browser cookies set sessionId "abc123"
agent-browser set credentials <user> <pass>
` + "```" + `

## 工作流程

1. 启动 HAR 录制 → 访问目标 → 获取页面结构 → 截图留证
2. 遍历关键页面、填写表单、触发 API 调用
3. 停止 HAR 录制 → 分析流量 → 验证漏洞

## 工作规则

- 必须先通过浏览器交互捕获真实流量，再基于流量做安全分析
- 每次页面状态变化后重新 snapshot -i 获取最新元素树
- 所有 agent-browser 命令加 --json 获取结构化输出`

	largeSystemPrompt := smallSystemPrompt + "\n\n## Injected Skill: agent-browser\n\n" + browserSkillContent

	userMsg := "请分析以下代码的安全性：func hello() { fmt.Println(\"hello\") }"

	t.Logf("Small system prompt: %d bytes", len(smallSystemPrompt))
	t.Logf("Large system prompt: %d bytes (with agent-browser)", len(largeSystemPrompt))
	t.Logf("Delta: +%d bytes", len(largeSystemPrompt)-len(smallSystemPrompt))

	runs := 2

	doTrial := func(label, systemPrompt string, attempt int) trialResult {
		var mu sync.Mutex
		var ttft time.Duration
		ttftRecorded := false
		start := time.Now()

		opts := []openai.Option{
			openai.WithURL(baseURL),
			openai.WithAPIKey(apiKey),
			openai.WithModel("deepseek-v4-flash"),
			openai.WithURLAutoComplete(true),
			openai.WithStream(true),
			openai.WithTimeout(60 * time.Second),
			openai.WithMaxRetries(0),
			openai.WithStreamFunc(func(event *openai.StreamEvent) {
				mu.Lock()
				defer mu.Unlock()
				if !ttftRecorded && (event.Content != "" || event.ReasonContent != "") {
					ttft = time.Since(start)
					ttftRecorded = true
				}
			}),
		}
		if proxy != "" {
			opts = append(opts, openai.WithProxy(proxy))
		}

		client := openai.NewClient(opts...)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		msgs := []*ai.MsgInfo{
			ai.NewSystemMsgInfo(systemPrompt),
			ai.NewUserMsgInfo(userMsg),
		}
		_, err := client.ChatExWithOptions(ctx, msgs, nil)
		total := time.Since(start)

		return trialResult{
			Label:   label,
			Attempt: attempt,
			TTFT:    ttft,
			Total:   total,
			Err:     err,
		}
	}

	t.Logf("")
	t.Logf("=== Running %d trials per group ===", runs)

	var results []trialResult
	for i := 0; i < runs; i++ {
		r := doTrial("small", smallSystemPrompt, i+1)
		t.Logf("[small #%d] TTFT=%v Total=%v Err=%v", i+1, r.TTFT, r.Total, r.Err)
		results = append(results, r)

		time.Sleep(1 * time.Second)

		r = doTrial("large", largeSystemPrompt, i+1)
		t.Logf("[large #%d] TTFT=%v Total=%v Err=%v", i+1, r.TTFT, r.Total, r.Err)
		results = append(results, r)

		if i < runs-1 {
			time.Sleep(2 * time.Second)
		}
	}

	t.Logf("")
	t.Logf("=== Summary ===")

	var smallTTFT, largeTTFT, smallTotal, largeTotal time.Duration
	var smallOK, largeOK int
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		switch r.Label {
		case "small":
			smallTTFT += r.TTFT
			smallTotal += r.Total
			smallOK++
		case "large":
			largeTTFT += r.TTFT
			largeTotal += r.Total
			largeOK++
		}
	}

	if smallOK > 0 {
		t.Logf("Small prompt: avg TTFT=%v, avg Total=%v (%d/%d succeeded)",
			smallTTFT/time.Duration(smallOK), smallTotal/time.Duration(smallOK), smallOK, runs)
	}
	if largeOK > 0 {
		t.Logf("Large prompt: avg TTFT=%v, avg Total=%v (%d/%d succeeded)",
			largeTTFT/time.Duration(largeOK), largeTotal/time.Duration(largeOK), largeOK, runs)
	}
	if smallOK > 0 && largeOK > 0 {
		avgSmallTTFT := smallTTFT / time.Duration(smallOK)
		avgLargeTTFT := largeTTFT / time.Duration(largeOK)
		avgSmallTotal := smallTotal / time.Duration(smallOK)
		avgLargeTotal := largeTotal / time.Duration(largeOK)
		t.Logf("Delta: TTFT %+v, Total %+v", avgLargeTTFT-avgSmallTTFT, avgLargeTotal-avgSmallTotal)
	}
	if largeOK == 0 && smallOK > 0 {
		t.Logf("FINDING: Large prompts consistently fail while small prompts succeed — " +
			"prompt bloat IS causing the API to fail for this endpoint")
	}
	if largeOK == 0 && smallOK == 0 {
		t.Logf("FINDING: Both prompt sizes fail — API connectivity issue, not prompt size related")
	}
}

func resolveOpenCodeGoConfig(t *testing.T) (baseURL, apiKey, proxy string) {
	t.Helper()

	if u := os.Getenv("ASTER_BASE_URL"); u != "" {
		baseURL = u
	}
	if k := os.Getenv("ASTER_API_KEY"); k != "" {
		apiKey = k
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return
	}

	cfgPath := filepath.Join(homeDir, ".aster", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err == nil {
		var cfg struct {
			DefaultProvider string `yaml:"default_provider"`
			Providers       map[string]struct {
				BaseURL string            `yaml:"base_url"`
				APIKey  string            `yaml:"api_key"`
				Env     map[string]string `yaml:"env"`
			} `yaml:"providers"`
		}
		if yaml.Unmarshal(data, &cfg) == nil {
			providerName := cfg.DefaultProvider
			if providerName == "" {
				providerName = "opencode-go"
			}
			if p, ok := cfg.Providers[providerName]; ok {
				if baseURL == "" {
					baseURL = p.BaseURL
				}
				if apiKey == "" && p.APIKey != "" {
					apiKey = expandEnv(p.APIKey)
				}
				if p.Env != nil && proxy == "" {
					proxy = p.Env["HTTPS_PROXY"]
				}
			}
		}
	}

	if apiKey == "" {
		credPath := filepath.Join(homeDir, ".aster", "credentials.yaml")
		data, err := os.ReadFile(credPath)
		if err == nil {
			var creds map[string]string
			if yaml.Unmarshal(data, &creds) == nil {
				providerNames := []string{"opencode-go", "opencode", "openai"}
				for _, name := range providerNames {
					if k, ok := creds[name]; ok && k != "" {
						apiKey = k
						break
					}
				}
			}
		}
	}

	return
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envName := s[2 : len(s)-1]
		if v := os.Getenv(envName); v != "" {
			return v
		}
	}
	return s
}
