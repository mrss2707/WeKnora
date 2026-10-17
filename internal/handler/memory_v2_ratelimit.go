package handler

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
)

// memoryV2Limiter is a per-key sliding-window limiter for Memory V2 writes.
// In-process: each app replica enforces its own budget, which bounds abuse
// without a shared store. Idle keys are swept lazily on access.
type memoryV2Limiter struct {
	window time.Duration
	max    int
	now    func() time.Time

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
}

func newMemoryV2Limiter(window time.Duration, max int) *memoryV2Limiter {
	return &memoryV2Limiter{window: window, max: max, now: time.Now, hits: map[string][]time.Time{}}
}

// allow records a hit for key and reports whether it is within budget. A
// non-positive max disables the limit. retryAfter is how long until the
// oldest hit leaves the window.
func (l *memoryV2Limiter) allow(key string) (ok bool, retryAfter time.Duration) {
	if l == nil || l.max <= 0 {
		return true, 0
	}
	now := l.now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > l.window {
		for k, ts := range l.hits {
			if len(ts) == 0 || !ts[len(ts)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
		l.lastSweep = now
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false, kept[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(kept, now)
	return true, 0
}

// Default budgets; override with MEMORY_V2_WRITE_LIMIT_PER_MIN (0 disables)
// and MEMORY_V2_DREAM_COOLDOWN (Go duration, 0 disables).
const (
	defaultMemoryWritesPerMinute = 120
	defaultDreamCooldown         = 2 * time.Minute
)

func memoryV2WriteLimiterFromEnv() *memoryV2Limiter {
	limit := defaultMemoryWritesPerMinute
	if v, err := strconv.Atoi(os.Getenv("MEMORY_V2_WRITE_LIMIT_PER_MIN")); err == nil && v >= 0 {
		limit = v
	}
	return newMemoryV2Limiter(time.Minute, limit)
}

func memoryV2DreamLimiterFromEnv() *memoryV2Limiter {
	cooldown := defaultDreamCooldown
	if v, err := time.ParseDuration(os.Getenv("MEMORY_V2_DREAM_COOLDOWN")); err == nil && v >= 0 {
		cooldown = v
	}
	if cooldown == 0 {
		return newMemoryV2Limiter(time.Minute, 0)
	}
	return newMemoryV2Limiter(cooldown, 1)
}

// limitWrite enforces the per-caller write budget; on rejection it records a
// 429 with Retry-After and reports false.
func (h *MemoryV2Handler) limitWrite(c *gin.Context) bool {
	key, _ := h.getUserID(c)
	if key == "" {
		tid, _ := h.getTenantID(c)
		key = "tenant:" + tid
	}
	return rejectIfLimited(c, h.writeLimiter, key, "Too many memory writes; slow down")
}

// limitDream enforces one dream pass per knowledge base (or tenant) per
// cooldown window.
func (h *MemoryV2Handler) limitDream(c *gin.Context, tenantID, kbID string) bool {
	return rejectIfLimited(c, h.dreamLimiter, tenantID+"/"+kbID, "A dream pass for this knowledge base ran recently; try again later")
}

func rejectIfLimited(c *gin.Context, l *memoryV2Limiter, key, msg string) bool {
	ok, retry := l.allow(key)
	if ok {
		return true
	}
	secs := int(retry.Seconds()) + 1
	c.Header("Retry-After", strconv.Itoa(secs))
	c.Error(errors.NewTooManyRequestsError(msg))
	return false
}
