package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/openai"
)

func TestChatExWithOptions_SendsPromptCacheFieldsAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test-Header"); got != "trace-123" {
			t.Fatalf("unexpected header X-Test-Header: %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if got := req["prompt_cache_key"]; got != "cache-key-1" {
			t.Fatalf("unexpected prompt_cache_key: %#v", got)
		}
		if got := req["prompt_cache_retention"]; got != "24h" {
			t.Fatalf("unexpected prompt_cache_retention: %#v", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := openai.NewClient(
		openai.WithURL(server.URL+"/v1/chat/completions"),
		openai.WithURLAutoComplete(false),
		openai.WithTimeout(5*time.Second),
		openai.WithStream(false),
		openai.WithHeaders(map[string]string{
			"X-Test-Header": "trace-123",
		}),
	)

	_, err := client.ChatExWithOptions(context.Background(), []*ai.MsgInfo{
		ai.NewUserMsgInfo("hello"),
	}, &ai.RequestOptions{
		PromptFamily:         "think_act",
		PromptCacheEnabled:   true,
		PromptCacheKey:       "cache-key-1",
		PromptCacheRetention: "24h",
	})
	if err != nil {
		t.Fatalf("ChatExWithOptions failed: %v", err)
	}
}

func TestChatExWithOptions_OmitsPromptCacheFieldsWhenDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if _, ok := req["prompt_cache_key"]; ok {
			t.Fatalf("did not expect prompt_cache_key when cache disabled: %#v", req)
		}
		if _, ok := req["prompt_cache_retention"]; ok {
			t.Fatalf("did not expect prompt_cache_retention when cache disabled: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := openai.NewClient(
		openai.WithURL(server.URL+"/v1/chat/completions"),
		openai.WithURLAutoComplete(false),
		openai.WithTimeout(5*time.Second),
		openai.WithStream(false),
	)

	_, err := client.ChatExWithOptions(context.Background(), []*ai.MsgInfo{
		ai.NewUserMsgInfo("hello"),
	}, &ai.RequestOptions{
		PromptFamily: "think_act",
	})
	if err != nil {
		t.Fatalf("ChatExWithOptions failed: %v", err)
	}
}
