package processor

import "testing"

func TestLRUCache_Basic(t *testing.T) {
	cache := newLRUCache(3)

	// Add first item
	if exists := cache.add("a"); exists {
		t.Error("expected 'a' to be new")
	}
	if cache.len() != 1 {
		t.Errorf("len = %d, want 1", cache.len())
	}

	// Add same item again
	if exists := cache.add("a"); !exists {
		t.Error("expected 'a' to exist")
	}
	if cache.len() != 1 {
		t.Errorf("len = %d, want 1", cache.len())
	}

	// Add more items
	cache.add("b")
	cache.add("c")
	if cache.len() != 3 {
		t.Errorf("len = %d, want 3", cache.len())
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := newLRUCache(3)

	// Fill cache
	cache.add("a")
	cache.add("b")
	cache.add("c")

	if cache.len() != 3 {
		t.Fatalf("len = %d, want 3", cache.len())
	}
	if cache.evictions() != 0 {
		t.Errorf("evictions = %d, want 0", cache.evictions())
	}

	// Add fourth item, should evict 'a' (oldest)
	cache.add("d")
	if cache.len() != 3 {
		t.Errorf("len = %d, want 3 after eviction", cache.len())
	}
	if cache.evictions() != 1 {
		t.Errorf("evictions = %d, want 1", cache.evictions())
	}

	// 'a' should no longer exist
	if exists := cache.add("a"); exists {
		t.Error("expected 'a' to be evicted and treated as new")
	}
	if cache.evictions() != 2 {
		t.Errorf("evictions = %d, want 2", cache.evictions())
	}

	// 'b' should have been evicted by adding 'a'
	if exists := cache.add("b"); exists {
		t.Error("expected 'b' to be evicted")
	}
}

func TestLRUCache_LRUOrdering(t *testing.T) {
	cache := newLRUCache(3)

	// Add a, b, c (in that order, so 'a' is oldest)
	cache.add("a")
	cache.add("b")
	cache.add("c")

	// Access 'a' to make it most recent
	if exists := cache.add("a"); !exists {
		t.Fatal("expected 'a' to exist")
	}

	// Now add 'd', which should evict 'b' (now oldest)
	cache.add("d")

	// 'a' should still exist (was refreshed)
	if exists := cache.add("a"); !exists {
		t.Error("expected 'a' to still exist after refresh")
	}

	// 'c' should still exist
	if exists := cache.add("c"); !exists {
		t.Error("expected 'c' to exist")
	}

	// 'd' should still exist
	if exists := cache.add("d"); !exists {
		t.Error("expected 'd' to exist")
	}

	// 'b' should have been evicted
	if exists := cache.add("b"); exists {
		t.Error("expected 'b' to be evicted")
	}

	// Now cache should contain: b, d, c (a was evicted when b was added)
	// Verify 'a' was evicted
	if exists := cache.add("a"); exists {
		t.Error("expected 'a' to have been evicted after adding 'b'")
	}
}

func TestLRUCache_Unbounded(t *testing.T) {
	// maxSize = 0 means unbounded
	cache := newLRUCache(0)

	// Add many items
	for i := 0; i < 1000; i++ {
		cache.add(string(rune('a' + (i % 26))))
	}

	// Should have 26 unique items (a-z)
	if cache.len() != 26 {
		t.Errorf("len = %d, want 26", cache.len())
	}

	// No evictions should occur
	if cache.evictions() != 0 {
		t.Errorf("evictions = %d, want 0 (unbounded)", cache.evictions())
	}
}

func TestLRUCache_NegativeSize(t *testing.T) {
	// Negative maxSize should also be unbounded
	cache := newLRUCache(-1)

	for i := 0; i < 100; i++ {
		cache.add(string(rune('0' + i)))
	}

	if cache.len() != 100 {
		t.Errorf("len = %d, want 100", cache.len())
	}
	if cache.evictions() != 0 {
		t.Errorf("evictions = %d, want 0", cache.evictions())
	}
}

func TestLRUCache_Keys(t *testing.T) {
	cache := newLRUCache(5)

	items := []string{"foo", "bar", "baz"}
	for _, item := range items {
		cache.add(item)
	}

	keys := cache.keys()
	if len(keys) != len(items) {
		t.Errorf("keys length = %d, want %d", len(keys), len(items))
	}

	// Check all items are present
	keySet := make(map[string]bool)
	for _, key := range keys {
		keySet[key] = true
	}

	for _, item := range items {
		if !keySet[item] {
			t.Errorf("expected key %q in keys", item)
		}
	}
}

func TestLRUCache_Reset(t *testing.T) {
	cache := newLRUCache(3)

	cache.add("a")
	cache.add("b")
	cache.add("c")
	cache.add("d") // Causes eviction

	if cache.len() != 3 {
		t.Fatalf("len = %d, want 3", cache.len())
	}
	if cache.evictions() != 1 {
		t.Fatalf("evictions = %d, want 1", cache.evictions())
	}

	cache.reset()

	if cache.len() != 0 {
		t.Errorf("len after reset = %d, want 0", cache.len())
	}
	if cache.evictions() != 0 {
		t.Errorf("evictions after reset = %d, want 0", cache.evictions())
	}

	// Should be able to add items again
	cache.add("x")
	if cache.len() != 1 {
		t.Errorf("len after adding to reset cache = %d, want 1", cache.len())
	}
}
