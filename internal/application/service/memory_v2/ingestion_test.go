package memory_v2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ingestionRepo struct {
	memories       map[string]*types.AgentMemory
	createErr      error
	updateErr      error
	fingerprintErr error
	cosineErr      error
	cosineResults  []*types.MemorySearchResult
	searchResults  []*types.MemorySearchResult

	createCalls int
	updateCalls int
	cosineCalls int
	searchCalls int
	nextID      int

	lastCosineFilter *types.MemoryFilter
	lastCosineLimit  int
	lastCosineVector []float32
}

var _ interfaces.MemoryRepositoryV2 = (*ingestionRepo)(nil)

func newIngestionRepo() *ingestionRepo {
	return &ingestionRepo{memories: make(map[string]*types.AgentMemory)}
}

func (r *ingestionRepo) Create(ctx context.Context, memory *types.AgentMemory) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.createCalls++
	if memory.ID == "" {
		r.nextID++
		memory.ID = fmt.Sprintf("ingestion-mem-%d", r.nextID)
	}
	r.memories[memory.ID] = memory
	return nil
}

func (r *ingestionRepo) GetByID(ctx context.Context, tenantID, id string) (*types.AgentMemory, error) {
	memory := r.memories[id]
	if memory == nil || memory.TenantID != tenantID {
		return nil, nil
	}
	return memory, nil
}

func (r *ingestionRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*types.AgentMemory, error) {
	if r.fingerprintErr != nil {
		return nil, r.fingerprintErr
	}
	for _, memory := range r.memories {
		if memory.TenantID == tenantID && memory.Fingerprint != nil && *memory.Fingerprint == fingerprint {
			return memory, nil
		}
	}
	return nil, nil
}

func (r *ingestionRepo) Update(ctx context.Context, memory *types.AgentMemory) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updateCalls++
	r.memories[memory.ID] = memory
	return nil
}

func (r *ingestionRepo) Delete(ctx context.Context, tenantID, id string) error {
	delete(r.memories, id)
	return nil
}

func (r *ingestionRepo) CreateRelation(ctx context.Context, rel *types.MemoryRelation) error {
	return nil
}
func (r *ingestionRepo) GetRelations(ctx context.Context, memoryID, tenantID string) ([]*types.MemoryRelation, error) {
	return nil, nil
}
func (r *ingestionRepo) DeleteRelation(ctx context.Context, id, tenantID string) error { return nil }

func (r *ingestionRepo) Search(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
	r.searchCalls++
	return r.searchResults, int64(len(r.searchResults)), nil
}

func (r *ingestionRepo) CosineSearch(ctx context.Context, filter *types.MemoryFilter, embedding []float32, limit int) ([]*types.MemorySearchResult, error) {
	r.cosineCalls++
	r.lastCosineFilter = filter
	r.lastCosineLimit = limit
	r.lastCosineVector = append([]float32(nil), embedding...)
	if r.cosineErr != nil {
		return nil, r.cosineErr
	}
	return r.cosineResults, nil
}

func (r *ingestionRepo) TryDreamerLock(ctx context.Context, tenantID string, workerID string) (bool, error) {
	return true, nil
}
func (r *ingestionRepo) UnlockDreamer(ctx context.Context, tenantID string) error    { return nil }
func (r *ingestionRepo) ComputeHubScores(ctx context.Context, tenantID string) error { return nil }
func (r *ingestionRepo) HardDeleteExpired(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	return 0, nil
}
func (r *ingestionRepo) InvalidateResultCache(ctx context.Context, tenantID string)  {}
func (r *ingestionRepo) SetCacheInvalidator(invalidator interfaces.CacheInvalidator) {}
func (r *ingestionRepo) GetEmbeddingDimension(ctx context.Context, tenantID string) (int, error) {
	return 3, nil
}

func newIngestionService(repo *ingestionRepo) *MemoryServiceV2Impl {
	config := types.DefaultMemoryV2Config()
	config.LintOnWrite.Enabled = false
	return &MemoryServiceV2Impl{
		repo:        repo,
		embedder:    &mockEmbedder{},
		config:      config,
		tokenBudget: NewTokenBudgetManager(),
	}
}

func TestValidateContentRejectsInvalidContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "too short", content: "short", want: "less than minimum"},
		{name: "too little non-whitespace", content: "     a     ", want: "non-whitespace"},
		{name: "too long", content: strings.Repeat("x", 10001), want: "exceeds maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContent(tt.content)
			require.Error(t, err)
			var validationErr *ErrMemoryValidation
			require.ErrorAs(t, err, &validationErr)
			assert.Contains(t, validationErr.Message, tt.want)
		})
	}

	assert.NoError(t, validateContent("valid memory content"))
}

func TestComputeFingerprintUsesStableSHA256(t *testing.T) {
	assert.Equal(t,
		"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		computeFingerprint("hello"),
	)
	assert.NotEqual(t, computeFingerprint("hello"), computeFingerprint("hello "))
}

func TestIngestionClassifiersAndScoring(t *testing.T) {
	typeCases := []struct {
		content string
		want    string
	}{
		{content: "Yesterday the outage event happened earlier and we remember the incident clearly.", want: "episodic"},
		{content: "A vector database is a system that is used for embedding retrieval and typically stores vectors.", want: "semantic"},
		{content: "Workflow steps: first prepare input, then validate data, next run process, finally review output.", want: "procedural"},
		{content: "We decided, approved, agreed, and finalized the decision to use PostgreSQL memory storage.", want: "decision"},
		{content: "I prefer dark mode and would rather use compact layouts because that option is better.", want: "preference"},
		{content: "Unique project note without any configured classifier keyword.", want: "fact"},
	}
	for _, tt := range typeCases {
		assert.Equal(t, tt.want, detectMemoryType(tt.content), tt.want)
	}

	tags := suggestTags("The architecture, architecture, and MEMORY pipeline requires PostgreSQL vectors for retrieval.", "decision")
	assert.LessOrEqual(t, len(tags), 10)
	assert.Equal(t, "decision", tags[0])
	assert.Contains(t, tags, "architecture")
	assert.Contains(t, tags, "memory")
	assert.NotContains(t, tags, "the")
	assert.NotContains(t, tags, "and")

	importantDecision := computeImportance("Critical approved decision required for the memory system.", "decision")
	assert.Equal(t, 6, importantDecision)
	assert.Equal(t, -1, computeImportance("Maybe perhaps possibly unclear and not sure.", "fact"))

	assert.Equal(t, 1, assignTier(0, "decision"))
	assert.Equal(t, 1, assignTier(4, "semantic"))
	assert.Equal(t, 2, assignTier(2, "semantic"))
	assert.Equal(t, 3, assignTier(-3, "fact"))
	assert.Equal(t, 2, assignTier(0, "fact"))

	assert.Equal(t, -5, clampInt(-10, -5, 6))
	assert.Equal(t, 6, clampInt(10, -5, 6))
	assert.Equal(t, 3, clampInt(3, -5, 6))
}

func TestCheckSemanticDedupEmptyVectorSkipsSearch(t *testing.T) {
	repo := newIngestionRepo()
	svc := newIngestionService(repo)

	result, err := svc.checkSemanticDedup(context.Background(), &types.AgentMemory{TenantID: "tenant-1"}, nil)

	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Zero(t, repo.cosineCalls)
}

func TestCheckSemanticDedupExactDuplicateReturnsTypedError(t *testing.T) {
	repo := newIngestionRepo()
	repo.cosineResults = []*types.MemorySearchResult{
		{Memory: nil, Score: 1.0},
		{Memory: &types.AgentMemory{ID: "existing-1"}, Score: 0.98},
	}
	svc := newIngestionService(repo)
	svc.config.SemanticDedup.ExactThreshold = 0.97
	svc.config.SemanticDedup.NearThreshold = 0.93
	svc.config.SemanticDedup.MaxMerges = 4

	result, err := svc.checkSemanticDedup(context.Background(), &types.AgentMemory{TenantID: "tenant-1", KbID: "kb-1"}, []float32{0.1, 0.2})

	assert.Nil(t, result)
	require.Error(t, err)
	var duplicateErr *ErrMemoryDuplicate
	require.ErrorAs(t, err, &duplicateErr)
	assert.Equal(t, "existing-1", duplicateErr.ExistingID)
	assert.Equal(t, 0.98, duplicateErr.Similarity)
	assert.Equal(t, "tenant-1", repo.lastCosineFilter.TenantID)
	assert.Equal(t, "kb-1", repo.lastCosineFilter.KbID)
	assert.Equal(t, 4, repo.lastCosineLimit)
	assert.Equal(t, []float32{0.1, 0.2}, repo.lastCosineVector)
}

func TestCheckSemanticDedupSearchErrorIsWrapped(t *testing.T) {
	repo := newIngestionRepo()
	repo.cosineErr = errors.New("vector index down")
	svc := newIngestionService(repo)

	result, err := svc.checkSemanticDedup(context.Background(), &types.AgentMemory{TenantID: "tenant-1"}, []float32{0.1})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "semantic dedup search failed")
}

func TestCheckSemanticDedupNearDuplicateMergesAndTruncates(t *testing.T) {
	existing := &types.AgentMemory{ID: "existing-1", TenantID: "tenant-1", Content: "Existing memory content", Importance: 1}
	repo := newIngestionRepo()
	repo.cosineResults = []*types.MemorySearchResult{{Memory: existing, Score: 0.95}}
	svc := newIngestionService(repo)
	svc.config.SemanticDedup.ExactThreshold = 0.99
	svc.config.SemanticDedup.NearThreshold = 0.93
	svc.config.SemanticDedup.MergeMaxChars = 45

	result, err := svc.checkSemanticDedup(context.Background(), &types.AgentMemory{TenantID: "tenant-1", Content: "New memory content with extra details", Importance: 4}, []float32{0.1})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Created)
	assert.Same(t, existing, result.Memory)
	assert.Equal(t, 1, repo.updateCalls)
	assert.LessOrEqual(t, len(existing.Content), 45)
	assert.Contains(t, existing.Content, "Existing memory content")
	assert.Equal(t, 4, existing.Importance)
}

func TestMergeMemoryUpdateErrorIsWrapped(t *testing.T) {
	repo := newIngestionRepo()
	repo.updateErr = errors.New("write conflict")
	svc := newIngestionService(repo)

	result, err := svc.mergeMemory(context.Background(), &types.AgentMemory{Content: "new content"}, &types.AgentMemory{ID: "existing-1", Content: "existing content"}, 0.94)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge update failed")
}

func TestSaveMemoryExactFingerprintDuplicateSkipsEmbeddingAndCreate(t *testing.T) {
	content := "valid duplicate memory content"
	fingerprint := computeFingerprint(content)
	existing := &types.AgentMemory{ID: "existing-1", TenantID: "tenant-1", Content: content, Fingerprint: &fingerprint}
	repo := newIngestionRepo()
	repo.memories[existing.ID] = existing
	svc := newIngestionService(repo)
	svc.embedder = &mockEmbedder{embedFunc: func(ctx context.Context, text string) ([]float32, error) {
		t.Fatalf("exact fingerprint duplicate should not call embedder")
		return nil, nil
	}}

	result, err := svc.SaveMemory(context.Background(), &types.AgentMemory{TenantID: "tenant-1", Content: content})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Created)
	assert.Same(t, existing, result.Memory)
	assert.Zero(t, repo.createCalls)
	assert.Zero(t, repo.cosineCalls)
}

func TestSaveMemoryValidationAndLookupErrors(t *testing.T) {
	t.Run("nil memory", func(t *testing.T) {
		svc := newIngestionService(newIngestionRepo())
		result, err := svc.SaveMemory(context.Background(), nil)
		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil memory")
	})

	t.Run("invalid content", func(t *testing.T) {
		svc := newIngestionService(newIngestionRepo())
		result, err := svc.SaveMemory(context.Background(), &types.AgentMemory{TenantID: "tenant-1", Content: "short"})
		assert.Nil(t, result)
		require.Error(t, err)
		var validationErr *ErrMemoryValidation
		assert.ErrorAs(t, err, &validationErr)
	})

	t.Run("fingerprint lookup", func(t *testing.T) {
		repo := newIngestionRepo()
		repo.fingerprintErr = errors.New("database unavailable")
		svc := newIngestionService(repo)
		result, err := svc.SaveMemory(context.Background(), &types.AgentMemory{TenantID: "tenant-1", Content: "valid memory content"})
		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fingerprint lookup failed")
	})
}

func TestSaveMemoryEmbeddingAndStoreErrors(t *testing.T) {
	t.Run("embedding failure", func(t *testing.T) {
		repo := newIngestionRepo()
		svc := newIngestionService(repo)
		svc.embedder = &mockEmbedder{embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return nil, errors.New("embedding provider down")
		}}

		result, err := svc.SaveMemory(context.Background(), &types.AgentMemory{TenantID: "tenant-1", Content: "valid memory content"})

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "embedding failed")
		assert.Zero(t, repo.createCalls)
	})

	t.Run("store failure", func(t *testing.T) {
		repo := newIngestionRepo()
		repo.createErr = errors.New("insert failed")
		svc := newIngestionService(repo)

		result, err := svc.SaveMemory(context.Background(), &types.AgentMemory{TenantID: "tenant-1", Content: "valid memory content"})

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "store failed")
	})
}

func TestSaveMemoryUniqueMemoryPopulatesDerivedFieldsAndStores(t *testing.T) {
	repo := newIngestionRepo()
	svc := newIngestionService(repo)
	memory := &types.AgentMemory{
		TenantID: "tenant-1",
		KbID:     "kb-1",
		UserID:   "user-1",
		Content:  "We decided and approved the critical decision to use PostgreSQL memory storage.",
	}

	result, err := svc.SaveMemory(context.Background(), memory)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Created)
	assert.Same(t, memory, result.Memory)
	assert.Equal(t, 1, repo.createCalls)
	assert.Equal(t, 1, repo.cosineCalls)
	assert.Equal(t, "decision", memory.MemoryType)
	assert.Contains(t, memory.Tags, "decision")
	assert.Contains(t, memory.Tags, "critical")
	assert.Equal(t, 6, memory.Importance)
	assert.Equal(t, 1, memory.Tier)
	assert.Equal(t, types.VerdictNone, memory.Verdict)
	require.NotNil(t, memory.Fingerprint)
	assert.Equal(t, computeFingerprint(memory.Content), *memory.Fingerprint)
	assert.False(t, memory.CreatedAt.IsZero())
	assert.False(t, memory.UpdatedAt.IsZero())
	assert.Equal(t, "tenant-1", repo.lastCosineFilter.TenantID)
	assert.Equal(t, "kb-1", repo.lastCosineFilter.KbID)
	assert.Equal(t, 3, repo.lastCosineLimit)
	assert.Contains(t, repo.memories, memory.ID)
}
