package react

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func TestBuildToolResultRender_ReadFileImage(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "pixel.png")
	writeTestImage(t, imagePath)

	tool := builtin_tools.NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": imagePath,
	})
	if err != nil {
		t.Fatalf("read_file execute failed: %v", err)
	}

	render := buildToolResultRender(builtin_tools.ReadFileToolName, out)
	contexts, ok := render.Content.([]*ai.ChatContext)
	if !ok {
		t.Fatalf("expected chat contexts, got %T", render.Content)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(contexts))
	}
	if contexts[0] == nil || contexts[0].Type != "text" || !strings.Contains(contexts[0].Text, imagePath) {
		t.Fatalf("unexpected text context: %#v", contexts[0])
	}
	if contexts[1] == nil || contexts[1].Type != "image_url" {
		t.Fatalf("unexpected image context: %#v", contexts[1])
	}
	url, _ := contexts[1].ImageURL["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("unexpected image data url: %q", url)
	}
	if len(render.Media) != 1 {
		t.Fatalf("expected one media item, got %d", len(render.Media))
	}
	if render.Media[0].Kind != "image" || render.Media[0].Path != imagePath {
		t.Fatalf("unexpected media payload: %+v", render.Media[0])
	}
}

func TestAICallProxyWriteToolResult_PreservesImageContexts(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "pixel.png")
	writeTestImage(t, imagePath)

	tool := builtin_tools.NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": imagePath,
	})
	if err != nil {
		t.Fatalf("read_file execute failed: %v", err)
	}

	agent, err := NewReActAgent("tool-result-test", &noopChatClientForExecute{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	render := buildToolResultRender(builtin_tools.ReadFileToolName, out)
	agent.AICallProxyWriteToolResult("call-read-image", builtin_tools.ReadFileToolName, "", nil, render.Content, "", false)

	if len(agent.stepHistory) != 1 {
		t.Fatalf("expected 1 step history item, got %d", len(agent.stepHistory))
	}
	contexts, ok := agent.stepHistory[0].Content.([]*ai.ChatContext)
	if !ok {
		t.Fatalf("expected persisted tool result contexts, got %T", agent.stepHistory[0].Content)
	}
	if len(contexts) != 2 || contexts[1] == nil || contexts[1].Type != "image_url" {
		t.Fatalf("unexpected persisted contexts: %#v", contexts)
	}
}

func writeTestImage(t *testing.T, path string) {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO6qvVsAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
}
