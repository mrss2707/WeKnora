package memory_v2

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/workers"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Compile-time interface check
var _ interfaces.MemoryServiceV2 = (*MemoryServiceV2Impl)(nil)

// MemoryCache provides a simple in-memory cache for embeddings and results.
type MemoryCache struct {
	mu       sync.RWMutex
	items    map[string]cacheEntry
	maxSize  int
}

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// NewMemoryCache creates a new MemoryCache with default max 10000 entries.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		items:   make(map[string]cacheEntry),
		maxSize: 10000,
	}
}

// Get retrieves a value from the cache. Returns nil if not found or expired.
func (c *MemoryCache) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[key]
	if !ok {
		return nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.value
}

// Set stores a value with a TTL in seconds. If the cache is at max capacity,
// a random entry is evicted to make room.
func (c *MemoryCache) Set(key string, value interface{}, ttlSeconds int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.items) >= c.maxSize {
		c.evictOne()
	}

	expiresAt := time.Time{}
	if ttlSeconds > 0 {
		expiresAt = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	c.items[key] = cacheEntry{value: value, expiresAt: expiresAt}
}

// evictOne removes one random entry. Must be called with lock held.
func (c *MemoryCache) evictOne() {
	for k := range c.items {
		delete(c.items, k)
		return
	}
}

// Invalidate removes a key from the cache.
func (c *MemoryCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// InvalidateByPrefix removes all keys matching the given prefix.
func (c *MemoryCache) InvalidateByPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.items {
		if strings.HasPrefix(k, prefix) {
			delete(c.items, k)
		}
	}
}

// Keys returns all cache keys for iteration.
func (c *MemoryCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.items))
	for k := range c.items {
		keys = append(keys, k)
	}
	return keys
}

// Len returns the number of items in the cache.
func (c *MemoryCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// StartCleanup runs a background goroutine that evicts expired entries.
// Safe to call multiple times; only the first call starts the loop.
func (c *MemoryCache) StartCleanup(interval time.Duration) *sync.Once {
	var once sync.Once
	once.Do(func() {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				c.evictExpired()
			}
		}()
	})
	return &once
}

// evictExpired removes all expired entries.
func (c *MemoryCache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, entry := range c.items {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(c.items, k)
		}
	}
}

// MemoryServiceV2Impl implements MemoryServiceV2.
type MemoryServiceV2Impl struct {
	repo         interfaces.MemoryRepositoryV2
	modelService interfaces.ModelService // lazy init for embedder and chat
	embedder     embedding.Embedder
	chat         chat.Chat
	config       types.MemoryV2Config
	cache        *MemoryCache
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	workerOnce   sync.Once
	tenantIDs    []string

	// Sub-components
	tokenBudget *TokenBudgetManager

	// Workers (from workers sub-package)
	entityExtractor *workers.EntityExtractor
	autoLinker      *workers.AutoLinker
	consolidator    *workers.Consolidator
	dreamer         *workers.DreamerWorker
	pruner          *workers.Pruner
	healthChecker   *workers.HealthChecker
	cacheWarmer     *workers.CacheWarmer
}

// NewMemoryServiceV2 creates a new MemoryServiceV2Impl.
// embedder and chat may be nil; they are lazily initialized via modelService on first use.
func NewMemoryServiceV2(
	repo interfaces.MemoryRepositoryV2,
	modelService interfaces.ModelService,
	config types.MemoryV2Config,
	tenantIDs []string,
) *MemoryServiceV2Impl {
	cache := NewMemoryCache()
	cache.StartCleanup(30 * time.Second)

	svc := &MemoryServiceV2Impl{
		repo:         repo,
		modelService: modelService,
		config:       config,
		cache:        cache,
		tokenBudget:  NewTokenBudgetManager().WithChat(nil),
		tenantIDs:    tenantIDs,
	}

	// Wire cache into repository for invalidation
	repo.SetCacheInvalidator(cache)

	// Initialize workers that don't need embedder/chat (nil-safe).
	// embedder/chat-dependent workers are created lazily on first use.
	svc.pruner = workers.NewPruner(repo)
	svc.healthChecker = workers.NewHealthChecker(repo)

	return svc
}

// getEmbedder returns the embedder, initializing it lazily via modelService.
// Resolves the first available Embedding model — does NOT pass empty ID
// (the system has no "default model by empty ID" convention).
func (s *MemoryServiceV2Impl) getEmbedder(ctx context.Context) (embedding.Embedder, error) {
	if s.embedder != nil {
		return s.embedder, nil
	}
	if s.modelService == nil {
		return nil, fmt.Errorf("memory V2: model service not available")
	}

	// Resolve default embedding model: list models, find first Embedding type
	models, err := s.modelService.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory V2: failed to list models: %w", err)
	}
	var modelID string
	for _, m := range models {
		if m.Type == types.ModelTypeEmbedding {
			modelID = m.ID
			break
		}
	}
	if modelID == "" {
		return nil, fmt.Errorf("memory V2: no Embedding model configured")
	}

	embedder, err := s.modelService.GetEmbeddingModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("memory V2: failed to get embedding model %s: %w", modelID, err)
	}
	s.embedder = embedder
	return embedder, nil
}

// getChat returns the chat model, initializing it lazily via modelService.
// Resolves the first available KnowledgeQA model — does NOT pass empty ID.
func (s *MemoryServiceV2Impl) getChat(ctx context.Context) (chat.Chat, error) {
	if s.chat != nil {
		return s.chat, nil
	}
	if s.modelService == nil {
		return nil, fmt.Errorf("memory V2: model service not available")
	}

	// Resolve default chat model: list models, find first KnowledgeQA type
	models, err := s.modelService.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory V2: failed to list models: %w", err)
	}
	var modelID string
	for _, m := range models {
		if m.Type == types.ModelTypeKnowledgeQA {
			modelID = m.ID
			break
		}
	}
	if modelID == "" {
		return nil, fmt.Errorf("memory V2: no KnowledgeQA model configured")
	}

	ch, err := s.modelService.GetChatModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("memory V2: failed to get chat model %s: %w", modelID, err)
	}
	s.chat = ch
	if s.tokenBudget != nil {
		s.tokenBudget.WithChat(ch)
	}
	return ch, nil
}

// ensureWorkers initializes embedder/chat-dependent workers on first call.
func (s *MemoryServiceV2Impl) ensureWorkers(ctx context.Context) {
	if s.entityExtractor != nil {
		return
	}
	embedder, err := s.getEmbedder(ctx)
	if err != nil {
		return
	}
	ch, err := s.getChat(ctx)
	if err != nil {
		return
	}
	s.entityExtractor = workers.NewEntityExtractor(s.repo, ch)
	s.autoLinker = workers.NewAutoLinker(s.repo, embedder)
	s.consolidator = workers.NewConsolidator(s.repo, embedder)
	s.dreamer = workers.NewDreamerWorker(s.repo, ch, s.config.Dreamer, s.tenantIDs)
	s.cacheWarmer = workers.NewCacheWarmer(s.repo, embedder, s.config.CacheWarmer)
}

// StartWorkers launches all background workers.
// Safe to call multiple times; actual init only happens once, on the first
// call with a valid tenant context.
func (s *MemoryServiceV2Impl) StartWorkers(ctx context.Context) {
	s.workerOnce.Do(func() {
		s.ensureWorkers(ctx)
		ctx, s.cancel = context.WithCancel(ctx)
		s.wg.Add(5)

	go runWorker(ctx, &s.wg, s.entityExtractor.Run)
	go runWorker(ctx, &s.wg, s.consolidator.Run)
	go runWorker(ctx, &s.wg, s.dreamer.Run)
	go runWorker(ctx, &s.wg, s.pruner.Run)
	go runWorker(ctx, &s.wg, s.healthChecker.Run)
	if s.config.CacheWarmer.Enabled {
		s.wg.Add(1)
		go runWorker(ctx, &s.wg, s.cacheWarmer.Run)
	}
	})
}

// Cleanup stops all workers gracefully.
func (s *MemoryServiceV2Impl) Cleanup() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// runWorker wraps a worker function with panic recovery and wg tracking.
func runWorker(ctx context.Context, wg *sync.WaitGroup, fn func(context.Context)) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "worker panic recovered: %v", r)
		}
	}()
	fn(ctx)
}

// ---------------------------------------------------------------------------
// AddEpisode
// ---------------------------------------------------------------------------

// AddEpisode processes a conversation session and adds memories.
// This is a bridge method for compatibility with the existing interface.
func (s *MemoryServiceV2Impl) AddEpisode(ctx context.Context, tenantID, userID, sessionID string, messages []types.Message) error {
	if len(messages) == 0 {
		return nil
	}

	s.ensureWorkers(ctx)

	// Construct conversation content from messages
	content := ""
	for _, msg := range messages {
		content += msg.Role + ": " + msg.Content + "\n"
	}

	// Save each message as an episodic memory
	memory := &types.AgentMemory{
		TenantID:   tenantID,
		UserID:     userID,
		Content:    content,
		MemoryType: "episodic",
		SessionID:  sessionID,
		Importance: 1,
		Tier:       2,
	}

	_, err := s.SaveMemory(ctx, memory)
	return err
}

// ---------------------------------------------------------------------------
// RetrieveMemory (MemoryContext bridge)
// ---------------------------------------------------------------------------

// RetrieveMemory runs the full search pipeline and packs results as structured
// XML into MemoryContext.RelatedEpisodes[0].Summary.
func (s *MemoryServiceV2Impl) RetrieveMemory(ctx context.Context, userID, query string) (*types.MemoryContext, error) {
	if query == "" {
		return &types.MemoryContext{}, nil
	}

	results, err := s.SearchMemories(ctx, query, &types.MemoryFilter{
		UserID: userID,
		Limit:  s.config.MaxSearchResults,
	})
	if err != nil {
		return &types.MemoryContext{}, err
	}
	if len(results) == 0 {
		return &types.MemoryContext{}, nil
	}

	formatted := s.packContext(ctx, query, results)

	return &types.MemoryContext{
		RelatedEpisodes: []types.Episode{{
			UserID:  userID,
			Summary: formatted,
		}},
	}, nil
}

// ---------------------------------------------------------------------------
// ConsolidateDream
// ---------------------------------------------------------------------------

// ConsolidateDream runs one dreamer pass for a tenant.
func (s *MemoryServiceV2Impl) ConsolidateDream(ctx context.Context, tenantID string) (*types.DreamResult, error) {
	if s.dreamer == nil {
		return &types.DreamResult{}, nil
	}
	return s.dreamer.RunPass(ctx, tenantID)
}

// ---------------------------------------------------------------------------
// AssessHealth
// ---------------------------------------------------------------------------

// AssessHealth runs all 6 health checks and returns issues.
func (s *MemoryServiceV2Impl) AssessHealth(ctx context.Context, tenantID, kbID string) ([]*types.MemoryHealthIssue, error) {
	report, err := s.healthChecker.AssessHealth(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	return report.Issues, nil
}
