//go:build memory_eval

// Dreamer smoke test against a real chat model and real ParadeDB: whatever
// the model proposes, the safety invariants must hold.
//
//	WEKNORA_TEST_POSTGRES_DSN=postgres://...  SSRF_WHITELIST=localhost,127.0.0.1
//	MEMV2_EVAL_CHAT_URL=http://localhost:11434/v1  MEMV2_EVAL_CHAT_MODEL=qwen2.5:3b
//	go test -tags memory_eval -run TestDreamerEval -v ./internal/application/service/memory_v2/
package memory_v2

import (
	"context"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/application/service/memory_v2/workers"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDreamerEval(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	ctx := context.Background()

	model, err := chat.NewChat(&chat.ChatConfig{
		Source: types.ModelSourceRemote, Provider: "generic",
		BaseURL:   envOr("MEMV2_EVAL_CHAT_URL", "http://localhost:11434/v1"),
		ModelName: envOr("MEMV2_EVAL_CHAT_MODEL", "qwen2.5:3b"),
		APIKey:    envOr("MEMV2_EVAL_CHAT_KEY", "eval"), ModelID: "eval-chat",
	}, nil)
	require.NoError(t, err)

	contents := []string{
		"Deploying on Friday afternoons broke the release pipeline twice because nobody was on call.",
		"The staging cluster runs on three nodes in ap-southeast-1.",
		"We decided to use PostgreSQL with pgvector for all memory storage.",
		"Fixed: the login loop was caused by a missing SameSite attribute on the session cookie.",
		"Alice prefers code reviews before 11am.",
		"maybe look into caching later",
		"Postgres là cơ sở dữ liệu chính của hệ thống, đã được thống nhất từ quý 1.",
		"The API rate limit is 120 writes per minute per user.",
		"Redis is used only as the asynq queue backend, never as a cache.",
		"TODO: migrate the docreader service to the new gRPC schema (half done).",
		"The old Elasticsearch cluster was decommissioned in March; Postgres replaced it.",
		"Elasticsearch is our primary search engine.",
	}
	ids := make([]string, len(contents))
	for i, c := range contents {
		// Distinct facts get orthogonal vectors, so no server-side merge check
		// can pass whatever the model proposes.
		vec := make([]float32, len(contents))
		vec[i] = 1
		ids[i] = createPGServiceMemory(t, db, "1", "kb", c, vec).ID
	}
	protected := map[string]types.MemoryVerdict{ids[2]: types.VerdictDecision, ids[3]: types.VerdictFixed}
	for id, v := range protected {
		require.NoError(t, repo.UpdateFields(ctx, "1", id, map[string]interface{}{"verdict": v}))
	}
	before := map[string]*types.AgentMemory{}
	for _, id := range ids {
		m, err := repo.GetByID(ctx, "1", id)
		require.NoError(t, err)
		before[id] = m
	}

	cfg := types.DefaultMemoryV2Config()
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)
	dreamer := workers.NewDreamerWorker(repo, model, cfg.Dreamer, nil).WithApplier(svc)

	// Past the merge probation (MergeLiveAfterRuns), so merges are live too.
	passes := cfg.Dreamer.MergeLiveAfterRuns + 2
	for pass := 1; pass <= passes; pass++ {
		res, err := dreamer.RunScopePass(ctx, "1", "kb")
		require.NoError(t, err)
		t.Logf("pass %d: proposed=%d applied=%d tokens~%d", pass, res.ActionsProposed, res.ActionsApplied, res.TokenUsed)
		for _, a := range res.Actions {
			t.Logf("  %s %s verdict=%q delta=%d conf=%.2f reason=%q", a.Type, a.TargetID, a.NewVerdict, a.Delta, a.Confidence, a.Reason)
		}
	}

	live := 0
	for _, id := range ids {
		m, err := repo.GetByID(ctx, "1", id)
		require.NoError(t, err)
		if m == nil {
			continue // merged by a live merge after probation
		}
		live++
		b := before[id]
		if v, ok := protected[id]; ok {
			assert.Equal(t, v, m.Verdict, "protected verdict changed")
			assert.Equal(t, b.Importance, m.Importance, "protected importance changed")
			assert.Equal(t, b.Content, m.Content, "protected content changed")
			continue
		}
		assert.False(t, m.Verdict.IsProtected(), "dreamer must never assign a protected verdict")
		assert.LessOrEqual(t, abs(m.Importance-b.Importance), passes, "importance moves by at most 1 per pass")
	}
	assert.Equal(t, len(ids), live, "unrelated memories are never merged")

	audit, _, err := repo.ListAudit(ctx, types.MemoryAuditFilter{TenantID: "1", KbID: "kb", Limit: 100})
	require.NoError(t, err)
	for _, e := range audit {
		assert.Equal(t, "dreamer", e.Actor)
		_, isProtected := protected[e.MemoryID]
		assert.False(t, isProtected, "audit shows a change to a protected memory")
	}
	t.Logf("audited changes: %d", len(audit))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
