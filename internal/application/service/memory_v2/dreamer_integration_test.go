package memory_v2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/workers"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestScopeLock_OnPostgres(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	ctx := context.Background()

	ok, state, err := repo.TryScopeLock(ctx, "1", "kb-a", "w1", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Zero(t, state.Runs)
	assert.Nil(t, state.LastRunAt)

	ok, _, err = repo.TryScopeLock(ctx, "1", "kb-a", "w2", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "a live lock is exclusive")

	ok, _, err = repo.TryScopeLock(ctx, "1", "kb-b", "w2", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "other knowledge bases lock independently")

	alive, err := repo.HeartbeatScopeLock(ctx, "1", "kb-a", "w1", time.Minute)
	require.NoError(t, err)
	assert.True(t, alive)
	alive, err = repo.HeartbeatScopeLock(ctx, "1", "kb-a", "w2", time.Minute)
	require.NoError(t, err)
	assert.False(t, alive, "only the holder can extend the lock")

	require.NoError(t, repo.ReleaseScopeLock(ctx, "1", "kb-a", "w1", true, map[string]interface{}{"proposed": 2}))
	ok, state, err = repo.TryScopeLock(ctx, "1", "kb-a", "w2", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, state.Runs)
	require.NotNil(t, state.LastRunAt)
	require.NoError(t, repo.ReleaseScopeLock(ctx, "1", "kb-a", "w2", false, nil))

	// An expired lock of a crashed worker is taken over.
	ok, _, err = repo.TryScopeLock(ctx, "1", "kb-c", "crashed", -time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	ok, _, err = repo.TryScopeLock(ctx, "1", "kb-c", "w3", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "expired locks are taken over")
}

func TestScopeChangesAndMarkDreamed_OnPostgres(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	ctx := context.Background()
	a := createPGServiceMemory(t, db, "1", "kb", "alpha memory", []float32{1, 0})
	b := createPGServiceMemory(t, db, "1", "kb", "beta memory", []float32{0.99, 0.14})
	c := createPGServiceMemory(t, db, "1", "kb", "gamma memory", []float32{0, 1})
	createPGServiceMemory(t, db, "1", "other-kb", "foreign memory", []float32{1, 0})

	changed, err := repo.ScopeChangedSince(ctx, "1", "kb", nil)
	require.NoError(t, err)
	assert.True(t, changed, "never dreamed: changed")

	before := map[string]time.Time{}
	for _, m := range []*types.AgentMemory{a, b, c} {
		got, err := repo.GetByID(ctx, "1", m.ID)
		require.NoError(t, err)
		before[m.ID] = got.UpdatedAt
	}
	time.Sleep(20 * time.Millisecond)
	mark := time.Now()
	require.NoError(t, repo.MarkDreamed(ctx, "1", []string{a.ID, b.ID}, mark))

	for id, ts := range before {
		got, err := repo.GetByID(ctx, "1", id)
		require.NoError(t, err)
		assert.True(t, got.UpdatedAt.Equal(ts), "marking reviewed is not an edit")
	}
	changed, err = repo.ScopeChangedSince(ctx, "1", "kb", &mark)
	require.NoError(t, err)
	assert.False(t, changed, "review marks do not count as changes")

	batch, err := repo.LeastRecentlyDreamed(ctx, "1", "kb", 2)
	require.NoError(t, err)
	require.Len(t, batch, 2)
	assert.Equal(t, c.ID, batch[0].ID, "never-reviewed memories first")
	for _, m := range batch {
		assert.Equal(t, "kb", m.KbID)
	}

	sim, err := repo.PairSimilarity(ctx, "1", a.ID, b.ID)
	require.NoError(t, err)
	assert.InDelta(t, 0.99, sim, 0.01)
	sim, err = repo.PairSimilarity(ctx, "1", a.ID, c.ID)
	require.NoError(t, err)
	assert.InDelta(t, 0, sim, 0.01)

	require.NoError(t, repo.UpdateFields(ctx, "1", c.ID, map[string]interface{}{"importance": 3}))
	changed, err = repo.ScopeChangedSince(ctx, "1", "kb", &mark)
	require.NoError(t, err)
	assert.True(t, changed, "a real edit is a change")
}

// TestDreamer_EndToEndOnPostgres runs a live (non dry-run) pass through the
// service applier: allowed changes land with an audit trail, protected and
// out-of-scope memories are untouched, and a second scheduled pass skips the
// unchanged scope.
func TestDreamer_EndToEndOnPostgres(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	ctx := context.Background()
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)

	plain := createPGServiceMemory(t, db, "1", "kb", "1 Deploys on Friday failed twice", []float32{1, 0})
	other := createPGServiceMemory(t, db, "1", "kb", "2 The staging cluster runs on three nodes", []float32{0, 1})
	protected := createPGServiceMemory(t, db, "1", "kb", "3 We decided to use Postgres", []float32{1, 1})
	require.NoError(t, repo.UpdateFields(ctx, "1", protected.ID, map[string]interface{}{"verdict": types.VerdictDecision}))
	foreign := createPGServiceMemory(t, db, "1", "kb-2", "4 Foreign knowledge base memory", []float32{1, 0})

	calls := 0
	model := &mockChat{chatFunc: func(_ context.Context, msgs []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
		calls++
		prompt := msgs[len(msgs)-1].Content
		// The prompt lists "[N] type=..." then content="<JSON string>" on the next line.
		idx := func(prefix string) int {
			lines := strings.Split(prompt, "\n")
			for i := 0; i+1 < len(lines); i++ {
				var n int
				if _, err := fmt.Sscanf(lines[i], "[%d]", &n); err == nil && strings.HasPrefix(strings.TrimPrefix(strings.TrimSpace(lines[i+1]), `content="`), prefix) {
					return n
				}
			}
			return 0
		}
		return &types.ChatResponse{Content: fmt.Sprintf(`{"actions":[
			{"type":"update_verdict","index":%d,"new_verdict":"gotcha","confidence":0.95,"reason":"pitfall"},
			{"type":"adjust_importance","index":%d,"delta":1,"confidence":0.9,"reason":"useful"},
			{"type":"update_verdict","index":%d,"new_verdict":"refuted","confidence":0.99,"reason":"protected, must be ignored"}
		]}`, idx("1 "), idx("2 "), idx("3 "))}, nil
	}}
	dreamer := workers.NewDreamerWorker(repo, model, cfg.Dreamer, nil).WithApplier(svc)

	res, err := dreamer.RunScopePass(ctx, "1", "kb")
	require.NoError(t, err)
	assert.Equal(t, 2, res.ActionsProposed, "the protected proposal is dropped")
	assert.Equal(t, 2, res.ActionsApplied)

	got, err := repo.GetByID(ctx, "1", plain.ID)
	require.NoError(t, err)
	assert.Equal(t, types.MemoryVerdict("gotcha"), got.Verdict)
	got, err = repo.GetByID(ctx, "1", other.ID)
	require.NoError(t, err)
	assert.Equal(t, other.Importance+1, got.Importance)
	got, err = repo.GetByID(ctx, "1", protected.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictDecision, got.Verdict, "protected memories never change")
	got, err = repo.GetByID(ctx, "1", foreign.ID)
	require.NoError(t, err)
	assert.Equal(t, types.VerdictNone, got.Verdict)

	audit, _, err := repo.ListAudit(ctx, types.MemoryAuditFilter{TenantID: "1", KbID: "kb", Limit: 10})
	require.NoError(t, err)
	require.Len(t, audit, 2, "every applied change is audited")
	for _, e := range audit {
		assert.Equal(t, "dreamer", e.Actor)
	}
	assert.Equal(t, 1, calls)
}

func createPGServiceMemory(t *testing.T, db *gorm.DB, tenantID, kbID, content string, vec []float32) *types.AgentMemory {
	t.Helper()
	padded := make([]float32, types.MemoryEmbeddingDim)
	copy(padded, vec)
	now := time.Now()
	m := &types.AgentMemory{
		ID: uuid.NewString(), TenantID: tenantID, KbID: kbID, Content: content,
		MemoryType: "semantic", Importance: 1, Tier: 2, Verdict: types.VerdictNone,
		Embedding: pgvector.NewVector(padded), CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(m).Error)
	return m
}
