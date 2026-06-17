package anthropic_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/anthropic"
)

// dumpRoundTripper 打印每次请求的原始请求体与原始响应包(状态码 + body),
// 然后把 body 还原给客户端继续解析。用于端到端测试时直接看服务端返回。
type dumpRoundTripper struct {
	t    *testing.T
	base http.RoundTripper
}

func (d *dumpRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		reqBytes, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBytes))
		req.ContentLength = int64(len(reqBytes))
		d.t.Logf("→ 请求 %s %s\n%s", req.Method, req.URL.String(), string(reqBytes))
	}
	resp, err := d.base.RoundTrip(req)
	if err != nil {
		d.t.Logf("← 传输错误: %v", err)
		return nil, err
	}
	respBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBytes))
	d.t.Logf("← 响应 HTTP %d\n%s", resp.StatusCode, string(respBytes))
	return resp, nil
}

// 端到端冒烟测试:真实访问 Highway 网关的 Anthropic 原生接口,验证修复后的客户端
// 在"多工具 + prompt cache"场景下能被真实端点正常受理(200),且不出现
// cache_control 断点超限错误。
//
// 说明:用户最初提供的 Base URL https://api.highwayapi.ai/openai + Endpoint
// /chat/completions/anthropic 实测 404(且 /openai/* 是 OpenAI 格式,不会走到
// anthropic 的 cache_control 代码路径)。经探测,可用的 Anthropic 原生端点为:
//
//	URL  ：https://api.highwayapi.ai/anthropic/v1/messages
//	鉴权 ：x-api-key + anthropic-version
//	Model：claude-sonnet-4-5-20250929
//
// 注意:该网关对断点上限并不严格(7 断点也可能 200),所以"必触发 400"的负向
// 保证由单元测试 TestBuildRequestBody_CacheControlBreakpointsWithinLimit 承担;
// 本测试只做正向冒烟。网关偶发 504,已通过重试缓解。
//
// 运行方式:通过环境变量提供 key(不要把真实 key 写进源码提交):
//
//	HIGHWAY_API_KEY=xxxx go test ./internal/ai/anthropic/ -run E2E -v
//
// 未提供 key 时该测试自动跳过,不影响普通 go test。
const (
	e2eBaseURL  = "https://api.highwayapi.ai/anthropic/v1"
	e2eEndpoint = "/messages"
	e2eModel    = "claude-sonnet-4-5-20250929"
)

func resolveE2EKey() string {
	return strings.TrimSpace(os.Getenv("HIGHWAY_API_KEY"))
}

func e2eTool(name, desc string) *ai.FunctionTool {
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        name,
			Description: desc,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "target path"},
				},
			},
		},
	}
}

func TestE2E_Highway_CacheControlUnderLimit(t *testing.T) {
	key := resolveE2EKey()
	if key == "" {
		t.Skip("跳过端到端测试:未设置环境变量 HIGHWAY_API_KEY")
	}

	httpClient := &http.Client{
		Timeout:   60 * time.Second,
		Transport: &dumpRoundTripper{t: t, base: http.DefaultTransport},
	}

	client := anthropic.NewClient(
		anthropic.WithURL(e2eBaseURL+e2eEndpoint),
		// URL 已是 .../messages,关闭自动补全避免重复追加。
		anthropic.WithURLAutoComplete(false),
		anthropic.WithAPIKey(key),
		anthropic.WithModel(e2eModel),
		anthropic.WithMaxTokens(256),
		// 网关偶发 504,留几次重试。
		anthropic.WithMaxRetries(3),
		anthropic.WithTimeout(60*time.Second),
		anthropic.WithHTTPClient(httpClient),
	)

	// 修复前:6 个工具各打断点 + system 1 个 = 7 个 cache_control 断点。
	// 修复后:只在最后一个工具打断点,固定为 system(1) + tools(1) = 2 个断点。
	tools := []*ai.FunctionTool{
		e2eTool("read_file", "read a file"),
		e2eTool("list_files", "list files in a directory"),
		e2eTool("rg", "ripgrep search"),
		e2eTool("bash", "run a shell command"),
		e2eTool("submit_plan", "submit the plan"),
		e2eTool("request_clarification", "ask the user a question"),
	}

	msgs := []*ai.MsgInfo{
		ai.NewSystemMsgInfo("你是一个代码审计助手,请严格按规则工作。\n<CURRENT_STEP>\n当前步骤:确认环境。"),
		ai.NewUserMsgInfo("只回复两个字:已就绪。"),
	}

	opts := &ai.RequestOptions{
		PromptFamily:         "think_act",
		PromptCacheEnabled:   true,
		PromptCacheRetention: "5m",
	}

	choices, err := client.ChatExWithOptions(context.Background(), msgs, opts, tools...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "cache_control") {
			t.Fatalf("修复回归:仍触发 cache_control 断点超限错误:%v", err)
		}
		t.Fatalf("端到端请求失败(非 cache_control 问题,可能是 key/网络/配额):%v", err)
	}

	if len(choices) == 0 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("响应为空:%#v", choices)
	}
	t.Logf("✅ 请求成功,未触发 cache_control 400")
	t.Logf("模型回复:%v", choices[0].Message.Content)
	if u := choices[0].Usage; u != nil {
		t.Logf("token 用量:in=%d out=%d cache_read=%d cache_write=%d",
			u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}
}
