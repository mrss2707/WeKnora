-- Down for 900077_mcp_token_and_api_key_owner: drop the token ciphertext and
-- API-key owner columns. Token auth is unaffected (TokenHash is retained).
DROP INDEX IF EXISTS idx_mcp_endpoints_token_encrypted;
ALTER TABLE mcp_endpoints DROP COLUMN IF EXISTS token_encrypted;
ALTER TABLE tenant_api_keys DROP COLUMN IF EXISTS created_by;
