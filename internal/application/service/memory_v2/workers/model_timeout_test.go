package workers

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hungChat models a provider that never answers until the caller gives up.
type hungChat struct{ scriptedChat }

func (h *hungChat) Chat(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDreamer_HungModelIsBounded(t *testing.T) {
	store := newDreamStore(dmem("a", types.VerdictNone, 1))
	app := &recordingApplier{}
	d := newDreamer(store, &hungChat{}, app)
	d.modelTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := d.RunScopePass(context.Background(), "1", "kb")
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "LLM call failed")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("a hung model must not hold the Dreamer pass")
	}
	assert.Empty(t, app.patches)
	assert.Empty(t, store.marked, "the batch stays eligible for the next pass")
}

func TestEntityExtractor_HungModelIsBounded(t *testing.T) {
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, &hungChat{})
	e.callTimeout = 50 * time.Millisecond
	done := make(chan struct{})
	go func() {
		e.processBatch(context.Background(), []*types.AgentMemory{makeExtractorMemory("m", "t", "x")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hung model must not hold the extraction batch")
	}
}
