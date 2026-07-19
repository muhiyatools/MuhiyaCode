package store

import "testing"

func TestSetGet(t *testing.T) {
	s := New()
	s.Set("a", "1")
	if v, ok := s.Get("a"); !ok || v != "1" {
		t.Fatalf("Get(a) = %q, %v; want %q, true", v, ok, "1")
	}
}

func TestKeysSorted(t *testing.T) {
	s := New()
	s.Set("b", "2")
	s.Set("a", "1")
	keys := s.Keys()
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("Keys() = %v, want [a b]", keys)
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.Set("a", "1")
	s.Delete("a")
	if _, ok := s.Get("a"); ok {
		t.Fatal("Get(a) after Delete: still present")
	}
}
