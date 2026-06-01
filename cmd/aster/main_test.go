package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/provider"
	"aster/internal/react"
	"aster/internal/tui"
)

// TestProtocolSelection_EndToEndURL 贯穿全链路验证：config 里手写 protocol（anthropic /
// openai）或不写时，经 ResolveProviderState -> newProviderClient（main.go 的 protocol
// switch）-> 对应客户端后，实际 POST 命中的 URL。每种情况都打印出来供 review。
func TestProtocolSelection_EndToEndURL(t *testing.T) {
	data, err := provider.LoadBundledSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	reg := provider.NewRegistry("")
	reg.LoadFromModelsDevData(data)

	cases := []struct {
		name         string
		providerName string
		protocolLine string // config 里的 protocol 行，空表示不写
		basePath     string // 用户配置的 base_url path 部分（故意不含接口路径）
		wantProtocol string
		wantPath     string
	}{
		{
			name:         "手写 anthropic（注册表未知 provider）",
			providerName: "my-gw",
			protocolLine: "    protocol: anthropic\n",
			basePath:     "/anthropic/v1",
			wantProtocol: "anthropic",
			wantPath:     "/anthropic/v1/messages",
		},
		{
			name:         "手写 openai（注册表未知 provider）",
			providerName: "my-gw",
			protocolLine: "    protocol: openai\n",
			basePath:     "/v1",
			wantProtocol: "openai-compatible",
			wantPath:     "/v1/chat/completions",
		},
		{
			name:         "不写 + 注册表已知 minimax-cn",
			providerName: "minimax-cn",
			protocolLine: "",
			basePath:     "/anthropic/v1",
			wantProtocol: "anthropic",
			wantPath:     "/anthropic/v1/messages",
		},
		{
			name:         "不写 + 注册表未知 provider（默认 openai）",
			providerName: "my-gw",
			protocolLine: "",
			basePath:     "/v1",
			wantProtocol: "",
			wantPath:     "/v1/chat/completions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				if strings.Contains(r.URL.Path, "/chat/completions") {
					// openai 客户端走 SSE 流式，返回合法 SSE 让其正常解析、避免重试退避。
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
					io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
					io.WriteString(w, "data: [DONE]\n\n")
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":          "msg_1",
					"type":        "message",
					"role":        "assistant",
					"stop_reason": "end_turn",
					"content":     []map[string]any{{"type": "text", "text": "ok"}},
				})
			}))
			defer server.Close()

			configYAML := "default_provider: " + tc.providerName + "\n" +
				"providers:\n" +
				"  " + tc.providerName + ":\n" +
				tc.protocolLine +
				"    base_url: " + server.URL + tc.basePath + "\n" +
				"    api_key: test-key\n" +
				"    default_model: MiniMax-M2\n"

			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(configYAML), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			cfg, err := tui.LoadConfig(path)
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			ps := cfg.ResolveProviderState("", "", "", "", reg, nil)
			if ps == nil {
				t.Fatal("nil provider state")
			}
			if ps.Protocol != tc.wantProtocol {
				t.Fatalf("Protocol 解析不符: got %q, want %q", ps.Protocol, tc.wantProtocol)
			}

			client := newProviderClient(ps, reg, nil, "")
			// URL 在请求发出时即被服务器捕获；openai 客户端可能因响应体非 SSE 而报错，忽略。
			_, _ = client.ChatEx(context.Background(), []*ai.MsgInfo{
				ai.NewUserMsgInfo("hello"),
			})

			t.Logf("Protocol=%-18q  配置 base_url=%s%s", ps.Protocol, server.URL, tc.basePath)
			t.Logf("           最终 POST URL = %s%s", server.URL, gotPath)
			if gotPath != tc.wantPath {
				t.Fatalf("最终 URL path 不符: got %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestChooseDefaultAgentDefinition_PrefersCodeAudit(t *testing.T) {
	profiles := []react.AgentDefinition{
		{Name: "example"},
		{Name: "code-audit"},
		{Name: "other"},
	}
	got := chooseDefaultAgentDefinition(profiles, "")
	if got.Name != "code-audit" {
		t.Fatalf("expected code-audit, got %q", got.Name)
	}
}

func TestChooseDefaultAgentDefinition_FallsBackToFirst(t *testing.T) {
	profiles := []react.AgentDefinition{
		{Name: "example"},
		{Name: "other"},
	}
	got := chooseDefaultAgentDefinition(profiles, "")
	if got.Name != "example" {
		t.Fatalf("expected fallback to first (example), got %q", got.Name)
	}
}

