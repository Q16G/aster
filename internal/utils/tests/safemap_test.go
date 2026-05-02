package utils_test

import (
	. "aster/internal/utils"
	"strconv"
	"sync"
	"testing"
)

func TestSafeMapWithKey_Basic(t *testing.T) {
	m := NewSafeMapWithKey[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	if v, ok := m.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %v,%v", v, ok)
	}
	if v, ok := m.Get("b"); !ok || v != 2 {
		t.Fatalf("Get(b) = %v,%v", v, ok)
	}
	if _, ok := m.Get("c"); ok {
		t.Fatalf("Get(c) should not exist")
	}

	m.Delete("a")
	if _, ok := m.Get("a"); ok {
		t.Fatalf("a should be deleted")
	}
}

func TestSafeMapWithKey_ForEach_Stop(t *testing.T) {
	m := NewSafeMapWithKey[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	visited := 0
	m.ForEach(func(key string, value int) bool {
		visited++
		return false
	})
	if visited != 1 {
		t.Fatalf("visited = %d", visited)
	}
}

func TestSafeMapWithKey_Concurrent(t *testing.T) {
	m := NewSafeMapWithKey[string, int]()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "k" + strconv.Itoa(i)
			m.Set(key, i)
			if v, ok := m.Get(key); !ok || v != i {
				t.Errorf("Get(%s) = %v,%v", key, v, ok)
			}
		}()
	}
	wg.Wait()
	if m.Len() != 50 {
		t.Fatalf("Len = %d", m.Len())
	}
}
