package memory_v2

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patchRepo records UpdateFields calls on top of the ingestion repo fake.
type patchRepo struct {
	*ingestionRepo
	fields    map[string]interface{}
	updateErr error
	calls     int
}

func (r *patchRepo) UpdateFields(_ context.Context, tenantID, id string, fields map[string]interface{}) error {
	r.calls++
	r.fields = fields
	return r.updateErr
}

func newPatchService() (*MemoryServiceV2Impl, *patchRepo) {
	base := newIngestionRepo()
	base.memories["m1"] = &types.AgentMemory{ID: "m1", TenantID: "t1", KbID: "kb1", Content: "a stored memory"}
	repo := &patchRepo{ingestionRepo: base}
	svc := newIngestionService(base)
	svc.repo = repo
	return svc, repo
}

func ptr[T any](v T) *T { return &v }

func TestPatchMemory_WritesOnlyProvidedFields(t *testing.T) {
	svc, repo := newPatchService()
	verdict := types.VerdictGotcha

	got, err := svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{
		Verdict: &verdict, Importance: ptr(0), Tags: &[]string{" db ", "DB", "pool"},
	})

	require.NoError(t, err)
	assert.Equal(t, "m1", got.ID)
	assert.Equal(t, 1, repo.calls)
	assert.Equal(t, map[string]interface{}{
		"verdict":    "gotcha",
		"importance": 0,
		"tags":       types.TagsArray{"db", "pool"},
	}, repo.fields, "explicit zero kept, omitted fields untouched, tags trimmed and de-duplicated")
	assert.Zero(t, repo.createCalls, "a patch never re-runs ingestion")
	assert.Zero(t, repo.cosineCalls)
}

func TestPatchMemory_Validation(t *testing.T) {
	bad := types.MemoryVerdict("bogus")
	longTag := make([]rune, maxTagRunes+1)
	for i := range longTag {
		longTag[i] = 'ữ'
	}
	tooMany := make([]string, maxTags+1)
	for i := range tooMany {
		tooMany[i] = string(rune('a' + i))
	}
	cases := map[string]interfaces.MemoryV2Patch{
		"verdict":     {Verdict: &bad},
		"importance+": {Importance: ptr(maxImportance + 1)},
		"importance-": {Importance: ptr(minImportance - 1)},
		"tier":        {Tier: ptr(4)},
		"type":        {MemoryType: ptr("gossip")},
		"long tag":    {Tags: &[]string{string(longTag)}},
		"many tags":   {Tags: &tooMany},
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo := newPatchService()
			_, err := svc.PatchMemory(context.Background(), "t1", "m1", patch)
			var v *types.ErrMemoryValidation
			require.ErrorAs(t, err, &v)
			assert.Zero(t, repo.calls, "invalid patches never reach the repository")
		})
	}
}

func TestPatchMemory_PropagatesRepositoryErrors(t *testing.T) {
	svc, repo := newPatchService()
	repo.updateErr = &types.ErrProtectedVerdict{}
	_, err := svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{Tier: ptr(1)})
	var protected *types.ErrProtectedVerdict
	assert.True(t, errors.As(err, &protected))
}

func TestPatchMemory_RejectsMissingScopeAndUnready(t *testing.T) {
	svc, _ := newPatchService()
	_, err := svc.PatchMemory(context.Background(), "", "m1", interfaces.MemoryV2Patch{Tier: ptr(1)})
	var v *types.ErrMemoryValidation
	require.ErrorAs(t, err, &v)

	svc.config.Enabled = false
	_, err = svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{Tier: ptr(1)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}

func TestTruncateRunes_KeepsUTF8Valid(t *testing.T) {
	assert.Equal(t, "Kết", truncateRunes("Kết nối", 3))
	assert.Equal(t, "abc", truncateRunes("abc", 10))
	assert.Equal(t, "abc", truncateRunes("abc", 0))
}
