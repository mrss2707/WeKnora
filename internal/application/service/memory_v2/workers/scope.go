package workers

import (
	"context"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Scope is one (tenant, knowledge base) partition of the memory store.
// Background workers always operate inside a single scope so they never mix
// tenants or knowledge bases.
type Scope = types.MemoryScope

// ModelResolver resolves the models of a scope: the knowledge base's own
// embedding/summary models, falling back to the tenant's defaults.
type ModelResolver interface {
	Embedder(ctx context.Context, tenantID, kbID string) (embedding.Embedder, string, error)
	Chat(ctx context.Context, tenantID, kbID string) (chat.Chat, error)
}

// PurposeChatResolver is optionally implemented by a ModelResolver to honor a
// knowledge base's per-task model choice.
type PurposeChatResolver interface {
	ChatFor(ctx context.Context, tenantID, kbID string, purpose types.MemoryModelPurpose) (chat.Chat, error)
}

// chatForPurpose prefers the purpose-aware resolver.
func chatForPurpose(ctx context.Context, r ModelResolver, sc Scope, purpose types.MemoryModelPurpose) (chat.Chat, error) {
	if pr, ok := r.(PurposeChatResolver); ok {
		return pr.ChatFor(ctx, sc.TenantID, sc.KbID, purpose)
	}
	return r.Chat(ctx, sc.TenantID, sc.KbID)
}

// listScopes returns the live scopes, or nil when the repository cannot
// enumerate them.
func listScopes(ctx context.Context, repo interface{}) ([]Scope, error) {
	lister, ok := repo.(interfaces.MemoryV2ScopeLister)
	if !ok {
		return nil, nil
	}
	return lister.ListMemoryScopes(ctx)
}
