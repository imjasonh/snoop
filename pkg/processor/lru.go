package processor

import "container/list"

// lruCache implements a simple Least Recently Used cache for string deduplication.
// It maintains a doubly-linked list for LRU ordering and a map for O(1) lookups.
type lruCache struct {
	maxSize int
	items   map[string]*list.Element
	order   *list.List
	evicted uint64
}

// newLRUCache creates a new LRU cache with the given maximum size.
// If maxSize is 0 or negative, the cache is unbounded.
func newLRUCache(maxSize int) *lruCache {
	return &lruCache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
}

// add adds a key to the cache. Returns true if the key was already present.
// If the cache is at capacity, the least recently used item is evicted.
func (c *lruCache) add(key string) bool {
	// Check if key already exists
	if elem, exists := c.items[key]; exists {
		// Move to front (most recently used)
		c.order.MoveToFront(elem)
		return true
	}

	// Add new key
	elem := c.order.PushFront(key)
	c.items[key] = elem

	// Evict if over capacity (only if maxSize > 0)
	if c.maxSize > 0 && c.order.Len() > c.maxSize {
		c.evictOldest()
	}

	return false
}

// evictOldest removes the least recently used item from the cache.
func (c *lruCache) evictOldest() {
	elem := c.order.Back()
	if elem != nil {
		c.order.Remove(elem)
		delete(c.items, elem.Value.(string))
		c.evicted++
	}
}

// len returns the current number of items in the cache.
func (c *lruCache) len() int {
	return len(c.items)
}

// evictions returns the total number of evictions that have occurred.
func (c *lruCache) evictions() uint64 {
	return c.evicted
}

// keys returns all keys currently in the cache (unsorted).
func (c *lruCache) keys() []string {
	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	return keys
}

// reset clears all items from the cache.
func (c *lruCache) reset() {
	c.items = make(map[string]*list.Element)
	c.order = list.New()
	c.evicted = 0
}
