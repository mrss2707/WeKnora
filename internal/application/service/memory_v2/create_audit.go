package memory_v2

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func (s *MemoryServiceV2Impl) createAuditedMemory(ctx context.Context, memory *types.AgentMemory) error {
	build := func(_, after *types.AgentMemory) *types.MemoryAuditEntry {
		return newAudit(ctx, types.MemoryAuditCreated, nil, after, nil)
	}
	if atomic, ok := s.repo.(interfaces.MemoryV2AtomicCreator); ok {
		return atomic.CreateAndAudit(ctx, memory, build)
	}
	if err := s.repo.Create(ctx, memory); err != nil {
		return err
	}
	s.recordAudit(ctx, build(nil, memory))
	return nil
}
