package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryKBScopeMismatch describes a memory stored under a tenant other than the
// owner tenant of its knowledge base. Such rows were written through a shared
// knowledge base by a member whose own tenant differs from the owner's.
type MemoryKBScopeMismatch struct {
	MemoryID       string
	KbID           string
	CurrentTenant  string
	OwnerTenant    string
	Content        string
	OldFingerprint *string
}

// MemoryV2KBScopeRepository is an optional capability of MemoryRepositoryV2
// (type-assert it, like MCPEndpointTokenRetriever) so the core interface stays
// untouched. It lets callers resolve a memory before its tenant is known and
// repairs rows that violate the "memory lives in the KB owner's tenant" rule.
type MemoryV2KBScopeRepository interface {
	// FindByID loads a live memory without a tenant filter. Callers MUST
	// authorize the result against its knowledge base before exposing it.
	FindByID(ctx context.Context, id string) (*types.AgentMemory, error)
	// ListKBScopeMismatches returns up to limit mismatched rows (0 = all).
	ListKBScopeMismatches(ctx context.Context, limit int) ([]*MemoryKBScopeMismatch, error)
	// MoveToKBOwnerTenant moves one row to its owner tenant, writing the audit
	// log in the same transaction. A non-nil newFingerprint that collides with
	// a live row in the destination is stored as NULL instead of failing.
	MoveToKBOwnerTenant(ctx context.Context, m *MemoryKBScopeMismatch, newFingerprint string) error
	// SyncRelationTenants re-tenants relations touching the given memories and
	// soft-deletes relations whose endpoints ended up in different tenants.
	SyncRelationTenants(ctx context.Context, memoryIDs []string) (retenanted, dropped int64, err error)
}
