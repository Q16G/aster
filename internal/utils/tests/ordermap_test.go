package utils_test

import (
	. "aster/internal/utils"
	"testing"
)

func TestOrderMapx_KeysOrder(t *testing.T) {
	m := NewOrderMapx[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)
	m.Set("b", 20) // 覆盖不改变顺序

	keys := m.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys len = %d", len(keys))
	}
	if keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("Keys = %#v", keys)
	}

	m.Delete("b")
	keys = m.Keys()
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "c" {
		t.Fatalf("Keys after delete = %#v", keys)
	}
}

func TestOrderMapx_ForEachOrder(t *testing.T) {
	m := NewOrderMapx[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	var got []string
	m.ForEach(func(key string, value int) {
		got = append(got, key)
	})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("ForEach order = %#v", got)
	}
}
