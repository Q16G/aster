package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModels(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o","owned_by":"openai"},{"id":"  ","owned_by":"skip"},{"id":"gpt-4.1-mini"}]}`))
	}))
	defer server.Close()

	models, err := ListModels(context.Background(), server.URL+"/v1", "test-key")
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if authHeader != "Bearer test-key" {
		t.Fatalf("unexpected auth header: %q", authHeader)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" || models[0].OwnedBy != "openai" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "gpt-4.1-mini" {
		t.Fatalf("unexpected second model: %+v", models[1])
	}
}

func TestListModelsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	if _, err := ListModels(context.Background(), server.URL, ""); err == nil {
		t.Fatal("expected error, got nil")
	}
}
