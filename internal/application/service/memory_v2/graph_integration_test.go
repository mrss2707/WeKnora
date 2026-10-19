package memory_v2

import (
	"context"
	"os"
	"regexp"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// pgServiceDB applies the real Memory V2 module migrations into an isolated
// schema (skips without WEKNORA_TEST_POSTGRES_DSN).
func pgServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("WEKNORA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEKNORA_TEST_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	schema := "memv2_svc_" + regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(uuid.NewString(), "")
	for _, ext := range []string{"vector", "pg_search", `"uuid-ossp"`} {
		_ = db.Exec(`CREATE EXTENSION IF NOT EXISTS ` + ext).Error
	}
	require.NoError(t, db.Exec(`CREATE SCHEMA `+schema).Error)
	t.Cleanup(func() { defer sqlDB.Close(); db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
	require.NoError(t, db.Exec(`SET search_path TO `+schema+`, public`).Error)
	for _, f := range []string{"900074_memory_v2", "900075_memory_v2_verdict", "900076_memory_v2_hnsw", "900079_memory_v2_audit", "900080_memory_v2_access_trigger", "900081_memory_v2_dreamer_scope", "900082_memory_v2_dreamer_budget", "900083_memory_v2_graph_jobs", "900084_memory_v2_scope_settings"} {
		sql, err := os.ReadFile("../../../../migrations/modules/memory_v2/postgres/" + f + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(sql)).Error, f)
	}
	return db
}

func TestGraph_EndToEndOnPostgres(t *testing.T) {
	db := pgServiceDB(t)
	repo := repository.NewMemoryRepository(db)
	// Vectors chosen so a~b~c are a chain of close neighbours and d is far.
	vecs := map[string][]float32{
		"a": {1, 0, 0}, "b": {0.9, 0.43, 0}, "c": {0.6, 0.8, 0}, "d": {0, 0, 1}, "other": {1, 0, 0},
	}
	emb := &mockEmbedder{embedFunc: func(_ context.Context, text string) ([]float32, error) {
		return vecs[text[len(text)-1:]], nil
	}}
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)
	svc.embedder = emb
	ctx := context.Background()

	ids := map[string]string{}
	for _, k := range []string{"a", "b", "c", "d"} {
		res, err := svc.SaveMemory(ctx, &types.AgentMemory{TenantID: "1", KbID: "kb", Content: "memory content " + k})
		require.NoError(t, err)
		require.True(t, res.Created)
		ids[k] = res.Memory.ID
	}
	_, err := svc.SaveMemory(ctx, &types.AgentMemory{TenantID: "1", KbID: "kb-2", Content: "memory content other"})
	require.NoError(t, err)

	var relCount int64
	require.NoError(t, db.Table("memory_relations").Where("deleted_at IS NULL").Count(&relCount).Error)
	assert.Positive(t, relCount, "saving related memories creates edges (the graph was empty before)")

	scoped := NewScopedMemoryV2Service(svc, repo).(interfaces.MemoryV2GraphExplorer)
	scope := interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb"}

	one, err := scoped.GraphDepth(ctx, scope, ids["a"], 1, 100)
	require.NoError(t, err)
	two, err := scoped.GraphDepth(ctx, scope, ids["a"], 2, 100)
	require.NoError(t, err)
	got := func(g *interfaces.MemoryV2Graph) map[string]bool {
		out := map[string]bool{}
		for _, n := range g.Nodes {
			assert.Equal(t, "kb", n.KbID, "never leaves the knowledge base")
			out[n.ID] = true
		}
		return out
	}
	assert.True(t, got(one)[ids["b"]], "direct neighbour")
	assert.True(t, got(two)[ids["c"]], "two hops away")
	assert.False(t, got(two)[ids["d"]], "unrelated memory not linked")

	var hub float64
	require.NoError(t, db.Raw(`SELECT hub_score FROM agent_memories WHERE id = ?`, ids["b"]).Scan(&hub).Error)
	assert.Positive(t, hub, "hub scores are refreshed when edges are created")

	ov, err := scoped.Overview(ctx, scope, 10)
	require.NoError(t, err)
	assert.Len(t, ov.Nodes, 4)
	assert.NotEmpty(t, ov.Relations)
	assert.Equal(t, ids["b"], ov.Nodes[0].ID, "the most connected memory leads the overview")

	// Relink is idempotent, rebuilds wiped edges and requeues exhausted jobs.
	require.NoError(t, db.Exec(`UPDATE extraction_queue SET status = 'failed', attempts = max_attempts WHERE memory_uuid = ? AND job_type = 'graph_link'`, ids["a"]).Error)
	before := relCount
	require.NoError(t, db.Exec(`UPDATE memory_relations SET deleted_at = now()`).Error)
	res, err := svc.RelinkKnowledgeBase(ctx, "1", "kb")
	require.NoError(t, err)
	assert.Equal(t, 4, res.Memories)
	var job struct {
		Status   string
		Attempts int
	}
	require.NoError(t, db.Raw(`SELECT status, attempts FROM extraction_queue WHERE memory_uuid = ? AND job_type = 'graph_link'`, ids["a"]).Scan(&job).Error)
	assert.Equal(t, "pending", job.Status)
	assert.Zero(t, job.Attempts)
	require.NoError(t, db.Table("memory_relations").Where("deleted_at IS NULL").Count(&relCount).Error)
	assert.Equal(t, before, relCount, "relink restores the same edges")
	res, err = svc.RelinkKnowledgeBase(ctx, "1", "kb")
	require.NoError(t, err)
	require.NoError(t, db.Table("memory_relations").Where("deleted_at IS NULL").Count(&relCount).Error)
	assert.Equal(t, before, relCount, "a second relink adds no duplicates")

	// Deleting a memory removes it and its edges from the graph.
	require.NoError(t, svc.DeleteMemory(ctx, "1", ids["b"]))
	after, err := scoped.GraphDepth(ctx, scope, ids["a"], 3, 100)
	require.NoError(t, err)
	assert.False(t, got(after)[ids["b"]])
}
