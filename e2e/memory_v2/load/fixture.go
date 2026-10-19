// Package load builds and measures isolated, synthetic Memory V2 load profiles.
package load

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	repository "github.com/Tencent/WeKnora/internal/application/repository/memory_v2"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type Profile struct {
	TenantID, UserID, Email, KBID, KBName, EmbeddingModelID string
	Rows                                                    []Row
	Client                                                  *rest.Client      `json:"-"`
	Endpoint                                                *rest.MCPEndpoint `json:"-"`
}

type Row struct {
	ID, Content string
	Protected   bool
}

type Fixture struct {
	RunID, BaseURL string
	Profiles       []*Profile
	DB             *gorm.DB `json:"-"`
	embedder       embedding.Embedder
}

func NewFixture(ctx context.Context, world *rest.IsolatedWorld) (*Fixture, error) {
	if !world.Verified() {
		return nil, fmt.Errorf("a verified isolated fixture world is required")
	}
	container, port := "memv2-e2e-pg", "55441"
	modelURL := "http://localhost:11434/v1"
	if world.BaseURL == "http://localhost:18082" {
		container, port, modelURL = "memv2-release-gate-postgres-1", "55442", "http://host.docker.internal:11434/v1"
	}
	binding, err := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}`, container).Output()
	if err != nil || strings.TrimSpace(string(binding)) != port {
		return nil, fmt.Errorf("refusing an unverified E2E database binding")
	}
	db, err := gorm.Open(postgres.Open("host=localhost port="+port+" user=postgres password=e2e dbname=weknora_e2e sslmode=disable"), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(2)
	pool.SetConnMaxLifetime(2 * time.Minute)
	fixture := &Fixture{RunID: uuid.NewString(), BaseURL: world.BaseURL, DB: db}
	fixture.embedder, err = embedding.NewEmbedder(embedding.Config{Source: types.ModelSourceRemote, BaseURL: "http://localhost:11434/v1", APIKey: "e2e", ModelName: "bge-m3", Dimensions: 1024, Provider: "generic", MaxConcurrency: 1}, nil, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("load-%s-%d", fixture.RunID[:8], i)
		session, err := rest.Register(ctx, world.BaseURL, name, name+"@memv2-e2e.test", "Memv2-e2e!")
		if err != nil {
			return fixture, err
		}
		client := session.As(world.BaseURL)
		embedID, err := client.CreateModel(ctx, rest.ModelSpec{Name: "bge-m3", Type: "Embedding", BaseURL: modelURL, APIKey: "e2e", Dimension: 1024})
		if err != nil {
			return fixture, err
		}
		chatID, err := client.CreateModel(ctx, rest.ModelSpec{Name: "qwen2.5:3b", Type: "KnowledgeQA", BaseURL: modelURL, APIKey: "e2e"})
		if err != nil {
			return fixture, err
		}
		kbName := "memv2-load-" + fixture.RunID[:8] + "-" + fmt.Sprint(i)
		kb, err := client.CreateKnowledgeBase(ctx, kbName, embedID, chatID)
		if err != nil {
			return fixture, err
		}
		profile := &Profile{TenantID: fmt.Sprint(session.HomeTenant), UserID: session.UserID, Email: session.Email, KBID: kb.ID, KBName: kbName, EmbeddingModelID: embedID, Client: client}
		fixture.Profiles = append(fixture.Profiles, profile)
		var matching int64
		if err := db.WithContext(ctx).Table("knowledge_bases").Where("id = ? AND CAST(tenant_id AS TEXT) = ? AND name = ? AND deleted_at IS NULL", kb.ID, profile.TenantID, kbName).Count(&matching).Error; err != nil {
			return fixture, err
		}
		if matching != 1 {
			return fixture, fmt.Errorf("API/database fixture scope does not match")
		}
		profile.Endpoint, err = client.CreateMCPEndpoint(ctx, kbName, []string{kb.ID}, []string{"memory_status", "memory_detail", "memory_graph", "memory_recall", "memory_save"}, 6000)
		if err != nil {
			return fixture, err
		}
	}
	return fixture, nil
}

// SeedTo imports scale fixtures using real model vectors and the production
// atomic create+audit+queue transaction. It deliberately bypasses ingestion
// dedup/classification; this is cardinality setup, not an ingestion benchmark.
func (f *Fixture) SeedTo(ctx context.Context, total int) error {
	if total < 200 || total > 10000 || len(f.Profiles) != 3 {
		return fmt.Errorf("scale fixture bounds must be 200..10000 across three verified profiles")
	}
	targets := []int{total / 2, total / 4, total - total/2 - total/4}
	repo := repository.NewMemoryRepository(f.DB)
	for p, profile := range f.Profiles {
		for len(profile.Rows) < targets[p] {
			start := len(profile.Rows)
			count := min(32, targets[p]-start)
			texts := make([]string, count)
			for i := range texts {
				texts[i] = FixtureContent(p, start+i)
			}
			vectors, err := f.embedder.BatchEmbed(ctx, texts)
			if err != nil {
				return err
			}
			if len(vectors) != len(texts) {
				return fmt.Errorf("real embedder returned the wrong batch cardinality")
			}
			for i, vector := range vectors {
				if err := ValidateVector(vector); err != nil {
					return err
				}
				metadata, _ := json.Marshal(map[string]string{"embedding_model_id": profile.EmbeddingModelID, "fixture_run": f.RunID})
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(profile.TenantID+"\x00"+profile.KBID+"\x00"+texts[i])))
				memory := &types.AgentMemory{ID: uuid.NewString(), TenantID: profile.TenantID, KbID: profile.KBID, UserID: profile.UserID, Content: texts[i], MemoryType: "semantic", Verdict: types.VerdictNone, Importance: 3, Tier: 1, Tags: types.TagsArray{"scale", "synthetic"}, Embedding: pgvector.NewVector(vector), Metadata: metadata, Fingerprint: &hash}
				if start+i == 0 {
					memory.Verdict, memory.Tier = types.VerdictDecision, 0
				}
				if start+i == 1 {
					memory.Verdict, memory.Tier = types.VerdictFixed, 0
				}
				if err := repo.CreateAndAudit(ctx, memory, func(_, after *types.AgentMemory) *types.MemoryAuditEntry {
					snapshot, _ := json.Marshal(after)
					return &types.MemoryAuditEntry{TenantID: after.TenantID, KbID: after.KbID, MemoryID: after.ID, UserID: profile.UserID, Actor: "e2e_fixture", Action: "created", After: snapshot, Details: []byte(`{"fixture":true}`)}
				}); err != nil {
					return err
				}
				profile.Rows = append(profile.Rows, Row{ID: memory.ID, Content: memory.Content, Protected: memory.Verdict.IsProtected()})
			}
		}
	}
	return nil
}

func ValidateVector(vector []float32) error {
	if len(vector) != 1024 {
		return fmt.Errorf("expected real bge-m3 1024-dimensional vectors")
	}
	var norm float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("non-finite embedding")
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return fmt.Errorf("zero embeddings do not qualify as scale fixtures")
	}
	return nil
}

func (f *Fixture) Close() error {
	pool, err := f.DB.DB()
	if err != nil {
		return err
	}
	return pool.Close()
}
