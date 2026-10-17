package memory_v2

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/pgvector/pgvector-go"
)

// backfillScopeEmbeddings repairs provenance by actually embedding the content,
// never by relabelling an unknown vector as the KB's current model. It runs only
// in the explicit Admin relink path; ordinary recalls never mutate embeddings.
func (s *MemoryServiceV2Impl) backfillScopeEmbeddings(ctx context.Context, tenantID, kbID string, reader interfaces.MemoryV2EmbeddingReader) (int, error) {
	embedder, modelID, err := s.embedderFor(ctx, tenantID, kbID)
	if err != nil {
		return 0, &types.MemoryV2ModelUnavailable{Cause: err}
	}
	if modelID == "" {
		return 0, nil
	} // fixed-model overrides used by embedders/tests
	_, ok := s.repo.(interfaces.MemoryV2FieldUpdater)
	if !ok {
		return 0, fmt.Errorf("memory_v2: repository cannot backfill embeddings")
	}
	count := 0
	for offset := 0; ; offset += relinkPageSize {
		page, err := reader.ListWithEmbeddings(ctx, tenantID, kbID, relinkPageSize, offset)
		if err != nil {
			return count, err
		}
		for _, m := range page {
			if err := ctx.Err(); err != nil {
				return count, err
			}
			if metadataString(m.Metadata, metadataEmbeddingModel) == modelID {
				continue
			}
			vector, err := embedder.Embed(ctx, m.Content)
			if err != nil {
				return count, &types.MemoryV2ModelUnavailable{Cause: err}
			}
			fields := map[string]interface{}{
				"embedding": pgvector.NewVector(vector),
				"metadata":  setMetadataString(m.Metadata, metadataEmbeddingModel, modelID),
			}
			if _, err := s.writeAuditedFields(ctx, m, fields, &m.UpdatedAt, map[string]interface{}{"fields": []string{"embedding_model_id"}, "reason": "admin_relink_backfill"}); err != nil {
				return count, err
			}
			count++
		}
		if len(page) < relinkPageSize {
			return count, nil
		}
	}
}
