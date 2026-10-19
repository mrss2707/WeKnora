//go:build memory_eval

// Recall evaluation against a real embedding model and real ParadeDB.
//
//	WEKNORA_TEST_POSTGRES_DSN=postgres://...  (ParadeDB with pgvector + pg_search)
//	MEMV2_EVAL_EMBED_URL=http://localhost:11434/v1  MEMV2_EVAL_EMBED_MODEL=bge-m3
//	MEMV2_EVAL_EMBED_DIM=1024  SSRF_WHITELIST=localhost,127.0.0.1
//	go test -tags memory_eval -run TestRecallEval -v ./internal/application/service/memory_v2/
//
// Memories are ingested through SaveMemory and queried through SearchMemories
// (the production pipeline); only the model endpoint is configurable.
package memory_v2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type evalSet struct {
	Memories []struct {
		Key     string `json:"key"`
		Content string `json:"content"`
	} `json:"memories"`
	Queries []struct {
		Q    string   `json:"q"`
		Want []string `json:"want"`
		Kind string   `json:"kind"`
	} `json:"queries"`
	Negatives []string `json:"negatives"`
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// evalDB creates an isolated schema with the production Memory V2 tables and
// indexes (pgvector HNSW + ParadeDB BM25).
func evalDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("WEKNORA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEKNORA_TEST_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	schema := "memv2_eval_" + regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(uuid.NewString(), "")
	require.NoError(t, db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error)
	require.NoError(t, db.Exec(`CREATE EXTENSION IF NOT EXISTS pg_search`).Error)
	require.NoError(t, db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error)
	require.NoError(t, db.Exec(`CREATE SCHEMA `+schema).Error)
	t.Cleanup(func() { db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
	require.NoError(t, db.Exec(`SET search_path TO `+schema+`, public`).Error)
	for _, f := range []string{"900074_memory_v2", "900075_memory_v2_verdict", "900076_memory_v2_hnsw", "900079_memory_v2_audit", "900080_memory_v2_access_trigger", "900081_memory_v2_dreamer_scope", "900082_memory_v2_dreamer_budget", "900083_memory_v2_graph_jobs"} {
		sql, err := os.ReadFile("../../../../migrations/modules/memory_v2/postgres/" + f + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(sql)).Error, f)
	}
	// One connection keeps the search_path for the whole run.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

func TestRecallEval(t *testing.T) {
	raw, err := os.ReadFile("eval/testdata/recall_eval.json")
	require.NoError(t, err)
	var set evalSet
	require.NoError(t, json.Unmarshal(raw, &set))

	dim, _ := strconv.Atoi(envOr("MEMV2_EVAL_EMBED_DIM", "1024"))
	emb, err := embedding.NewEmbedder(embedding.Config{
		Source: types.ModelSourceRemote, Provider: "generic",
		BaseURL:   envOr("MEMV2_EVAL_EMBED_URL", "http://localhost:11434/v1"),
		ModelName: envOr("MEMV2_EVAL_EMBED_MODEL", "bge-m3"),
		APIKey:    envOr("MEMV2_EVAL_EMBED_KEY", "eval"), Dimensions: dim, ModelID: "eval-embed",
	}, nil, nil)
	require.NoError(t, err)

	db := evalDB(t)
	repo := repository.NewMemoryRepository(db)
	cfg := types.DefaultMemoryV2Config()
	cfg.LintOnWrite.Enabled = false
	svc := NewMemoryServiceV2(repo, nil, cfg, nil)
	svc.embedder = emb
	ctx := context.Background()

	idToKey := map[string]string{}
	for _, m := range set.Memories {
		res, err := svc.SaveMemory(ctx, &types.AgentMemory{TenantID: "1", KbID: "kb-eval", Content: m.Content})
		require.NoError(t, err, m.Key)
		require.True(t, res.Created, "fixture %s merged into %s; fixtures must be distinct", m.Key, idToKey[res.Memory.ID])
		idToKey[res.Memory.ID] = m.Key
	}

	type sweep struct{ minCos, floor float64 }
	sweeps := []sweep{{0.0, 0.0}, {0.3, 0.2}, {0.4, 0.2}, {0.45, 0.25}, {0.5, 0.3}, {0.55, 0.3}}
	if v := os.Getenv("MEMV2_EVAL_ONLY_DEFAULTS"); v != "" {
		sweeps = []sweep{{cfg.MinCosine, cfg.RelativeScoreFloor}}
	}

	report := &strings.Builder{}
	fmt.Fprintf(report, "\nRecall eval: %d memories, %d queries, %d negatives, model=%s\n",
		len(set.Memories), len(set.Queries), len(set.Negatives), envOr("MEMV2_EVAL_EMBED_MODEL", "bge-m3"))
	fmt.Fprintf(report, "%-9s %-6s | %-6s %-6s %-6s %-6s | %-9s %-8s\n", "min_cos", "floor", "R@1", "R@3", "R@5", "MRR", "avg_hits", "neg_hits")
	for _, sw := range sweeps {
		svc.config.MinCosine, svc.config.RelativeScoreFloor = sw.minCos, sw.floor
		var r1, r3, r5, mrr, hits float64
		byKind := map[string][2]float64{}
		var misses []string
		for _, q := range set.Queries {
			res, err := svc.SearchMemories(ctx, q.Q, &types.MemoryFilter{TenantID: "1", KbID: "kb-eval", Limit: 5})
			require.NoError(t, err)
			hits += float64(len(res))
			want := map[string]bool{}
			for _, w := range q.Want {
				want[w] = true
			}
			rank := 0
			for i, r := range res {
				if want[idToKey[r.Memory.ID]] {
					rank = i + 1
					break
				}
			}
			k := byKind[q.Kind]
			k[1]++
			if rank > 0 {
				mrr += 1 / float64(rank)
				r5++
				k[0]++
				if rank <= 3 {
					r3++
				}
				if rank == 1 {
					r1++
				}
			} else {
				got := []string{}
				for _, r := range res {
					got = append(got, idToKey[r.Memory.ID])
				}
				misses = append(misses, fmt.Sprintf("    miss [%s] %q want %v got %v", q.Kind, q.Q, q.Want, got))
			}
			byKind[q.Kind] = k
		}
		var negHits float64
		for _, nq := range set.Negatives {
			res, err := svc.SearchMemories(ctx, nq, &types.MemoryFilter{TenantID: "1", KbID: "kb-eval", Limit: 5})
			require.NoError(t, err)
			negHits += float64(len(res))
		}
		n := float64(len(set.Queries))
		fmt.Fprintf(report, "%-9.2f %-6.2f | %-6.3f %-6.3f %-6.3f %-6.3f | %-9.2f %-8.2f\n",
			sw.minCos, sw.floor, r1/n, r3/n, r5/n, mrr/n, hits/n, negHits/float64(len(set.Negatives)))
		kinds := make([]string, 0, len(byKind))
		for k := range byKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		parts := []string{}
		for _, k := range kinds {
			parts = append(parts, fmt.Sprintf("%s=%.0f/%.0f", k, byKind[k][0], byKind[k][1]))
		}
		fmt.Fprintf(report, "    by kind: %s\n", strings.Join(parts, " "))
		if sw.minCos == cfg.MinCosine && sw.floor == cfg.RelativeScoreFloor {
			for _, m := range misses {
				fmt.Fprintln(report, m)
			}
			if gate := os.Getenv("MEMV2_EVAL_MIN_RECALL5"); gate != "" {
				min, _ := strconv.ParseFloat(gate, 64)
				require.GreaterOrEqual(t, r5/n, min, "recall@5 below the release gate")
			}
		}
	}
	// Cosine distribution: best relevant hit per query vs best hit of the
	// negative queries, to place MinCosine between them.
	var relevant, negative []float64
	for _, q := range set.Queries {
		vec, err := emb.Embed(ctx, q.Q)
		require.NoError(t, err)
		res, err := repo.CosineSearch(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb-eval"}, vec, 40)
		require.NoError(t, err)
		want := map[string]bool{}
		for _, w := range q.Want {
			want[w] = true
		}
		for _, r := range res {
			if want[idToKey[r.Memory.ID]] {
				relevant = append(relevant, r.Score)
				break
			}
		}
	}
	for _, nq := range set.Negatives {
		vec, err := emb.Embed(ctx, nq)
		require.NoError(t, err)
		res, err := repo.CosineSearch(ctx, &types.MemoryFilter{TenantID: "1", KbID: "kb-eval"}, vec, 1)
		require.NoError(t, err)
		if len(res) > 0 {
			negative = append(negative, res[0].Score)
		}
	}
	sort.Float64s(relevant)
	sort.Float64s(negative)
	pct := func(xs []float64, p float64) float64 { return xs[int(p*float64(len(xs)-1))] }
	fmt.Fprintf(report, "cosine of best relevant hit: min=%.3f p10=%.3f p50=%.3f; best hit of negative queries: %v\n",
		relevant[0], pct(relevant, 0.1), pct(relevant, 0.5), negative)
	t.Log(report.String())
}
