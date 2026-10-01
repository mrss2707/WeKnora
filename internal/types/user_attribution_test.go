package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHumanUserID(t *testing.T) {
	// Real human user ids (opaque UUIDs / simple ids) are accepted.
	assert.True(t, IsHumanUserID("550e8400-e29b-41d4-a716-446655440000"))
	assert.True(t, IsHumanUserID("user-1"))
	assert.True(t, IsHumanUserID("  user-1  "))

	// Synthetic machine principals are rejected.
	assert.False(t, IsHumanUserID(""))
	assert.False(t, IsHumanUserID("   "))
	assert.False(t, IsHumanUserID("system-1"))            // X-API-Key tenant synthetic user
	assert.False(t, IsHumanUserID("system-42"))           // same, other tenant
	assert.False(t, IsHumanUserID("mcp-abc"))             // MCP endpoint synthetic user
	assert.False(t, IsHumanUserID("api_platform:123"))    // principal StorageID form
	assert.False(t, IsHumanUserID("api_external_user:7")) // principal StorageID form
}

func TestHumanUserIDFromContext(t *testing.T) {
	human := "550e8400-e29b-41d4-a716-446655440000"
	ctx := context.WithValue(context.Background(), UserIDContextKey, human)
	id, ok := HumanUserIDFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, human, id)
	assert.Equal(t, human, HumanUserIDOrEmpty(ctx))

	// Synthetic user in context is not a human.
	synCtx := context.WithValue(context.Background(), UserIDContextKey, "system-1")
	_, ok = HumanUserIDFromContext(synCtx)
	assert.False(t, ok)
	assert.Equal(t, "", HumanUserIDOrEmpty(synCtx))

	// MCP machine user in context is not a human.
	mcpCtx := context.WithValue(context.Background(), UserIDContextKey, "mcp-abc")
	_, ok = HumanUserIDFromContext(mcpCtx)
	assert.False(t, ok)
	assert.Equal(t, "", HumanUserIDOrEmpty(mcpCtx))

	// Absent user.
	_, ok = HumanUserIDFromContext(context.Background())
	assert.False(t, ok)
	assert.Equal(t, "", HumanUserIDOrEmpty(context.Background()))
}
