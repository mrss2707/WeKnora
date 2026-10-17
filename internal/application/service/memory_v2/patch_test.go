package memory_v2

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patchRepo records UpdateFields calls on top of the ingestion repo fake.
type patchRepo struct {
	*ingestionRepo
	fields    map[string]interface{}
	expected  *time.Time
	updateErr error
	calls     int
}

func (r *patchRepo) UpdateFields(ctx context.Context, tenantID, id string, fields map[string]interface{}) error {
	return r.UpdateFieldsIf(ctx, tenantID, id, fields, nil)
}

func (r *patchRepo) UpdateFieldsIf(_ context.Context, tenantID, id string, fields map[string]interface{}, expected *time.Time) error {
	r.calls++
	r.fields = fields
	r.expected = expected
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

func TestPatchMemory_ContentEditReembedsAndPassesLock(t *testing.T) {
	svc, repo := newPatchService()
	embedded := ""
	svc.embedder = &mockEmbedder{embedFunc: func(_ context.Context, text string) ([]float32, error) {
		embedded = text
		return []float32{0.5, 0.5}, nil
	}}
	version := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)

	_, err := svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{
		Content: ptr("Kết nối postgres đã tăng lên 50"), ExpectedUpdatedAt: &version,
	})

	require.NoError(t, err)
	assert.Equal(t, "Kết nối postgres đã tăng lên 50", embedded, "new content is re-embedded")
	assert.Equal(t, "Kết nối postgres đã tăng lên 50", repo.fields["content"])
	assert.Equal(t, computeScopedFingerprint("t1", "kb1", "Kết nối postgres đã tăng lên 50"), repo.fields["fingerprint"])
	assert.Contains(t, repo.fields, "embedding")
	require.NotNil(t, repo.expected)
	assert.True(t, version.Equal(*repo.expected), "the optimistic lock reaches the repository")
}

func TestPatchMemory_ContentDuplicateOfAnotherIsConflict(t *testing.T) {
	svc, repo := newPatchService()
	fp := computeScopedFingerprint("t1", "kb1", "text owned by m2")
	repo.memories["m2"] = &types.AgentMemory{ID: "m2", TenantID: "t1", KbID: "kb1", Content: "text owned by m2", Fingerprint: &fp}

	_, err := svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{Content: ptr("text owned by m2")})

	var conflict *types.MemoryV2ConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "m2", conflict.ExistingID)
	assert.Zero(t, repo.calls)
}

func TestPatchMemory_InvalidContentAndUnchangedContent(t *testing.T) {
	svc, repo := newPatchService()
	_, err := svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{Content: ptr("short")})
	var v *types.ErrMemoryValidation
	require.ErrorAs(t, err, &v)

	got, err := svc.PatchMemory(context.Background(), "t1", "m1", interfaces.MemoryV2Patch{Content: ptr("a stored memory")})
	require.NoError(t, err)
	assert.Equal(t, "m1", got.ID)
	assert.Zero(t, repo.calls, "unchanged content writes nothing")
}

func TestValidateContent_CountsCharactersNotBytes(t *testing.T) {
	vi := make([]rune, maxContentChars)
	for i := range vi {
		vi[i] = 'ữ' // 3 bytes each
	}
	assert.NoError(t, validateContent(string(vi)), "10000 Vietnamese characters are allowed")
	assert.Error(t, validateContent(string(vi)+"a"))
	assert.Error(t, validateContent("ữữữữữữữữữ"), "9 characters is too short even though it is 27 bytes")
}
