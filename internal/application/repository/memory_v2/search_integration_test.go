package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withParadeDBIndex adds the production BM25 index (migration 900074) when
// pg_search is installed, and reports whether it did.
func withParadeDBIndex(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pg_search`).Error; err != nil {
		return false
	}
	require.NoError(t, db.Exec(`CREATE INDEX idx_agent_memories_fts ON agent_memories
		USING bm25 (id, tenant_id, kb_id, content, memory_type, importance, tier, tags) WITH (key_field='id')`).Error)
	return true
}

func ids(rs []*types.MemorySearchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Memory.Content
	}
	return out
}

func seedSearchCorpus(t *testing.T, db *gorm.DB) {
	t.Helper()
	createPostgresMemory(t, db, "1", "kb", "Postgres connection pool exhausted under load", types.VerdictNone, []float32{1, 0, 0})
	createPostgresMemory(t, db, "1", "kb", "Decided to use pgbouncer for the postgres pool", types.VerdictDecision, []float32{0.9, 0.1, 0})
	createPostgresMemory(t, db, "1", "kb", "Kết nối postgres bị đầy pool khi tải cao", types.VerdictNone, []float32{0.8, 0.2, 0})
	createPostgresMemory(t, db, "1", "kb", "Redis eviction policy is allkeys-lru", types.VerdictNone, []float32{0, 0, 1})
	createPostgresMemory(t, db, "1", "kb", "Refuted: postgres pool is unlimited", types.VerdictRefuted, []float32{1, 0, 0})
	createPostgresMemory(t, db, "1", "other-kb", "postgres pool in another knowledge base", types.VerdictNone, []float32{1, 0, 0})
	createPostgresMemory(t, db, "2", "kb", "postgres pool of another tenant", types.VerdictNone, []float32{1, 0, 0})
}

func TestPostgresMemoryRepository_KeywordSearchMatchesAnyTerm(t *testing.T) {
	for _, mode := range []string{"paradedb", "tsvector"} {
		t.Run(mode, func(t *testing.T) {
			db := memoryV2PostgresDB(t)
			repo := NewMemoryRepository(db)
			if mode == "paradedb" {
				if !withParadeDBIndex(t, db) {
					t.Skip("pg_search unavailable")
				}
			} else {
				repo.paradeDBOnce.Do(func() { repo.paradeDBAvail = false })
			}
			seedSearchCorpus(t, db)
			ctx := context.Background()

			// A natural-language query: no memory contains the whole phrase.
			got, err := repo.BM25Search(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb", Query: "why is the postgres pool exhausted?", Limit: 10})
			require.NoError(t, err)
			require.NotEmpty(t, got, "any-term match, not exact phrase")
			if mode == "paradedb" {
				assert.Equal(t, "Postgres connection pool exhausted under load", got[0].Memory.Content, "best BM25 match first")
			} else {
				// ts_rank (no stemming, no IDF) orders differently; the
				// fallback only guarantees the match is found.
				assert.Contains(t, ids(got), "Postgres connection pool exhausted under load")
			}
			for _, r := range got {
				assert.Equal(t, "1", r.Memory.TenantID)
				assert.Equal(t, "kb", r.Memory.KbID)
				assert.NotEqual(t, types.VerdictRefuted, r.Memory.Verdict, "refuted excluded by default")
				assert.Positive(t, r.Score)
			}

			vi, err := repo.BM25Search(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb", Query: "tải cao", Limit: 10})
			require.NoError(t, err)
			require.Len(t, vi, 1)
			assert.Equal(t, "Kết nối postgres bị đầy pool khi tải cao", vi[0].Memory.Content, "Vietnamese terms match")

			typed, err := repo.BM25Search(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb", Query: "postgres", MemoryType: "decision", Limit: 10})
			require.NoError(t, err)
			assert.Empty(t, typed, "filters apply to keyword search (seeded memories are semantic)")

			odd, err := repo.BM25Search(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb", Query: `max_open_conns: (50) "x" AND OR NOT *`, Limit: 10})
			require.NoError(t, err, "arbitrary user text never breaks the query")
			_ = odd

			list, total, err := repo.Search(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb", Query: "redis pgbouncer", Limit: 10})
			require.NoError(t, err)
			assert.EqualValues(t, 2, total)
			assert.ElementsMatch(t, []string{"Decided to use pgbouncer for the postgres pool", "Redis eviction policy is allkeys-lru"}, ids(list))
		})
	}
}

func TestPostgresMemoryRepository_CosineSearchAppliesAllFilters(t *testing.T) {
	db := memoryV2PostgresDB(t)
	repo := NewMemoryRepository(db)
	seedSearchCorpus(t, db)
	require.NoError(t, db.Exec(`UPDATE agent_memories SET session_id = 's1', user_id = 'alice' WHERE content LIKE 'Decided%'`).Error)
	require.NoError(t, db.Exec(`UPDATE agent_memories SET tier = 1 WHERE content LIKE 'Kết nối%'`).Error)
	ctx := context.Background()
	q := []float32{1, 0, 0}

	all, err := repo.CosineSearch(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb"}, q, 10)
	require.NoError(t, err)
	assert.Len(t, all, 4, "same tenant+kb, refuted excluded")
	assert.Equal(t, "Postgres connection pool exhausted under load", all[0].Memory.Content)
	for i := 1; i < len(all); i++ {
		assert.GreaterOrEqual(t, all[i-1].Score, all[i].Score, "ordered by similarity")
	}

	tier := 1
	cases := map[string]*types.MemoryFilter{
		"session": {TenantID: "1", KbID: "kb", SessionID: "s1"},
		"author":  {TenantID: "1", KbID: "kb", AuthorUserID: "alice"},
		"tier":    {TenantID: "1", KbID: "kb", Tier: &tier},
		"verdict": {TenantID: "1", KbID: "kb", Verdicts: []types.MemoryVerdict{types.VerdictDecision}},
	}
	want := map[string]string{
		"session": "Decided to use pgbouncer for the postgres pool",
		"author":  "Decided to use pgbouncer for the postgres pool",
		"tier":    "Kết nối postgres bị đầy pool khi tải cao",
		"verdict": "Decided to use pgbouncer for the postgres pool",
	}
	for name, f := range cases {
		got, err := repo.CosineSearch(ctx, f, q, 10)
		require.NoError(t, err, name)
		assert.Equal(t, []string{want[name]}, ids(got), name)
	}
	got, err := repo.CosineSearch(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb", MemoryType: "decision"}, q, 10)
	require.NoError(t, err)
	assert.Empty(t, got, "memory_type filter applies to vector search")
}
