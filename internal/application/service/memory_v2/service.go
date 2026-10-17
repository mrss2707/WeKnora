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
	mu      sync.RWMutex
	items   map[string]cacheEntry
	maxSize int
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
	modelService interfaces.ModelService
	// resolver picks each (tenant, knowledge base) scope's own models.
	resolver *modelResolver
	// embedder and chat, when set, override the resolver for every scope.
	// Production leaves them nil; tests inject fixed models here.
	embedder   embedding.Embedder
	chat       chat.Chat
	config     types.MemoryV2Config
	cache      *MemoryCache
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	workerOnce sync.Once
	initOnce   sync.Once
	workerMu   sync.Mutex
	tenantIDs  []string

	// readinessReason overrides the disabled reason derived from config
	// (used to report Lite mode). Set once during wiring; guarded by
	// readinessMu.
	readinessMu     sync.RWMutex
	readinessReason string

	// Sub-components
	tokenBudget *TokenBudgetManager

	// Workers (from workers sub-package)
	entityExtractor *workers.EntityExtractor
	autoLinker      *workers.AutoLinker
	consolidator    *workers.Consolidator
	dreamer         *workers.DreamerWorker
	pruner          *workers.Pruner
	healthChecker   *workers.HealthChecker
}

// NewMemoryServiceV2 creates a new MemoryServiceV2Impl. Models are resolved
// per (tenant, knowledge base) scope through modelService; call SetKBLookup to
// let a knowledge base's own embedding/summary models take precedence.
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
		resolver:     newModelResolver(modelService, nil),
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

// SetReadinessReason overrides the disabled reason reported by Readiness
// (used to report Lite mode at wiring time). Safe to call from any goroutine.
func (s *MemoryServiceV2Impl) SetReadinessReason(reason string) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	s.readinessReason = reason
}

// Readiness reports whether the module may serve requests and run background
// work, with the concrete reason when it may not. The reported reason is, in
// order of precedence: the wiring-time override (e.g. Lite mode), a missing
// repository, or the enabled flag in config.
func (s *MemoryServiceV2Impl) Readiness() types.MemoryV2Readiness {
	if s == nil {
		return types.MemoryV2Readiness{Ready: false, Reason: types.MemoryV2ReasonRepoUnavailable}
	}
	s.readinessMu.RLock()
	override := s.readinessReason
	s.readinessMu.RUnlock()
	if override != "" {
		return types.MemoryV2Readiness{Ready: false, Reason: override}
	}
	if s.repo == nil {
		return types.MemoryV2Readiness{Ready: false, Reason: types.MemoryV2ReasonRepoUnavailable}
	}
	if !s.config.Enabled {
		return types.MemoryV2Readiness{Ready: false, Reason: types.MemoryV2ReasonConfigDisabled}
	}
	return types.MemoryV2Readiness{Ready: true, Reason: types.MemoryV2ReasonEnabled}
}

// notReadyReason returns the readiness reason when the module is not ready,
// or "" when it is. Callers reject work with this reason.
func (s *MemoryServiceV2Impl) notReadyReason() string {
	if r := s.Readiness(); !r.Ready {
		return r.Reason
	}
	return ""
}

// SetKBLookup wires the knowledge-base lookup used to find a knowledge base's
// own models. Call once during wiring, before serving requests.
func (s *MemoryServiceV2Impl) SetKBLookup(kbs KBModelLookup) {
	s.resolver = newModelResolver(s.modelService, kbs)
}

// modelSource adapts the service's fixed-model override or its resolver to
// the workers' ModelResolver contract.
type modelSource struct{ s *MemoryServiceV2Impl }

func (m modelSource) Embedder(ctx context.Context, tenantID, kbID string) (embedding.Embedder, string, error) {
	return m.s.embedderFor(ctx, tenantID, kbID)
}

func (m modelSource) Chat(ctx context.Context, tenantID, kbID string) (chat.Chat, error) {
	return m.s.chatFor(ctx, tenantID, kbID)
}

// embedderFor returns the embedder of a scope and its model id ("" for a
// fixed override).
func (s *MemoryServiceV2Impl) embedderFor(ctx context.Context, tenantID, kbID string) (embedding.Embedder, string, error) {
	if s.embedder != nil {
		return s.embedder, "", nil
	}
	return s.resolver.Embedder(ctx, tenantID, kbID)
}

// chatFor returns the chat model of a scope.
func (s *MemoryServiceV2Impl) chatFor(ctx context.Context, tenantID, kbID string) (chat.Chat, error) {
	if s.chat != nil {
		return s.chat, nil
	}
	return s.resolver.Chat(ctx, tenantID, kbID)
}

// initWorkers constructs every worker exactly once. Workers resolve models
// per scope at run time, so construction needs no model, tenant or request.
// Workers that change memories go through the service (validation, merge
// transaction, audit) rather than writing to the repository directly.
func (s *MemoryServiceV2Impl) initWorkers() {
	s.initOnce.Do(func() {
		s.workerMu.Lock()
		defer s.workerMu.Unlock()
		src := modelSource{s}
		s.entityExtractor = workers.NewEntityExtractor(s.repo, nil).WithResolver(src)
		s.autoLinker = workers.NewAutoLinker(s.repo, s.embedder).WithResolver(src)
		s.consolidator = workers.NewConsolidator(s.repo, nil)
		s.dreamer = workers.NewDreamerWorker(s.repo, s.chat, s.dreamerConfig(), nil).WithResolver(src).WithApplier(s)
		if s.pruner == nil {
			s.pruner = workers.NewPruner(s.repo)
		}
		s.pruner = s.pruner.WithDeleter(s)
	})
}

// StartWorkers constructs the workers and launches the background loops with
// the server-lifetime ctx (never a request ctx). Each worker that changes
// memories has its own flag; memory_v2.worker_mutations=false stops them all.
func (s *MemoryServiceV2Impl) StartWorkers(ctx context.Context) {
	if reason := s.notReadyReason(); reason != "" {
		logger.Warnf(ctx, "[MemoryV2] StartWorkers skipped: %s", reason)
		return
	}
	s.initWorkers()
	s.workerOnce.Do(func() {
		ctx, s.cancel = context.WithCancel(ctx)
		started := []string{"health"}
		start := func(name string, on bool, fn func(context.Context)) {
			if !on || !s.config.WorkerMutations {
				return
			}
			s.wg.Add(1)
			go runWorker(ctx, &s.wg, fn)
			started = append(started, name)
		}
		s.wg.Add(1)
		go runWorker(ctx, &s.wg, s.healthChecker.Run)
		start("dreamer", s.config.Dreamer.Enabled, s.dreamer.Run)
		start("pruner", s.config.PrunerEnabled, s.pruner.Run)
		start("consolidator", s.config.ConsolidatorEnabled, s.consolidator.Run)
		start("entity-extractor", s.config.EntityExtractorEnabled, s.entityExtractor.Run)
		logger.Infof(ctx, "[MemoryV2] Workers started: %v (worker_mutations=%v)", started, s.config.WorkerMutations)
	})
}

// Cleanup stops all workers gracefully.
func (s *MemoryServiceV2Impl) Cleanup() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// dreamerConfig returns the dreamer settings, forcing dry-run when the
// worker kill switch is off so a manual pass only proposes actions.
func (s *MemoryServiceV2Impl) dreamerConfig() types.DreamerConfig {
	cfg := s.config.Dreamer
	if !s.config.WorkerMutations {
		cfg.DryRun = true
	}
	return cfg
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
// Without a tenant the episode cannot be scoped to a workspace, so the call
// is a no-op instead of writing tenant-less data.
func (s *MemoryServiceV2Impl) AddEpisode(ctx context.Context, tenantID, userID, sessionID string, messages []types.Message) error {
	if len(messages) == 0 {
		return nil
	}
	if strings.TrimSpace(tenantID) == "" {
		logger.Warnf(ctx, "[MemoryV2] AddEpisode skipped: empty tenantID")
		return nil
	}

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

// ConsolidateDream runs one dreamer pass for a tenant. Empty results signal
// "nothing to do" for both an unready module and a missing tenant.
func (s *MemoryServiceV2Impl) ConsolidateDream(ctx context.Context, tenantID string) (*types.DreamResult, error) {
	return s.ConsolidateDreamScope(ctx, tenantID, "")
}

// ConsolidateDreamScope runs one dreamer pass restricted to one knowledge base
// (the whole tenant when kbID is empty), using that knowledge base's model.
func (s *MemoryServiceV2Impl) ConsolidateDreamScope(ctx context.Context, tenantID, kbID string) (*types.DreamResult, error) {
	if reason := s.notReadyReason(); reason != "" {
		logger.Warnf(ctx, "[MemoryV2] ConsolidateDream skipped: %s", reason)
		return &types.DreamResult{}, nil
	}
	if strings.TrimSpace(tenantID) == "" {
		logger.Warnf(ctx, "[MemoryV2] ConsolidateDream skipped: empty tenantID")
		return &types.DreamResult{}, nil
	}
	s.initWorkers()
	if s.dreamer == nil {
		return &types.DreamResult{}, nil
	}
	return s.dreamer.RunScopePass(ctx, tenantID, kbID)
}

// ---------------------------------------------------------------------------
// AssessHealth
// ---------------------------------------------------------------------------

// AssessHealth runs all 7 health checks and returns issues. Health checks
// scope per tenant, so an empty tenant or an unready module is an error.
func (s *MemoryServiceV2Impl) AssessHealth(ctx context.Context, tenantID, kbID string) ([]*types.MemoryHealthIssue, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2: %s", reason)
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("memory V2: tenant ID is required for health assessment")
	}
	var embedderDim int
	if emb, _, err := s.embedderFor(ctx, tenantID, kbID); err == nil && emb != nil {
		embedderDim = emb.GetDimensions()
	}
	report, err := s.healthChecker.AssessHealth(ctx, tenantID, kbID, embedderDim)
	if err != nil {
		return nil, err
	}
	return report.Issues, nil
}
