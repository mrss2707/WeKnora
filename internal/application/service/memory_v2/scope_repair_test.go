package memory_v2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// scopeRepairRepo embeds the repository contract (unused methods panic) and
// adds the optional KB-scope capability on an in-memory list.
type scopeRepairRepo struct {
	interfaces.MemoryRepositoryV2
	rows        []*interfaces.MemoryKBScopeMismatch
	failIDs     map[string]bool
	moved       map[string]string // id -> fingerprint
	synced      []string
	invalidated []string
}

func (r *scopeRepairRepo) FindByID(context.Context, string) (*types.AgentMemory, error) {
	return nil, nil
}

func (r *scopeRepairRepo) ListKBScopeMismatches(_ context.Context, limit int) ([]*interfaces.MemoryKBScopeMismatch, error) {
	var out []*interfaces.MemoryKBScopeMismatch
	for _, m := range r.rows {
		if _, done := r.moved[m.MemoryID]; done {
			continue
		}
		out = append(out, m)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *scopeRepairRepo) MoveToKBOwnerTenant(_ context.Context, m *interfaces.MemoryKBScopeMismatch, fp string) error {
	if r.failIDs[m.MemoryID] {
		return assert.AnError
	}
	if r.moved == nil {
		r.moved = map[string]string{}
	}
	r.moved[m.MemoryID] = fp
	return nil
}

func (r *scopeRepairRepo) SyncRelationTenants(_ context.Context, ids []string) (int64, int64, error) {
	r.synced = append(r.synced, ids...)
	return 2, 1, nil
}

func (r *scopeRepairRepo) InvalidateResultCache(_ context.Context, tenantID string) {
	r.invalidated = append(r.invalidated, tenantID)
}

func newMismatch(id string) *interfaces.MemoryKBScopeMismatch {
	return &interfaces.MemoryKBScopeMismatch{MemoryID: id, KbID: "kb", CurrentTenant: "2", OwnerTenant: "1", Content: "c-" + id}
}

func TestRepairKBTenantScope(t *testing.T) {
	ctx := context.Background()

	t.Run("migrates with owner-scoped fingerprint and refreshes caches", func(t *testing.T) {
		repo := &scopeRepairRepo{rows: []*interfaces.MemoryKBScopeMismatch{newMismatch("a"), newMismatch("b")}}
		report, err := RepairKBTenantScope(ctx, repo, KBScopeRepairOn)
		require.NoError(t, err)
		assert.Equal(t, 2, report.Moved)
		assert.Equal(t, 2, report.Migrations["2->1"])
		assert.Equal(t, computeScopedFingerprint("1", "kb", "c-a"), repo.moved["a"])
		assert.ElementsMatch(t, []string{"a", "b"}, repo.synced)
		assert.ElementsMatch(t, []string{"1", "2"}, repo.invalidated)
		assert.EqualValues(t, 1, report.Dropped)
	})

	t.Run("audit only detects", func(t *testing.T) {
		repo := &scopeRepairRepo{rows: []*interfaces.MemoryKBScopeMismatch{newMismatch("a")}}
		report, err := RepairKBTenantScope(ctx, repo, KBScopeRepairAudit)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Detected)
		assert.Zero(t, report.Moved)
		assert.Empty(t, repo.moved)
	})

	t.Run("off does nothing", func(t *testing.T) {
		repo := &scopeRepairRepo{rows: []*interfaces.MemoryKBScopeMismatch{newMismatch("a")}}
		report, err := RepairKBTenantScope(ctx, repo, KBScopeRepairOff)
		require.NoError(t, err)
		assert.Zero(t, report.Detected)
	})

	t.Run("a persistently failing batch terminates", func(t *testing.T) {
		repo := &scopeRepairRepo{
			rows:    []*interfaces.MemoryKBScopeMismatch{newMismatch("a")},
			failIDs: map[string]bool{"a": true},
		}
		report, err := RepairKBTenantScope(ctx, repo, KBScopeRepairOn)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Failed)
		assert.Zero(t, report.Moved)
	})
}

func TestKBScopeRepairModeFromEnv(t *testing.T) {
	for env, want := range map[string]KBScopeRepairMode{
		"": KBScopeRepairOn, "on": KBScopeRepairOn, "off": KBScopeRepairOff, "audit": KBScopeRepairAudit, "bogus": KBScopeRepairAudit,
	} {
		t.Setenv("MEMORY_V2_KB_SCOPE_REPAIR", env)
		assert.Equal(t, want, KBScopeRepairModeFromEnv(), "env=%q", env)
	}
}
