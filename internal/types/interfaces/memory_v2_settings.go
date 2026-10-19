package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2ScopeSettingsStore persists per-KB Memory V2 model choices without
// extending MemoryRepositoryV2.
type MemoryV2ScopeSettingsStore interface {
	// GetScopeSettings returns nil, nil when the scope has no stored settings.
	GetScopeSettings(ctx context.Context, tenantID, kbID string) (*types.MemoryV2ScopeSettings, error)
	UpsertScopeSettings(ctx context.Context, settings *types.MemoryV2ScopeSettings) error
}

// MemoryV2ModelSettingsService reads and changes per-KB model choices. Callers
// authorize the scope first; implementations validate model ownership/type.
type MemoryV2ModelSettingsService interface {
	GetModelSettings(ctx context.Context, tenantID, kbID string) (*types.MemoryV2ModelSettings, error)
	UpdateModelSettings(ctx context.Context, tenantID, kbID, userID string, update types.MemoryV2ModelSettingsUpdate) (*types.MemoryV2ModelSettings, error)
}
