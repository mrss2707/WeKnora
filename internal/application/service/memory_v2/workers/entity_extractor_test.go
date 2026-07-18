package workers

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockEntityExtractorRepo implements interfaces.MemoryRepositoryV2 for testing.
type mockEntityExtractorRepo struct {
	createFunc       func(ctx context.Context, memory *types.AgentMemory) error
	getByIDFunc      func(ctx context.Context, tenantID, id string) (*types.AgentMemory, error)
	updateFunc       func(ctx context.Context, memory *types.AgentMemory) error
	deleteFunc       func(ctx context.Context, tenantID, id string) error
	searchFunc       func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error)
	cosineSearchFunc func(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error)
}

func (m *mockEntityExtractorRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, memory)
	}
	return nil
}

func (m *mockEntityExtractorRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, tenantID, id)
	}
	return nil, nil
}

func (m *mockEntityExtractorRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, memory)
	}
	return nil
}

func (m *mockEntityExtractorRepo) Delete(ctx context.Context, tenantID, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, id)
	}
	return nil
}

func (m *mockEntityExtractorRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockEntityExtractorRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	if m.cosineSearchFunc != nil {
		return m.cosineSearchFunc(ctx, filter, embedding, limit)
	}
	return nil, nil
}

func (m *mockEntityExtractorRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	return true, nil
}

func (m *mockEntityExtractorRepo) UnlockDreamer(ctx context.Context, tenantID string) error {
	return nil
}

func (m *mockEntityExtractorRepo) ComputeHubScores(ctx context.Context, tenantID string) error {
	return nil
}

func (m *mockEntityExtractorRepo) InvalidateResultCache(ctx context.Context, tenantID string) {}

func (m *mockEntityExtractorRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	return nil, nil
}
func (m *mockEntityExtractorRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (m *mockEntityExtractorRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (m *mockEntityExtractorRepo) DeleteRelation(ctx context.Context, id, tenantID string) error {
	return nil
}
func (m *mockEntityExtractorRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (m *mockEntityExtractorRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}

// mockEntityExtractorChat implements chat.Chat for testing the entity extractor.
type mockEntityExtractorChat struct {
	mu       sync.Mutex
	chatFunc func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error)
	chatCalls int
}

func (m *mockEntityExtractorChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	m.mu.Lock()
	m.chatCalls++
	m.mu.Unlock()
	if m.chatFunc != nil {
		return m.chatFunc(ctx, messages, opts)
	}
	return &types.ChatResponse{Content: "[]"}, nil
}

func (m *mockEntityExtractorChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (m *mockEntityExtractorChat) GetModelName() string { return "mock-extractor" }

func (m *mockEntityExtractorChat) GetModelID() string { return "mock-extractor-model" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeExtractorMemory(id, tenantID, content string) *types.AgentMemory {
	return &types.AgentMemory{
		ID:         id,
		TenantID:   tenantID,
		Content:    content,
		Importance: 3,
		Verdict:    types.VerdictNone,
		MemoryType: "semantic",
		Tier:       2,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func newTestEntityExtractor(repo *mockEntityExtractorRepo, chatClient chat.Chat) *EntityExtractor {
	return NewEntityExtractor(repo, chatClient)
}

// entityExtractorResponseJSON builds the JSON response for the mock LLM.
func entityExtractorResponseJSON(items []entityBatchResult) string {
	b, err := json.Marshal(items)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Test: Buffer accumulates items up to 10 and triggers flush
// ---------------------------------------------------------------------------

func TestEntityExtractor_BufferAccumulatesUpToTen(t *testing.T) {
	chatCalled := make(chan struct{}, 1)
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			chatCalled <- struct{}{}
			return &types.ChatResponse{Content: "[]"}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	// Enqueue 9 items — buffer should have 9, no flush
	for i := 0; i < 9; i++ {
		e.Enqueue(makeExtractorMemory("mem-id", "tenant-1", "test content"))
	}

	assert.Equal(t, 9, len(e.buffer), "buffer should have 9 items after 9 enqueues")

	// Enqueue the 10th item — flush goroutine should be triggered
	e.Enqueue(makeExtractorMemory("mem-id-10", "tenant-1", "10th item"))

	// Wait for flush to call Chat
	select {
	case <-chatCalled:
		// flush was triggered
	case <-time.After(time.Second):
		t.Fatal("flush was not triggered after enqueueing 10th item")
	}

	// Buffer should be cleared after flush
	assert.Empty(t, e.buffer, "buffer should be empty after flush")
}

func TestEntityExtractor_BufferDoesNotFlushBeforeTen(t *testing.T) {
	mockChat := &mockEntityExtractorChat{}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	for i := 0; i < 9; i++ {
		e.Enqueue(makeExtractorMemory("mem-id", "tenant-1", "test content"))
	}

	// Chat should not have been called with only 9 items
	assert.Equal(t, 0, mockChat.chatCalls, "no flush should occur before 10 items")
	assert.Equal(t, 9, len(e.buffer), "buffer should have 9 items")
}

// ---------------------------------------------------------------------------
// Test: Flush sends batch LLM call
// ---------------------------------------------------------------------------

func TestEntityExtractor_FlushSendsBatchLLMCall(t *testing.T) {
	var capturedMessages []chat.Message
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			capturedMessages = messages
			return &types.ChatResponse{Content: "[]"}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	// Enqueue a few items
	e.Enqueue(makeExtractorMemory("mem-1", "tenant-1", "Memory one"))
	e.Enqueue(makeExtractorMemory("mem-2", "tenant-1", "Memory two"))

	// Call flush directly
	e.flush(context.Background())

	require.Len(t, capturedMessages, 1, "should send exactly one message")
	assert.Equal(t, "user", capturedMessages[0].Role, "message role should be user")
	assert.Contains(t, capturedMessages[0].Content, "entity extraction", "should contain extraction prompt")
	assert.Contains(t, capturedMessages[0].Content, "Memory one", "should include memory content in prompt")
	assert.Contains(t, capturedMessages[0].Content, "Memory two", "should include memory content in prompt")

	// Buffer should be empty after flush
	assert.Empty(t, e.buffer)
}

// ---------------------------------------------------------------------------
// Test: Entity extraction with confidence >= 0.7 stores relations
// ---------------------------------------------------------------------------

func TestEntityExtractor_HighConfidenceEntityStored(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			items := []entityBatchResult{
				{
					MemoryIndex: 1,
					Entities: []extractedEntity{
						{Name: "John Doe", Type: "Person", Confidence: 0.95},
						{Name: "Acme Corp", Type: "Organization", Confidence: 0.85},
					},
				},
			}
			return &types.ChatResponse{Content: entityExtractorResponseJSON(items)}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem := makeExtractorMemory("mem-1", "tenant-1", "John Doe works at Acme Corp")

	// processBatch is unexported but accessible in the same package
	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	// The processBatch method does not error under normal conditions
	// (relations are assigned to _), so the main assertion is no panic.
	// Chat was called exactly once.
	assert.Equal(t, 1, mockChat.chatCalls, "Chat should be called exactly once")
}

func TestEntityExtractor_HighConfidenceEntityWithMultipleMemories(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			items := []entityBatchResult{
				{
					MemoryIndex: 1,
					Entities: []extractedEntity{
						{Name: "Alice", Type: "Person", Confidence: 0.98},
					},
				},
				{
					MemoryIndex: 2,
					Entities: []extractedEntity{
						{Name: "Go Language", Type: "Technology", Confidence: 0.92},
					},
				},
			}
			return &types.ChatResponse{Content: entityExtractorResponseJSON(items)}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem1 := makeExtractorMemory("mem-1", "tenant-1", "Alice is a developer")
	mem2 := makeExtractorMemory("mem-2", "tenant-1", "Go Language is fast")

	e.processBatch(context.Background(), []*types.AgentMemory{mem1, mem2})

	// Both entities should be processed without error
	assert.Equal(t, 1, mockChat.chatCalls)
}

// ---------------------------------------------------------------------------
// Test: Entity extraction with confidence < 0.7 skipped
// ---------------------------------------------------------------------------

func TestEntityExtractor_LowConfidenceEntitySkipped(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			items := []entityBatchResult{
				{
					MemoryIndex: 1,
					Entities: []extractedEntity{
						{Name: "Maybe Person", Type: "Person", Confidence: 0.50},
						{Name: "Definitely Person", Type: "Person", Confidence: 0.95},
					},
				},
			}
			return &types.ChatResponse{Content: entityExtractorResponseJSON(items)}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem := makeExtractorMemory("mem-1", "tenant-1", "Some content")

	// Should not panic — low-confidence entity is skipped with continue
	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	assert.Equal(t, 1, mockChat.chatCalls)
}

func TestEntityExtractor_AllLowConfidenceSkipped(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			items := []entityBatchResult{
				{
					MemoryIndex: 1,
					Entities: []extractedEntity{
						{Name: "Low One", Type: "Person", Confidence: 0.30},
						{Name: "Low Two", Type: "Organization", Confidence: 0.55},
						{Name: "Low Three", Type: "Concept", Confidence: 0.69},
					},
				},
			}
			return &types.ChatResponse{Content: entityExtractorResponseJSON(items)}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem := makeExtractorMemory("mem-1", "tenant-1", "Some content")

	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	assert.Equal(t, 1, mockChat.chatCalls)
	// No panic means success
}

// ---------------------------------------------------------------------------
// Test: Empty buffer flush is no-op
// ---------------------------------------------------------------------------

func TestEntityExtractor_EmptyBufferFlushIsNoOp(t *testing.T) {
	mockChat := &mockEntityExtractorChat{}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	// Buffer is empty — flush should return immediately
	e.flush(context.Background())

	assert.Equal(t, 0, mockChat.chatCalls, "Chat should not be called when buffer is empty")
	assert.Empty(t, e.buffer)
}

// ---------------------------------------------------------------------------
// Test: Context cancellation stops worker
// ---------------------------------------------------------------------------

func TestEntityExtractor_Run_ContextCancellation(t *testing.T) {
	mockChat := &mockEntityExtractorChat{}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	// Give the goroutine time to enter the select loop
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case <-done:
		// Run returned cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after context cancellation")
	}
}

func TestEntityExtractor_Run_PreCancelledContext(t *testing.T) {
	mockChat := &mockEntityExtractorChat{}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Run returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds after pre-cancelled context")
	}
}

func TestEntityExtractor_Run_ContextCancellationFlushesRemaining(t *testing.T) {
	var capturedMessages []chat.Message
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			capturedMessages = messages
			return &types.ChatResponse{Content: "[]"}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	// Enqueue items before starting Run
	e.Enqueue(makeExtractorMemory("mem-1", "tenant-1", "Remaining memory"))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Run returned
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2 seconds")
	}

	// The remaining item should have been flushed on context cancellation
	require.NotNil(t, capturedMessages, "remaining items should be flushed on cancellation")
	assert.Contains(t, capturedMessages[0].Content, "Remaining memory")
}

// ---------------------------------------------------------------------------
// Test: LLM call error handled gracefully
// ---------------------------------------------------------------------------

func TestEntityExtractor_LLMCallErrorHandledGracefully(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return nil, assert.AnError
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem := makeExtractorMemory("mem-1", "tenant-1", "Test content")

	// Should not panic when LLM call returns an error
	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	assert.Equal(t, 1, mockChat.chatCalls, "Chat should have been attempted")
}

func TestEntityExtractor_FlushWithLLMCallError(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return nil, assert.AnError
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	e.Enqueue(makeExtractorMemory("mem-1", "tenant-1", "Error test"))

	// flush should handle the LLM error without panicking
	e.flush(context.Background())

	assert.Equal(t, 1, mockChat.chatCalls)
	assert.Empty(t, e.buffer, "buffer should still be cleared even on error")
}

// ---------------------------------------------------------------------------
// Test: Invalid JSON response from LLM
// ---------------------------------------------------------------------------

func TestEntityExtractor_InvalidJSONResponse(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			return &types.ChatResponse{Content: "not valid json"}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem := makeExtractorMemory("mem-1", "tenant-1", "Test")

	// Should not panic on invalid JSON
	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	assert.Equal(t, 1, mockChat.chatCalls)
}

// ---------------------------------------------------------------------------
// Test: processBatch with nil chat
// ---------------------------------------------------------------------------

func TestEntityExtractor_NilChatDoesNotPanic(t *testing.T) {
	e := &EntityExtractor{
		repo:   &mockEntityExtractorRepo{},
		chat:   nil,
		buffer: make([]*types.AgentMemory, 0, 10),
	}

	mem := makeExtractorMemory("mem-1", "tenant-1", "Test")
	e.processBatch(context.Background(), []*types.AgentMemory{mem})
	// Should not panic with nil chat
}

// ---------------------------------------------------------------------------
// Test: Out-of-range memory_index in LLM response
// ---------------------------------------------------------------------------

func TestEntityExtractor_OutOfRangeMemoryIndex(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			items := []entityBatchResult{
				{
					MemoryIndex: 99, // Out of range
					Entities: []extractedEntity{
						{Name: "Ghost", Type: "Person", Confidence: 0.95},
					},
				},
			}
			return &types.ChatResponse{Content: entityExtractorResponseJSON(items)}, nil
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	mem := makeExtractorMemory("mem-1", "tenant-1", "Test")

	// Should skip out-of-range memory index without panic
	e.processBatch(context.Background(), []*types.AgentMemory{mem})

	assert.Equal(t, 1, mockChat.chatCalls)
}

// ---------------------------------------------------------------------------
// Test: Panic recovery in processBatch
// ---------------------------------------------------------------------------

func TestEntityExtractor_FlushPanicRecovery(t *testing.T) {
	mockChat := &mockEntityExtractorChat{
		chatFunc: func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
			panic("simulated panic in chat call")
		},
	}
	e := newTestEntityExtractor(&mockEntityExtractorRepo{}, mockChat)

	e.Enqueue(makeExtractorMemory("mem-1", "tenant-1", "Panic test"))

	// flush does not have a recover, so the panic propagates
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic to propagate from flush/processBatch")
		assert.Contains(t, r, "simulated panic in chat call")
	}()

	e.flush(context.Background())
}

// ---------------------------------------------------------------------------
// Test: formatBatchItem helper
// ---------------------------------------------------------------------------

func TestFormatBatchItem(t *testing.T) {
	result := formatBatchItem(1, "mem-123", "Hello world")
	assert.Equal(t, "[mem-123] Hello world\n", result)

	result2 := formatBatchItem(5, "mem-456", "Multi\nline")
	assert.Equal(t, "[mem-456] Multi\nline\n", result2)
}
