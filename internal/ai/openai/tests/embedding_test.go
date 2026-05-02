package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"aster/internal/ai/openai"
)

func TestEmbedding_ParsesOpenAIStyleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2, 0.3}},
				{"embedding": []float32{0.4, 0.5, 0.6}},
			},
		})
	}))
	defer server.Close()

	client := openai.NewClient(
		openai.WithURL(server.URL+"/v1/embeddings"),
		openai.WithURLAutoComplete(false),
		openai.WithTimeout(5*time.Second),
		openai.WithStream(false),
	)

	got, err := client.Embedding(context.Background(), []string{"a", "b"}, "test-model")
	if err != nil {
		t.Fatalf("Embedding failed: %v", err)
	}
	want := [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected embeddings: got=%#v want=%#v", got, want)
	}
}

func TestEmbedding_ParsesOllamaStyleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "bge-m3",
			"embeddings": [][]float32{
				{0.01, 0.02, 0.03},
			},
			"total_duration":    123,
			"load_duration":     45,
			"prompt_eval_count": 6,
		})
	}))
	defer server.Close()

	client := openai.NewClient(
		openai.WithURL(server.URL+"/api/embed"),
		openai.WithURLAutoComplete(false),
		openai.WithTimeout(5*time.Second),
		openai.WithStream(false),
	)

	got, err := client.Embedding(context.Background(), []string{"a"}, "bge-m3")
	if err != nil {
		t.Fatalf("Embedding failed: %v", err)
	}
	want := [][]float32{
		{0.01, 0.02, 0.03},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected embeddings: got=%#v want=%#v", got, want)
	}
}

func TestEmbedding_MergesExtraBodyIntoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if got := req["model"]; got != "test-model" {
			t.Fatalf("unexpected model: %#v", got)
		}
		if got := req["user"]; got != "alice" {
			t.Fatalf("expected extra_body field user=alice, got %#v", got)
		}
		if got := req["dimensions"]; got != float64(8) {
			t.Fatalf("expected extra_body field dimensions=8, got %#v", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer server.Close()

	client := openai.NewClient(
		openai.WithURL(server.URL+"/v1/embeddings"),
		openai.WithURLAutoComplete(false),
		openai.WithTimeout(5*time.Second),
		openai.WithStream(false),
		openai.WithExtraBody(map[string]any{
			"user":       "alice",
			"dimensions": 8,
		}),
	)

	got, err := client.Embedding(context.Background(), []string{"a"}, "test-model")
	if err != nil {
		t.Fatalf("Embedding failed: %v", err)
	}
	want := [][]float32{
		{0.1, 0.2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected embeddings: got=%#v want=%#v", got, want)
	}
}
