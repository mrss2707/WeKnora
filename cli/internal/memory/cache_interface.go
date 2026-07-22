package memory

import "time"

// CacheStore is the interface for hook result caching and session dedup.
// The default implementation is MemCache (in-memory TTL). Designed so a
// Redis backend can be swapped in later.
type CacheStore interface {
	Get(key string) (any, bool)
	Set(key string, value any, ttl time.Duration)
	Invalidate(prefix string)
}
