package tuicontext

import (
	"sync"
	"testing"
)

func TestProvider_GetSet(t *testing.T) {
	p := NewProvider(0)
	if v := p.Get(); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
	p.Set(42)
	if v := p.Get(); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestProvider_Update(t *testing.T) {
	p := NewProvider(10)
	p.Update(func(v int) int { return v + 5 })
	if v := p.Get(); v != 15 {
		t.Fatalf("expected 15, got %d", v)
	}
}

func TestProvider_Subscribe(t *testing.T) {
	p := NewProvider("")
	var received []string
	unsub := p.Subscribe(func(v string) {
		received = append(received, v)
	})

	p.Set("hello")
	p.Set("world")

	if len(received) != 2 || received[0] != "hello" || received[1] != "world" {
		t.Fatalf("unexpected notifications: %v", received)
	}

	unsub()
	p.Set("ignored")

	if len(received) != 2 {
		t.Fatalf("expected 2 notifications after unsubscribe, got %d", len(received))
	}
}

func TestProvider_MultipleSubscribers(t *testing.T) {
	p := NewProvider(0)
	var count1, count2 int
	unsub1 := p.Subscribe(func(int) { count1++ })
	p.Subscribe(func(int) { count2++ })

	p.Set(1)
	if count1 != 1 || count2 != 1 {
		t.Fatalf("expected both called once: count1=%d count2=%d", count1, count2)
	}

	unsub1()
	p.Set(2)
	if count1 != 1 || count2 != 2 {
		t.Fatalf("after unsub1: count1=%d count2=%d", count1, count2)
	}
}

func TestProvider_ConcurrentAccess(t *testing.T) {
	p := NewProvider(0)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			p.Set(v)
		}(i)
		go func() {
			defer wg.Done()
			_ = p.Get()
		}()
	}

	wg.Wait()
}

func TestProvider_SubscribeUnsubscribeConcurrent(t *testing.T) {
	p := NewProvider(0)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unsub := p.Subscribe(func(int) {})
			p.Set(1)
			unsub()
		}()
	}

	wg.Wait()
}

func TestProvider_UpdateNotifiesSubscribers(t *testing.T) {
	p := NewProvider(0)
	var received int
	p.Subscribe(func(v int) { received = v })

	p.Update(func(v int) int { return v + 100 })
	if received != 100 {
		t.Fatalf("expected 100, got %d", received)
	}
}
