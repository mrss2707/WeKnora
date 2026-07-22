package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheSetGet(t *testing.T) {
	c := NewCache()
	c.Set("key1", "value1", 5*time.Second)

	v, ok := c.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", v)
}

func TestCacheExpiry(t *testing.T) {
	c := NewCache()
	c.Set("key1", "value1", 10*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	_, ok := c.Get("key1")
	assert.False(t, ok, "expired key should not be found")
}

func TestCacheInvalidate(t *testing.T) {
	c := NewCache()
	c.Set("prefix:a", 1, time.Hour)
	c.Set("prefix:b", 2, time.Hour)
	c.Set("other", 3, time.Hour)

	c.Invalidate("prefix:")

	_, ok := c.Get("prefix:a")
	assert.False(t, ok, "invalidated key should not be found")
	_, ok = c.Get("prefix:b")
	assert.False(t, ok)
	v, ok := c.Get("other")
	assert.True(t, ok)
	assert.Equal(t, 3, v)
}

func TestCacheNilSafe(t *testing.T) {
	var c *MemCache
	// All operations should be no-ops on nil cache
	c.Set("key", "value", time.Hour)
	_, ok := c.Get("key")
	assert.False(t, ok)
	c.Invalidate("prefix")
}
