package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const defaultModelsDevURL = "https://models.dev/api.json"

type ModelsDevData map[string]*ModelsDevProvider

type ModelsDevProvider struct {
	ID     string                     `json:"id"`
	Name   string                     `json:"name"`
	API    string                     `json:"api,omitempty"`
	Env    []string                   `json:"env,omitempty"`
	NPM    string                     `json:"npm,omitempty"`
	Models map[string]*ModelsDevModel `json:"models"`
}

type ModelsDevModel struct {
	ID          string                        `json:"id"`
	Name        string                        `json:"name"`
	Family      string                        `json:"family,omitempty"`
	ReleaseDate string                        `json:"release_date,omitempty"`
	Attachment  bool                          `json:"attachment"`
	Reasoning   bool                          `json:"reasoning"`
	Temperature bool                          `json:"temperature"`
	ToolCall    bool                          `json:"tool_call"`
	Cost        *ModelsDevCost                `json:"cost,omitempty"`
	Limit       ModelsDevLimit                `json:"limit"`
	Modalities  *ModelsDevModality            `json:"modalities,omitempty"`
	Status      string                        `json:"status,omitempty"`
	Options     map[string]any                `json:"options,omitempty"`
	Headers     map[string]string             `json:"headers,omitempty"`
	Provider    *ModelsDevProviderOverride    `json:"provider,omitempty"`
	Variants    map[string]map[string]any     `json:"variants,omitempty"`
	OpenWeights bool                          `json:"open_weights,omitempty"`
}

type ModelsDevCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

type ModelsDevLimit struct {
	Context int `json:"context"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output"`
}

type ModelsDevModality struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

type ModelsDevProviderOverride struct {
	ID  string `json:"id,omitempty"`
	API string `json:"api,omitempty"`
}

func FetchModelsDevData(ctx context.Context) (ModelsDevData, error) {
	url := os.Getenv("ASTER_MODELS_URL")
	if url == "" {
		url = defaultModelsDevURL
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev returned %d", resp.StatusCode)
	}

	var data ModelsDevData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode models.dev: %w", err)
	}
	return data, nil
}

func LoadCachedModelsDevData(cachePath string) (ModelsDevData, error) {
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}
	var data ModelsDevData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode cache: %w", err)
	}
	return data, nil
}

func SaveModelsDevCache(cachePath string, data ModelsDevData) error {
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath, raw, 0644)
}

func LoadBundledSnapshot() (ModelsDevData, error) {
	if len(modelsSnapshotData) == 0 {
		return nil, fmt.Errorf("no bundled snapshot")
	}
	var data ModelsDevData
	if err := json.Unmarshal(modelsSnapshotData, &data); err != nil {
		return nil, fmt.Errorf("decode bundled snapshot: %w", err)
	}
	return data, nil
}
