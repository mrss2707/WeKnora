package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2HealthReporter preserves coverage without extending MemoryServiceV2.
// Implementations apply the same readiness and scope checks as AssessHealth.
type MemoryV2HealthReporter interface {
	AssessHealthReport(ctx context.Context, tenantID, kbID string) (*types.HealthReport, error)
}

// MemoryV2HealthEmbeddingReader avoids sampling a different KB's embeddings.
type MemoryV2HealthEmbeddingReader interface {
	GetHealthEmbeddingDimension(ctx context.Context, tenantID, kbID string) (int, error)
}
