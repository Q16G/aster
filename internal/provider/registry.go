package provider

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

)

type ProviderInfo struct {
	ID       string
	Name     string
	BaseURL  string
	EnvVars  []string
	Protocol string
	Models   map[string]*ModelInfo
}

type ModelInfo struct {
	ID           string
	Name         string
	Family       string
	ProviderID   string
	Status       string
	Capabilities ModelCapabilities
	Cost         ModelCost
	Limit        ModelLimit
	Options      map[string]any
	Headers      map[string]string
	Variants     map[string]map[string]any
}

type ModelCapabilities struct {
	Temperature bool
	Reasoning   bool
	Attachment  bool
	ToolCall    bool
	Vision      bool
	Audio       bool
}

type ModelLimit struct {
	Context int
	Input   int
	Output  int
}

type ModelCost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

type Registry struct {
	mu        sync.RWMutex
	providers map[string]*ProviderInfo
	cachePath string
}

func NewRegistry(cachePath string) *Registry {
	return &Registry{
		providers: make(map[string]*ProviderInfo),
		cachePath: cachePath,
	}
}

func (r *Registry) LoadFromModelsDevData(data ModelsDevData) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for pid, mp := range data {
		if mp == nil {
			continue
		}
		id := pid
		if mp.ID != "" {
			id = mp.ID
		}

		info := &ProviderInfo{
			ID:       id,
			Name:     mp.Name,
			BaseURL:  mp.API,
			EnvVars:  mp.Env,
			Protocol: inferProtocol(mp.NPM),
			Models:   make(map[string]*ModelInfo, len(mp.Models)),
		}

		for mid, mm := range mp.Models {
			if mm == nil {
				continue
			}
			modelID := mid
			if mm.ID != "" {
				modelID = mm.ID
			}
			mi := &ModelInfo{
				ID:         modelID,
				Name:       mm.Name,
				Family:     mm.Family,
				ProviderID: id,
				Status:     mm.Status,
				Capabilities: ModelCapabilities{
					Temperature: mm.Temperature,
					Reasoning:   mm.Reasoning,
					Attachment:  mm.Attachment,
					ToolCall:    mm.ToolCall,
					Vision:      hasModality(mm.Modalities, "input", "image"),
					Audio:       hasModality(mm.Modalities, "input", "audio"),
				},
				Limit: ModelLimit{
					Context: mm.Limit.Context,
					Input:   mm.Limit.Input,
					Output:  mm.Limit.Output,
				},
				Options:  mm.Options,
				Headers:  mm.Headers,
				Variants: mm.Variants,
			}
			if mm.Cost != nil {
				mi.Cost = ModelCost{
					Input:      mm.Cost.Input,
					Output:     mm.Cost.Output,
					CacheRead:  mm.Cost.CacheRead,
					CacheWrite: mm.Cost.CacheWrite,
				}
			}
			info.Models[modelID] = mi
		}
		r.providers[id] = info
	}
}

func (r *Registry) GetProvider(id string) (*ProviderInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) ListProviders() []*ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ProviderInfo, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b *ProviderInfo) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

func (r *Registry) ListProvidersSorted(priority []string, isAvailable func(string) bool) []*ProviderInfo {
	all := r.ListProviders()
	rank := make(map[string]int, len(priority))
	for i, id := range priority {
		rank[id] = i + 1
	}
	slices.SortStableFunc(all, func(a, b *ProviderInfo) int {
		ra, rb := len(priority)+1, len(priority)+1
		if v, ok := rank[a.ID]; ok {
			ra = v
		}
		if v, ok := rank[b.ID]; ok {
			rb = v
		}
		aAvail, bAvail := false, false
		if isAvailable != nil {
			aAvail = isAvailable(a.ID)
			bAvail = isAvailable(b.ID)
		}
		if aAvail != bAvail {
			if aAvail {
				return -1
			}
			return 1
		}
		if ra != rb {
			return ra - rb
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return all
}

func (r *Registry) GetModel(providerID, modelID string) (*ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil, false
	}
	m, ok := p.Models[modelID]
	return m, ok
}

func (r *Registry) ListModels(providerID string) []*ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil
	}
	out := make([]*ModelInfo, 0, len(p.Models))
	for _, m := range p.Models {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b *ModelInfo) int {
		ra, rb := statusRank(a.Status), statusRank(b.Status)
		if ra != rb {
			return ra - rb
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (r *Registry) ResolveContextBudget(modelID string) (contextWindow, outputLimit int, found bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lower := strings.ToLower(modelID)
	for _, p := range r.providers {
		for _, m := range p.Models {
			if strings.ToLower(m.ID) == lower {
				return m.Limit.Context, m.Limit.Output, true
			}
		}
	}
	for _, p := range r.providers {
		for _, m := range p.Models {
			if strings.Contains(lower, strings.ToLower(m.ID)) {
				return m.Limit.Context, m.Limit.Output, true
			}
		}
	}
	return 0, 0, false
}

func (r *Registry) ResolveModelCapabilities(modelID string) (vision, audio bool, found bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lower := strings.ToLower(modelID)
	for _, p := range r.providers {
		for _, m := range p.Models {
			if strings.ToLower(m.ID) == lower {
				return m.Capabilities.Vision, m.Capabilities.Audio, true
			}
		}
	}
	for _, p := range r.providers {
		for _, m := range p.Models {
			if strings.Contains(lower, strings.ToLower(m.ID)) {
				return m.Capabilities.Vision, m.Capabilities.Audio, true
			}
		}
	}
	return false, false, false
}

func (r *Registry) IsProviderAvailable(id string) bool {
	p, ok := r.GetProvider(id)
	if !ok {
		return false
	}
	if len(p.EnvVars) == 0 {
		return true
	}
	for _, env := range p.EnvVars {
		if os.Getenv(env) != "" {
			return true
		}
	}
	return false
}

func (r *Registry) ResolveAPIKey(providerID, cfgKey string) string {
	if cfgKey != "" {
		return os.Expand(cfgKey, os.Getenv)
	}
	p, ok := r.GetProvider(providerID)
	if !ok {
		return ""
	}
	for _, env := range p.EnvVars {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return ""
}

func (r *Registry) ProviderEnvVar(providerID string) string {
	p, ok := r.GetProvider(providerID)
	if !ok {
		return ""
	}
	if len(p.EnvVars) == 0 {
		return ""
	}
	return p.EnvVars[0]
}

func (r *Registry) StartBackgroundRefresh(ctx context.Context) {
	if os.Getenv("ASTER_DISABLE_MODELS_FETCH") != "" {
		return
	}
	go func() {
		r.refreshOnce(ctx)

		ticker := time.NewTicker(60 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.refreshOnce(ctx)
			}
		}
	}()
}

func (r *Registry) refreshOnce(ctx context.Context) {
	data, err := FetchModelsDevData(ctx)
	if err != nil {
		return
	}
	r.LoadFromModelsDevData(data)
	if r.cachePath != "" {
		_ = SaveModelsDevCache(r.cachePath, data)
	}
}

// InitRegistry creates a Registry and loads data with the priority:
// local cache → bundled snapshot → HTTP fetch.
func InitRegistry(cachePath string) *Registry {
	reg := NewRegistry(cachePath)

	if cachePath != "" {
		if data, err := LoadCachedModelsDevData(cachePath); err == nil && len(data) > 0 {
			reg.LoadFromModelsDevData(data)
			return reg
		}
	}

	if data, err := LoadBundledSnapshot(); err == nil && len(data) > 0 {
		reg.LoadFromModelsDevData(data)
		return reg
	}

	return reg
}

func inferProtocol(npm string) string {
	switch npm {
	case "@ai-sdk/anthropic":
		return "anthropic"
	case "@ai-sdk/openai":
		return "native-openai"
	case "@ai-sdk/openai-compatible", "":
		return "openai-compatible"
	default:
		return "openai-compatible"
	}
}

func hasModality(m *ModelsDevModality, direction, kind string) bool {
	if m == nil {
		return false
	}
	var list []string
	switch direction {
	case "input":
		list = m.Input
	case "output":
		list = m.Output
	}
	for _, v := range list {
		if v == kind {
			return true
		}
	}
	return false
}

func statusRank(s string) int {
	switch s {
	case "", "active":
		return 0
	case "beta":
		return 1
	case "alpha":
		return 2
	case "deprecated":
		return 3
	default:
		return 4
	}
}
