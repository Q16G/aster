package selfupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type UpdateChecker struct {
	currentVersion string
	cachePath      string
	proxy          string
	notifyFunc     func(rel *Release)

	mu     sync.RWMutex
	latest *Release
}

type CheckerOption func(*UpdateChecker)

func WithProxy(proxy string) CheckerOption {
	return func(c *UpdateChecker) {
		c.proxy = proxy
	}
}

func WithNotifyFunc(fn func(rel *Release)) CheckerOption {
	return func(c *UpdateChecker) {
		c.notifyFunc = fn
	}
}

func NewUpdateChecker(currentVersion, cachePath string, opts ...CheckerOption) *UpdateChecker {
	c := &UpdateChecker{
		currentVersion: currentVersion,
		cachePath:      cachePath,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *UpdateChecker) StartBackgroundCheck(ctx context.Context) {
	if os.Getenv("ASTER_DISABLE_UPDATE_CHECK") != "" {
		return
	}
	if c.currentVersion == "dev" {
		return
	}

	go func() {
		c.checkOnce(ctx)

		ticker := time.NewTicker(60 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.checkOnce(ctx)
			}
		}
	}()
}

func (c *UpdateChecker) Latest() *Release {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

func (c *UpdateChecker) IsUpdateAvailable() bool {
	rel := c.Latest()
	if rel == nil {
		return false
	}
	return IsNewer(c.currentVersion, rel.TagName)
}

func (c *UpdateChecker) checkOnce(ctx context.Context) {
	if cached := c.loadCache(); cached != nil && IsNewer(c.currentVersion, cached.TagName) {
		c.setLatest(cached)
	}

	releases, err := FetchReleases(ctx, &FetchOptions{Proxy: c.proxy})
	if err != nil {
		return
	}

	rel := SelectLatest(releases, DefaultChannels)
	if rel == nil {
		return
	}

	c.saveCache(rel)

	if IsNewer(c.currentVersion, rel.TagName) {
		c.setLatest(rel)
	}
}

func (c *UpdateChecker) setLatest(rel *Release) {
	c.mu.Lock()
	alreadySet := c.latest != nil && c.latest.TagName == rel.TagName
	c.latest = rel
	c.mu.Unlock()

	if !alreadySet && c.notifyFunc != nil {
		c.notifyFunc(rel)
	}
}

type updateCache struct {
	Release   *Release  `json:"release"`
	CheckedAt time.Time `json:"checked_at"`
}

func (c *UpdateChecker) loadCache() *Release {
	if c.cachePath == "" {
		return nil
	}
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		return nil
	}
	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	if time.Since(cache.CheckedAt) > 24*time.Hour {
		return nil
	}
	return cache.Release
}

func (c *UpdateChecker) saveCache(rel *Release) {
	if c.cachePath == "" {
		return
	}
	cache := updateCache{
		Release:   rel,
		CheckedAt: time.Now(),
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.cachePath), 0o755)
	_ = os.WriteFile(c.cachePath, data, 0o644)
}
