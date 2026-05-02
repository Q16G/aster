package utils

import (
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

type LRUCache[K comparable, V any] struct {
	cache *lru.Cache[K, V]
	mu    sync.RWMutex
}

func NewLRUCache[K comparable, V any](size int) (*LRUCache[K, V], error) {
	cache, err := lru.New[K, V](size)
	if err != nil {
		return nil, err
	}
	return &LRUCache[K, V]{
		cache: cache,
	}, nil
}

func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Get(key)
}

func (c *LRUCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Add(key, value)
}

func (c *LRUCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Remove(key)
}

func (c *LRUCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Purge()
}

func (c *LRUCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Len()
}

func (c *LRUCache[K, V]) Resize(size int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache.Resize(size)
}

func (c *LRUCache[K, V]) Contains(key K) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Contains(key)
}
