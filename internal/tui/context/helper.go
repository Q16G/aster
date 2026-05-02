package tuicontext

import "sync"

type subscriber[T any] struct {
	id int
	fn func(T)
}

type Provider[T any] struct {
	mu     sync.RWMutex
	value  T
	subs   []subscriber[T]
	nextID int
}

func NewProvider[T any](initial T) *Provider[T] {
	return &Provider[T]{value: initial}
}

func (p *Provider[T]) Get() T {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.value
}

func (p *Provider[T]) Set(v T) {
	p.mu.Lock()
	p.value = v
	subs := make([]subscriber[T], len(p.subs))
	copy(subs, p.subs)
	p.mu.Unlock()

	for _, s := range subs {
		s.fn(v)
	}
}

func (p *Provider[T]) Update(fn func(T) T) {
	p.mu.Lock()
	p.value = fn(p.value)
	v := p.value
	subs := make([]subscriber[T], len(p.subs))
	copy(subs, p.subs)
	p.mu.Unlock()

	for _, s := range subs {
		s.fn(v)
	}
}

func (p *Provider[T]) Subscribe(fn func(T)) func() {
	p.mu.Lock()
	id := p.nextID
	p.nextID++
	p.subs = append(p.subs, subscriber[T]{id: id, fn: fn})
	p.mu.Unlock()

	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, s := range p.subs {
			if s.id == id {
				p.subs = append(p.subs[:i], p.subs[i+1:]...)
				return
			}
		}
	}
}
