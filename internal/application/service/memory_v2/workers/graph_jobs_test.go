package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type graphJobFake struct {
	interfaces.MemoryRepositoryV2
	memory      *types.AgentMemory
	getErr      error
	relationErr error
	claimed     bool
	finishes    []bool
	relations   []*types.MemoryRelation
	cancel      context.CancelFunc
}

func (f *graphJobFake) EnqueueGraphLink(context.Context, *types.AgentMemory) error { return nil }
func (f *graphJobFake) ClaimGraphLink(_ context.Context, worker string, _ time.Duration) (*types.MemoryGraphJob, error) {
	if f.claimed {
		return nil, nil
	}
	f.claimed = true
	return &types.MemoryGraphJob{ID: "job", TenantID: "1", KbID: "kb", MemoryID: "source", Generation: 1, LockedBy: worker}, nil
}
func (f *graphJobFake) FinishGraphLink(_ context.Context, _ *types.MemoryGraphJob, success bool) error {
	f.finishes = append(f.finishes, success)
	f.cancel()
	return nil
}
func (f *graphJobFake) GetByID(context.Context, string, string) (*types.AgentMemory, error) {
	return f.memory, f.getErr
}
func (f *graphJobFake) CosineSearch(_ context.Context, filter *types.MemoryFilter, _ []float32, _ int) ([]*types.MemorySearchResult, error) {
	return []*types.MemorySearchResult{{Memory: &types.AgentMemory{ID: "neighbour", TenantID: filter.TenantID, KbID: filter.KbID}, Score: 0.9}}, nil
}
func (f *graphJobFake) CreateRelation(_ context.Context, relation *types.MemoryRelation) error {
	if f.relationErr != nil {
		return f.relationErr
	}
	f.relations = append(f.relations, relation)
	return nil
}

func TestGraphJobs_RetryErrorsAndRespectScopeAndProvenance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*graphJobFake)
		success   bool
		relations int
	}{
		{name: "stored vectors", success: true, relations: 1},
		{name: "relation failure", mutate: func(f *graphJobFake) { f.relationErr = errors.New("database unavailable") }},
		{name: "deleted while queued", mutate: func(f *graphJobFake) { f.memory = nil; f.getErr = gorm.ErrRecordNotFound }, success: true},
		{name: "scope changed", mutate: func(f *graphJobFake) { f.memory.KbID = "another-kb" }},
		{name: "unknown provenance", mutate: func(f *graphJobFake) { f.memory.Metadata = nil }},
		{name: "no vector", mutate: func(f *graphJobFake) { f.memory.Embedding = pgvector.NewVector(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &graphJobFake{cancel: cancel, memory: &types.AgentMemory{ID: "source", TenantID: "1", KbID: "kb", Embedding: pgvector.NewVector([]float32{1, 0}), Metadata: []byte(`{"embedding_model_id":"model"}`)}}
			if tc.mutate != nil {
				tc.mutate(f)
			}
			worker := NewGraphJobs(f, NewAutoLinker(f, nil))
			require.NotNil(t, worker)
			worker.Run(c)
			require.Equal(t, []bool{tc.success}, f.finishes)
			require.Len(t, f.relations, tc.relations)
		})
	}
}
