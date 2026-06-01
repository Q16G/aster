package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/anthropic"
)

// TestMinimaxCN_FinalRequestURL 验证 minimax-cn 的 base_url 经过 anthropic 客户端
// 拼接后实际命中的请求路径。默认开启 URLAutoComplete，不含 /messages 的 base_url
// 会被自动补全。
func TestMinimaxCN_FinalRequestURL(t *testing.T) {
	cases := []struct {
		name        string
		baseURLPath string // 模拟 ps.BaseURL 的 path 部分
		wantPath    string // 期望服务器实际收到的 path
	}{
		{
			name:        "registry-default (.../anthropic/v1) 自动补全 /messages",
			baseURLPath: "/anthropic/v1",
			wantPath:    "/anthropic/v1/messages",
		},
		{
			name:        "已带 /messages 时幂等不变",
			baseURLPath: "/anthropic/v1/messages",
			wantPath:    "/anthropic/v1/messages",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":          "msg_1",
					"type":        "message",
					"role":        "assistant",
					"stop_reason": "end_turn",
					"content":     []map[string]any{{"type": "text", "text": "ok"}},
				})
			}))
			defer server.Close()

			// 对应 cmd/aster/main.go newAnthropicClient 里 WithURL(ps.BaseURL) 的行为，
			// ps.BaseURL = "https://api.minimaxi.com/anthropic/v1"（来自注册表）。
			client := anthropic.NewClient(
				anthropic.WithURL(server.URL+tc.baseURLPath),
				anthropic.WithAPIKey("test-key"),
				anthropic.WithModel("MiniMax-M2"),
				anthropic.WithTimeout(5*time.Second),
			)

			_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
				ai.NewUserMsgInfo("hello"),
			})
			if err != nil {
				t.Fatalf("ChatEx failed: %v", err)
			}

			t.Logf("base_url path=%q  =>  实际请求 path=%q", tc.baseURLPath, gotPath)
			if gotPath != tc.wantPath {
				t.Fatalf("URL 拼接结果不符: got %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

// TestAnthropic_URLAutoCompleteDisabled 验证关闭补全后 URL 原样使用。
func TestAnthropic_URLAutoCompleteDisabled(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_1",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL+"/custom/gateway"),
		anthropic.WithURLAutoComplete(false),
		anthropic.WithModel("MiniMax-M2"),
		anthropic.WithTimeout(5*time.Second),
	)

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
		ai.NewUserMsgInfo("hello"),
	})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if gotPath != "/custom/gateway" {
		t.Fatalf("关闭补全后应原样请求: got %q", gotPath)
	}
}
