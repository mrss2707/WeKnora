-- Mirrors module migration 900077_mcp_token_and_api_key_owner (postgres): keeps an
-- AES copy of the MCP endpoint bearer token for owner retrieval and records
-- the human who created a tenant API key for memory attribution.
ALTER TABLE mcp_endpoints ADD COLUMN token_encrypted TEXT NOT NULL DEFAULT '';
ALTER TABLE tenant_api_keys ADD COLUMN created_by VARCHAR(36) NOT NULL DEFAULT '';
