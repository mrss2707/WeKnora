package memory_v2

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergeStoreRepo records MergeInto/RestoreMerge calls and audit rows.
type mergeStoreRepo struct {
	*ingestionRepo
	merged   *types.AgentMemory
	losers   []string
	audit    []*types.MemoryAuditEntry
	restored *types.AgentMemory
	revived  []*types.AgentMemory
}

func (r *mergeStoreRepo) MergeInto(_ context.Context, survivor *types.AgentMemory, loserIDs []string, audit []*types.MemoryAuditEntry) error {
	cp := *survivor
	r.merged, r.losers = &cp, loserIDs
	r.audit = append(r.audit, audit...)
	r.memories[survivor.ID] = &cp
	for _, id := range loserIDs {
		delete(r.memories, id)
	}
	return nil
}

func (r *mergeStoreRepo) RestoreMerge(_ context.Context, before *types.AgentMemory, losers []*types.AgentMemory, audit []*types.MemoryAuditEntry) error {
	r.restored, r.revived = before, losers
	r.audit = append(r.audit, audit...)
	cp := *before
	r.memories[before.ID] = &cp
	return nil
}

func (r *mergeStoreRepo) RecordAudit(_ context.Context, entries ...*types.MemoryAuditEntry) error {
	r.audit = append(r.audit, entries...)
	return nil
}

func (r *mergeStoreRepo) ListAudit(_ context.Context, f types.MemoryAuditFilter) ([]*types.MemoryAuditEntry, int64, error) {
	var out []*types.MemoryAuditEntry
	for i := len(r.audit) - 1; i >= 0; i-- {
		e := r.audit[i]
		if e.MemoryID != f.MemoryID {
			continue
		}
		if len(f.Actions) > 0 {
			ok := false
			for _, a := range f.Actions {
				ok = ok || a == e.Action
			}
			if !ok {
				continue
			}
		}
		out = append(out, e)
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, int64(len(out)), nil
}

func newMergeFixture() (*MemoryServiceV2Impl, *mergeStoreRepo) {
	repo := &mergeStoreRepo{ingestionRepo: newIngestionRepo()}
	svc := newIngestionService(repo.ingestionRepo)
	svc.repo = repo
	return svc, repo
}

func mem(id, kb, content string, importance int, tags ...string) *types.AgentMemory {
	return &types.AgentMemory{ID: id, TenantID: "1", KbID: kb, Content: content, Importance: importance, Tags: types.TagsArray(tags), Verdict: types.VerdictNone}
}

func TestMergeMemories_UnionsAndAuditsAtomically(t *testing.T) {
	svc, repo := newMergeFixture()
	survivor := mem("s", "kb", "Pool exhausted under load", 1, "postgres", "Pool")
	loser := mem("l", "kb", "Raised max_open_conns to fifty", 4, "pool", "config")
	repo.memories["s"], repo.memories["l"] = survivor, loser

	got, err := svc.mergeMemories(context.Background(), survivor, []*types.AgentMemory{loser}, nil)

	require.NoError(t, err)
	assert.Equal(t, "Pool exhausted under load\n---\nRaised max_open_conns to fifty", got.Content)
	assert.Equal(t, 4, got.Importance, "importance is the max")
	assert.Equal(t, types.TagsArray{"postgres", "Pool", "config"}, got.Tags, "tags unioned case-insensitively")
	assert.Equal(t, []string{"l"}, repo.losers)
	assert.Contains(t, string(got.Metadata), `"merged_from":["l"]`)
	require.NotNil(t, got.Fingerprint)
	assert.Equal(t, computeScopedFingerprint("1", "kb", got.Content), *got.Fingerprint, "fingerprint follows the new text")
	require.Len(t, repo.audit, 2)
	assert.Equal(t, types.MemoryAuditMerged, repo.audit[0].Action)
	assert.NotEmpty(t, repo.audit[0].Before, "before image kept for undo")
	assert.Equal(t, types.MemoryAuditMergedIn, repo.audit[1].Action)
	assert.Equal(t, "l", repo.audit[1].MemoryID)
}

func TestMergeMemories_Guards(t *testing.T) {
	svc, _ := newMergeFixture()
	ctx := context.Background()
	survivor := mem("s", "kb", "survivor text", 0)

	_, err := svc.mergeMemories(ctx, survivor, []*types.AgentMemory{mem("x", "other-kb", "other text", 0)}, nil)
	assert.ErrorContains(t, err, "across tenants or knowledge bases")

	protected := mem("p", "kb", "a decision", 0)
	protected.Verdict = types.VerdictDecision
	_, err = svc.mergeMemories(ctx, survivor, []*types.AgentMemory{protected}, nil)
	var pe *types.ErrProtectedVerdict
	assert.ErrorAs(t, err, &pe, "a protected memory is never merged away")

	svc.config.SemanticDedup.MergeMaxChars = 20
	_, err = svc.mergeMemories(ctx, survivor, nil, mem("", "kb", "this pushes it over", 0))
	assert.ErrorIs(t, err, errMergeTooLong)
	assert.Equal(t, "survivor text", survivor.Content, "survivor untouched when skipped")
}

func TestCheckSemanticDedup_NeverRewritesProtectedMemory(t *testing.T) {
	svc, repo := newMergeFixture()
	decision := mem("d", "kb", "Decided to use pgbouncer", 4)
	decision.Verdict = types.VerdictDecision
	repo.memories["d"] = decision
	repo.cosineResults = []*types.MemorySearchResult{{Memory: &types.AgentMemory{ID: "d"}, Score: 0.95}}

	res, err := svc.checkSemanticDedup(context.Background(), mem("", "kb", "We use pgbouncer now", 0), []float32{1})

	require.NoError(t, err)
	assert.Nil(t, res, "stored as a new memory instead of merging into a decision")
	assert.Nil(t, repo.merged)
	assert.Equal(t, "Decided to use pgbouncer", decision.Content)
}

func TestUndoMerge_RestoresSurvivorAndLosers(t *testing.T) {
	svc, repo := newMergeFixture()
	survivor := mem("s", "kb", "original survivor", 1)
	loser := mem("l", "kb", "absorbed memory", 2)
	repo.memories["s"], repo.memories["l"] = survivor, loser
	_, err := svc.mergeMemories(context.Background(), survivor, []*types.AgentMemory{loser}, nil)
	require.NoError(t, err)

	restored, err := svc.UndoMerge(context.Background(), "1", "kb", "s")

	require.NoError(t, err)
	assert.Equal(t, "original survivor", restored.Content)
	assert.Equal(t, "original survivor", repo.restored.Content)
	require.Len(t, repo.revived, 1)
	assert.Equal(t, "l", repo.revived[0].ID)
	assert.Equal(t, types.MemoryAuditUnmerged, repo.audit[len(repo.audit)-1].Action)

	_, err = svc.UndoMerge(context.Background(), "1", "kb", "s")
	var v *types.ErrMemoryValidation
	assert.ErrorAs(t, err, &v, "an undo cannot be undone twice")
}

// accessRepo records TouchAccess calls.
type accessRepo struct {
	*ingestionRepo
	mu      sync.Mutex
	touched map[string][]string
}

func (r *accessRepo) TouchAccess(_ context.Context, tenantID string, ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.touched[tenantID] = append(r.touched[tenantID], ids...)
	return nil
}

func TestSearchMemories_TracksAccessOfReturnedMemories(t *testing.T) {
	base := newIngestionRepo()
	hit := &types.AgentMemory{ID: "hit", TenantID: "1", KbID: "kb", Content: "pgbouncer transaction mode", Importance: 6, CreatedAt: time.Now()}
	base.cosineResults = []*types.MemorySearchResult{{Memory: hit, Score: 0.99}}
	repo := &accessRepo{ingestionRepo: base, touched: map[string][]string{}}
	svc := newIngestionService(base)
	svc.repo = repo

	results, err := svc.SearchMemories(context.Background(), "pgbouncer", &types.MemoryFilter{TenantID: "1", KbID: "kb", Limit: 5})

	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.touched["1"]) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"hit"}, repo.touched["1"])
}

func TestReclaimOrphanedMemories_Modes(t *testing.T) {
	assert := assert.New(t)
	n, err := ReclaimOrphanedMemories(context.Background(), newIngestionRepo(), KBScopeRepairOn)
	assert.NoError(err)
	assert.Zero(n, "repositories without the capability are skipped")

	repo := &orphanRepo{ingestionRepo: newIngestionRepo(), orphans: []*types.AgentMemory{
		{ID: "o1", TenantID: "1", KbID: "gone"}, {ID: "o2", TenantID: "2", KbID: "gone"},
	}}
	n, err = ReclaimOrphanedMemories(context.Background(), repo, KBScopeRepairAudit)
	assert.NoError(err)
	assert.Equal(2, n)
	assert.Empty(repo.deleted, "audit mode never deletes")

	n, err = ReclaimOrphanedMemories(context.Background(), repo, KBScopeRepairOff)
	assert.NoError(err)
	assert.Zero(n)

	n, err = ReclaimOrphanedMemories(context.Background(), repo, KBScopeRepairOn)
	assert.NoError(err)
	assert.Equal(2, n)
	assert.Equal(map[string][]string{"1": {"o1"}, "2": {"o2"}}, repo.deleted, "deleted per tenant")
	assert.Len(repo.audit, 2)
	assert.Equal(types.MemoryAuditReclaimed, repo.audit[0].Action)
	assert.Equal("system", repo.audit[0].Actor)
}

type orphanRepo struct {
	*ingestionRepo
	orphans []*types.AgentMemory
	deleted map[string][]string
	audit   []*types.MemoryAuditEntry
}

func (r *orphanRepo) ListOrphanedKBMemories(context.Context, int) ([]*types.AgentMemory, error) {
	return r.orphans, nil
}

func (r *orphanRepo) SoftDeleteMemoriesWithRelations(_ context.Context, tenantID string, ids []string) error {
	if r.deleted == nil {
		r.deleted = map[string][]string{}
	}
	r.deleted[tenantID] = append(r.deleted[tenantID], ids...)
	r.orphans = nil
	return nil
}

func (r *orphanRepo) RecordAudit(_ context.Context, e ...*types.MemoryAuditEntry) error {
	r.audit = append(r.audit, e...)
	return nil
}

func (r *orphanRepo) ListAudit(context.Context, types.MemoryAuditFilter) ([]*types.MemoryAuditEntry, int64, error) {
	return nil, 0, nil
}
