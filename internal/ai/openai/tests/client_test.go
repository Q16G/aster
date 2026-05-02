package openai_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"aster/internal/ai/openai"
)

func TestOpenAIClient(t *testing.T) {
	baseURL := "" // 填入真实地址，如 "https://api.openai.com/v1"
	apiKey := ""  // 填入真实 API Key
	model := ""   // 填入模型名，如 "gpt-4"

	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("请填入真实的 baseURL, apiKey, model 后运行测试")
	}

	client := openai.NewClient(
		openai.WithURL(baseURL),
		openai.WithAPIKey(apiKey),
		openai.WithModel(model),
		//openai.WithProxy("http://127.0.0.1:8083"),
		openai.WithTimeout(60*time.Second),
		openai.WithStream(true),
		openai.WithStreamFunc(func(event *openai.StreamEvent) {
			if event.ReasonContent != "" {
				fmt.Print("[Reason] ", event.ReasonContent)
			}
			if event.Content != "" {
				fmt.Print(event.Content)
			}
			if event.Done {
				fmt.Println("\n[Done]")
			}
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := client.ChatText(ctx, "你好，请用一句话介绍你自己")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}

	fmt.Println("\n--- Final Response ---")
	fmt.Println(resp)
}
