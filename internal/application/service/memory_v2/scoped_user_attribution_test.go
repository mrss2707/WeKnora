package memory_v2

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When the request carries a human user (set by MCPEndpointUserAttribution from
// an X-API-Key owner), memory_save records that user for attribution.
func TestScopedMemorySaveRecordsHumanUser(t *testing.T) {
	repo := &scopedMemoryRepoFake{}
	svc, base := readyScopedService(repo)
	scope := interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}
	human := "550e8400-e29b-41d4-a716-446655440000"

	ctx := context.WithValue(context.Background(), types.UserIDContextKey, human)
	_, err := svc.Save(ctx, scope, "valid memory content", "session-1")
	require.NoError(t, err)
	assert.Equal(t, human, base.saved.UserID)
}

// Synthetic machine principals (the default MCP endpoint user, or the
// X-API-Key tenant synthetic user) must not be recorded as the creator.
func TestScopedMemorySaveIgnoresSyntheticUsers(t *testing.T) {
	repo := &scopedMemoryRepoFake{}
	svc, base := readyScopedService(repo)
	scope := interfaces.MemoryV2Scope{TenantID: "1", KnowledgeBaseID: "kb-1"}

	mcpCtx := context.WithValue(context.Background(), types.UserIDContextKey, "mcp-abc")
	_, err := svc.Save(mcpCtx, scope, "valid memory content", "")
	require.NoError(t, err)
	assert.Empty(t, base.saved.UserID)

	sysCtx := context.WithValue(context.Background(), types.UserIDContextKey, "system-1")
	_, err = svc.Save(sysCtx, scope, "valid memory content", "")
	require.NoError(t, err)
	assert.Empty(t, base.saved.UserID)
}
