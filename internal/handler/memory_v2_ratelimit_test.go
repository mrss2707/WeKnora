package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMemoryV2Limiter_SlidingWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newMemoryV2Limiter(time.Minute, 2)
	l.now = func() time.Time { return now }

	ok, _ := l.allow("u1")
	assert.True(t, ok)
	ok, _ = l.allow("u1")
	assert.True(t, ok)
	ok, retry := l.allow("u1")
	assert.False(t, ok)
	assert.Equal(t, time.Minute, retry)
	ok, _ = l.allow("u2")
	assert.True(t, ok, "keys are independent")

	now = now.Add(61 * time.Second)
	ok, _ = l.allow("u1")
	assert.True(t, ok, "budget refills after the window")

	assert.True(t, func() bool { ok, _ := newMemoryV2Limiter(time.Minute, 0).allow("x"); return ok }(), "0 disables")
}

func TestMemoryV2Handler_WritesAreRateLimited(t *testing.T) {
	svc := &memoryV2HandlerServiceFake{saveResult: &types.SaveMemoryResult{Created: true}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	h.writeLimiter = newMemoryV2Limiter(time.Minute, 1)

	c, rec := newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "first memory text"})
	setMemoryV2Auth(c, 42, "user-1")
	h.CreateMemory(c)
	require.Equal(t, http.StatusCreated, rec.Code)

	c, _ = newMemoryV2HandlerTestContext(http.MethodPost, "/memories", map[string]any{"content": "second memory text"})
	setMemoryV2Auth(c, 42, "user-1")
	h.CreateMemory(c)
	require.Len(t, c.Errors, 1)
	assert.Contains(t, c.Errors.Last().Error(), "Too many memory writes")
	assert.NotEmpty(t, c.Writer.Header().Get("Retry-After"))
	assert.Equal(t, 1, svc.saveCalls)
}

func TestMemoryV2Handler_DreamCooldownPerKnowledgeBase(t *testing.T) {
	svc := &memoryV2HandlerServiceFake{dreamResult: &types.DreamResult{}}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	h.dreamLimiter = newMemoryV2Limiter(time.Hour, 1)

	for i, wantErr := range []bool{false, true} {
		c, _ := newMemoryV2HandlerTestContext(http.MethodPost, "/memories/dream", map[string]any{})
		setMemoryV2Auth(c, 42, "admin")
		h.TriggerDream(c)
		if wantErr {
			require.Len(t, c.Errors, 1, "call %d", i)
		} else {
			require.Empty(t, c.Errors, "call %d", i)
		}
	}
	assert.Equal(t, 1, svc.dreamCalls)
}
