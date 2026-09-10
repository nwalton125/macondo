package endgameui

import "testing"

func TestSolveCacheGetSet(t *testing.T) {
	c := NewSolveCache(2)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.Set("a", 1)
	v, ok := c.Get("a")
	if !ok || v.(int) != 1 {
		t.Fatalf("expected hit with value 1, got %v %v", v, ok)
	}
}

func TestSolveCacheEviction(t *testing.T) {
	c := NewSolveCache(2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3) // evicts "a" (least recently used)

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected \"a\" to have been evicted")
	}
	if v, ok := c.Get("b"); !ok || v.(int) != 2 {
		t.Fatal("expected \"b\" to still be cached")
	}
	if v, ok := c.Get("c"); !ok || v.(int) != 3 {
		t.Fatal("expected \"c\" to still be cached")
	}
}

func TestSolveCacheOverwrite(t *testing.T) {
	c := NewSolveCache(2)
	c.Set("a", 1)
	c.Set("a", 2)
	v, ok := c.Get("a")
	if !ok || v.(int) != 2 {
		t.Fatalf("expected overwritten value 2, got %v %v", v, ok)
	}
}
