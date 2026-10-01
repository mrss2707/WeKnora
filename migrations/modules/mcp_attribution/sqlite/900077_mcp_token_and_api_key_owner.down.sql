-- Mirrors module migration 900077_mcp_token_and_api_key_owner (postgres), down.
ALTER TABLE mcp_endpoints DROP COLUMN token_encrypted;
ALTER TABLE tenant_api_keys DROP COLUMN created_by;
