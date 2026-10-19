package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MemoryV2DreamUndoer reverts the changes one Dreamer pass applied to a
// knowledge base. Callers authorize (tenantID, kbID) as KB Admin first.
type MemoryV2DreamUndoer interface {
	UndoDreamRun(ctx context.Context, tenantID, kbID, runID string) (*types.DreamUndoResult, error)
}
