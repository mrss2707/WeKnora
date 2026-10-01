package types

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPEndpointBeforeSaveEncrypts(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))

	ep := &MCPEndpoint{ID: "e1", TokenEncrypted: "mcp_secret_token"}
	require.NoError(t, ep.BeforeSave(nil))
	assert.True(t, strings.HasPrefix(ep.TokenEncrypted, utils.EncPrefix),
		"expected enc:v1: ciphertext, got %q", ep.TokenEncrypted)
	assert.NotEqual(t, "mcp_secret_token", ep.TokenEncrypted)

	// The stored value round-trips through DecryptStoredSecret.
	plain, err := utils.DecryptStoredSecret(ep.TokenEncrypted)
	require.NoError(t, err)
	assert.Equal(t, "mcp_secret_token", plain)
}

func TestMCPEndpointBeforeSaveIdempotent(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))

	ep := &MCPEndpoint{ID: "e1", TokenEncrypted: "mcp_secret"}
	require.NoError(t, ep.BeforeSave(nil))
	first := ep.TokenEncrypted

	// Saving an already-encrypted value must not double-encrypt it.
	require.NoError(t, ep.BeforeSave(nil))
	assert.Equal(t, first, ep.TokenEncrypted)
}

func TestMCPEndpointBeforeSaveClearsWithoutKey(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "")

	ep := &MCPEndpoint{ID: "e1", TokenEncrypted: "mcp_secret"}
	require.NoError(t, ep.BeforeSave(nil))
	assert.Equal(t, "", ep.TokenEncrypted, "without SYSTEM_AES_KEY the plaintext token must never be persisted")
}

func TestMCPEndpointBeforeSaveEmptyNoop(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))

	ep := &MCPEndpoint{ID: "e1"}
	require.NoError(t, ep.BeforeSave(nil))
	assert.Equal(t, "", ep.TokenEncrypted)
}
