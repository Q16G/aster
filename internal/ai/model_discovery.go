package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ModelDescriptor struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
	Created int64  `json:"created,omitempty"`
}

type modelsListResponse struct {
	Data []ModelDescriptor `json:"data"`
}

func ListModels(ctx context.Context, baseURL, apiKey string) ([]ModelDescriptor, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base url is empty")
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list models failed: %s", resp.Status)
	}

	var payload modelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]ModelDescriptor, 0, len(payload.Data))
	for _, item := range payload.Data {
		item.ID = strings.TrimSpace(item.ID)
		item.OwnedBy = strings.TrimSpace(item.OwnedBy)
		if item.ID == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}
