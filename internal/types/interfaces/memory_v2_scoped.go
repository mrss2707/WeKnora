package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2Scope is a trusted tenant and knowledge-base boundary established by
// the caller before Memory V2 data is accessed. External inputs must never be
// copied into this scope without authorization.
type MemoryV2Scope struct {
	TenantID        string
	KnowledgeBaseID string
}

// MemoryV2Graph is a bounded graph centered on one memory. Nodes and relations
// are guaranteed to belong to the same trusted scope.
type MemoryV2Graph struct {
	Focal     *types.AgentMemory
	Nodes     []*types.AgentMemory
	Relations []*types.MemoryRelation
	Truncated bool
}

// ScopedMemoryV2Service exposes the Memory V2 operations that are safe for
// scoped machine principals such as built-in MCP endpoints.
type ScopedMemoryV2Service interface {
	Readiness() types.MemoryV2Readiness
	Recall(ctx context.Context, scope MemoryV2Scope, query string, limit int) ([]*types.MemorySearchResult, error)
	Save(ctx context.Context, scope MemoryV2Scope, content, sessionID string) (*types.SaveMemoryResult, error)
	Detail(ctx context.Context, scope MemoryV2Scope, memoryID string) (*types.AgentMemory, error)
	Graph(ctx context.Context, scope MemoryV2Scope, memoryID string, limit int) (*MemoryV2Graph, error)
	Status(ctx context.Context, tenantID string) types.MemoryStatusResponse
}
