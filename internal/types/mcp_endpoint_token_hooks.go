package types

import (
	"fmt"

	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
)

// BeforeSave encrypts MCPEndpoint.TokenEncrypted at rest using AES-256-GCM when
// SYSTEM_AES_KEY is configured (and clears the field when it is not). It mutates the field in place (not
// tx.Statement.SetColumn) so the value persists through both Create and Save
// and the hook can be unit-tested with BeforeSave(nil).
//
// Security note: unlike the hash-only TokenHash, this column is recoverable so
// a logged-in owner can auto-fill the token in the MCP config UI. That is a
// deliberate trade-off (Option A) accepted for a low-user deployment; the
// column is never serialized (json:"-") and is only returned through the
// dedicated Admin retrieve endpoint.
func (e *MCPEndpoint) BeforeSave(_ *gorm.DB) error {
	if e.TokenEncrypted == "" {
		return nil
	}
	key := utils.GetAESKey()
	if key == nil {
		// No SYSTEM_AES_KEY: never persist the bearer token in plaintext. Drop the
		// recoverable copy instead; the endpoint keeps working (TokenHash
		// authenticates) and is reported as non-retrievable until rotated with a key.
		e.TokenEncrypted = ""
		return nil
	}
	// EncryptAESGCM is idempotent: already-prefixed values come back unchanged.
	encrypted, err := utils.EncryptAESGCM(e.TokenEncrypted, key)
	if err != nil {
		// Never fall through to storing the plaintext token silently: abort the
		// write so the caller sees the failure instead.
		return fmt.Errorf("encrypt mcp_endpoints.token_encrypted (id=%s): %w", e.ID, err)
	}
	e.TokenEncrypted = encrypted
	return nil
}
