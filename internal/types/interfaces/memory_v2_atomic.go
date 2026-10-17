package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2AuditBuilder snapshots the locked versions inside the mutation's
// transaction. A failed audit insert must roll the mutation back.
type MemoryV2AuditBuilder func(before, after *types.AgentMemory) *types.MemoryAuditEntry

type MemoryV2AtomicFieldWriter interface {
	UpdateFieldsAndAudit(ctx context.Context, tenantID, id string, fields map[string]interface{}, expected *time.Time, audit MemoryV2AuditBuilder) (*types.AgentMemory, error)
}
type MemoryV2AtomicCreator interface {
	CreateAndAudit(ctx context.Context, memory *types.AgentMemory, audit MemoryV2AuditBuilder) error
}

type MemoryV2AtomicDeleter interface {
	DeleteAndAudit(ctx context.Context, tenantID, id string, audit MemoryV2AuditBuilder) error
}
