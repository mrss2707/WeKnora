package memory_v2

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// relinkPageSize is how many memories a relink pass loads per page.
const relinkPageSize = 200

var _ interfaces.MemoryV2Relinker = (*MemoryServiceV2Impl)(nil)

// RelinkKnowledgeBase links every memory of a knowledge base to its nearest
// neighbours (backfill for memories stored before auto-linking worked, or
// after changing the threshold), then recomputes the tenant's hub scores.
// Existing edges are kept; duplicates are ignored by the unique index.
func (s *MemoryServiceV2Impl) RelinkKnowledgeBase(ctx context.Context, tenantID, kbID string) (*types.MemoryRelinkResult, error) {
	if reason := s.notReadyReason(); reason != "" {
		return nil, fmt.Errorf("memory V2 not ready: %s", reason)
	}
	if tenantID == "" || kbID == "" {
		return nil, &ErrMemoryValidation{Message: "tenant_id and kb_id are required"}
	}
	s.initWorkers()
	store, ok := s.repo.(interfaces.MemoryV2EmbeddingReader)
	if !ok {
		return nil, fmt.Errorf("memory_v2: relink is not supported by this repository")
	}
	res := &types.MemoryRelinkResult{}
	// Repair all vectors before the graph pass so every candidate has verified
	// provenance, including neighbours on later pages.
	var err error
	res.Reembedded, err = s.backfillScopeEmbeddings(ctx, tenantID, kbID, store)
	if err != nil {
		return res, err
	}
	for offset := 0; ; offset += relinkPageSize {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		page, err := store.ListWithEmbeddings(ctx, tenantID, kbID, relinkPageSize, offset)
		if err != nil {
			return res, err
		}
		for _, m := range page {
			res.Relations += s.autoLinker.LinkMemory(ctx, m)
			res.Memories++
		}
		if len(page) < relinkPageSize {
			break
		}
	}
	if err := s.repo.ComputeHubScores(ctx, tenantID); err != nil {
		return res, err
	}
	return res, nil
}
