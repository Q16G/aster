package utils

import "sync"

// OrderMapx 是一个按插入顺序遍历的 map（并发安全）。
// 设计目标：满足 memory.TimelineMemory 对 Key 顺序、Keys/ForEach 的需求。
type OrderMapx[K comparable, V any] struct {
	mu   sync.RWMutex
	m    map[K]V
	keys []K
}

func NewOrderMapx[K comparable, V any]() *OrderMapx[K, V] {
	return &OrderMapx[K, V]{
		m: make(map[K]V),
	}
}

func (o *OrderMapx[K, V]) Set(key K, value V) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.m == nil {
		o.m = make(map[K]V)
	}
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = value
	o.mu.Unlock()
}

func (o *OrderMapx[K, V]) Get(key K) (V, bool) {
	var zero V
	if o == nil {
		return zero, false
	}
	o.mu.RLock()
	v, ok := o.m[key]
	o.mu.RUnlock()
	return v, ok
}

func (o *OrderMapx[K, V]) Delete(key K) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if _, ok := o.m[key]; !ok {
		o.mu.Unlock()
		return
	}
	delete(o.m, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	o.mu.Unlock()
}

func (o *OrderMapx[K, V]) Keys() []K {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	if len(o.keys) == 0 {
		o.mu.RUnlock()
		return nil
	}
	out := make([]K, len(o.keys))
	copy(out, o.keys)
	o.mu.RUnlock()
	return out
}

func (o *OrderMapx[K, V]) ForEach(fn func(key K, value V)) {
	if o == nil || fn == nil {
		return
	}
	o.mu.RLock()
	if len(o.keys) == 0 {
		o.mu.RUnlock()
		return
	}
	keys := make([]K, len(o.keys))
	copy(keys, o.keys)
	m := o.m
	o.mu.RUnlock()

	for _, k := range keys {
		o.mu.RLock()
		v, ok := m[k]
		o.mu.RUnlock()
		if !ok {
			continue
		}
		fn(k, v)
	}
}

func (o *OrderMapx[K, V]) Len() int {
	if o == nil {
		return 0
	}
	o.mu.RLock()
	n := len(o.m)
	o.mu.RUnlock()
	return n
}
