package utils

import "sync"

// SafeMapWithKey 是一个并发安全的泛型 map。
// - ForEach 回调返回 false 表示停止遍历
type SafeMapWithKey[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

func NewSafeMapWithKey[K comparable, V any]() *SafeMapWithKey[K, V] {
	return &SafeMapWithKey[K, V]{
		m: make(map[K]V),
	}
}

func (s *SafeMapWithKey[K, V]) Set(key K, value V) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.m == nil {
		s.m = make(map[K]V)
	}
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SafeMapWithKey[K, V]) Get(key K) (V, bool) {
	var zero V
	if s == nil {
		return zero, false
	}
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SafeMapWithKey[K, V]) Delete(key K) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

func (s *SafeMapWithKey[K, V]) ForEach(fn func(key K, value V) bool) {
	if s == nil || fn == nil {
		return
	}
	s.mu.RLock()
	if len(s.m) == 0 {
		s.mu.RUnlock()
		return
	}

	snapshot := make(map[K]V, len(s.m))
	for k, v := range s.m {
		snapshot[k] = v
	}
	s.mu.RUnlock()

	for k, v := range snapshot {
		if !fn(k, v) {
			return
		}
	}
}

func (s *SafeMapWithKey[K, V]) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	n := len(s.m)
	s.mu.RUnlock()
	return n
}

type SafeMap[V any] struct {
	inner *SafeMapWithKey[string, V]
}

func NewSafeMap[V any]() *SafeMap[V] {
	return &SafeMap[V]{inner: NewSafeMapWithKey[string, V]()}
}

func (s *SafeMap[V]) Set(key string, value V) {
	if s == nil || s.inner == nil {
		return
	}
	s.inner.Set(key, value)
}

func (s *SafeMap[V]) Get(key string) (V, bool) {
	var zero V
	if s == nil || s.inner == nil {
		return zero, false
	}
	return s.inner.Get(key)
}

func (s *SafeMap[V]) Delete(key string) {
	if s == nil || s.inner == nil {
		return
	}
	s.inner.Delete(key)
}

func (s *SafeMap[V]) ForEach(fn func(key string, value V) bool) {
	if s == nil || s.inner == nil {
		return
	}
	s.inner.ForEach(fn)
}

func (s *SafeMap[V]) Len() int {
	if s == nil || s.inner == nil {
		return 0
	}
	return s.inner.Len()
}
