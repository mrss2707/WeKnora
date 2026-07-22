// Package memory provides in-memory TTL cache for hook results and session dedup.
// Interface is designed for future Redis swap; nil-safe (hooks work without cache).
package memory

import (
	"sync"
	"time"
)

// entry is a single cache item with expiration.
type entry struct {
	value   any
	expires time.Time
}

// MemCache is a thread-safe in-memory TTL cache implementing CacheStore.
type MemCache struct {
	mu    sync.RWMutex
	items map[string]*entry
}

// NewCache creates a new MemCache. Starts a background goroutine for periodic
// cleanup (every 60s). The caller should not create multiple caches per process.
func NewCache() *MemCache {
	c := &MemCache{items: make(map[string]*entry)}
	go c.reapLoop(60 * time.Second)
	return c
}

// Get returns the cached value and true, or nil and false if absent or expired.
// Nil-safe: returns (nil, false) when c is nil.
func (c *MemCache) Get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	return e.value, true
}

// Set stores a value with the given TTL. Nil-safe: no-op when c is nil.
func (c *MemCache) Set(key string, value any, ttl time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items[key] = &entry{value: value, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
}

// Invalidate removes all keys starting with the given prefix.
// Nil-safe: no-op when c is nil.
func (c *MemCache) Invalidate(prefix string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	for k := range c.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.items, k)
		}
	}
	c.mu.Unlock()
}

// reapLoop periodically removes expired entries.
func (c *MemCache) reapLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, e := range c.items {
			if now.After(e.expires) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}

// Compile-time check: MemCache implements CacheStore.
var _ CacheStore = (*MemCache)(nil)
