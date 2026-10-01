-- Migration: 900077_mcp_token_and_api_key_owner
-- Two columns for MCP memory attribution + token auto-fill:
--
--   * mcp_endpoints.token_encrypted keeps an AES-256-GCM copy of the endpoint
--     bearer token (SYSTEM_AES_KEY) so a logged-in owner can auto-fill it in
--     the MCP config UI. The SHA-256 TokenHash still authenticates; this column
--     only enables retrieval. Rows written before this migration stay blank
--     (legacy endpoints) until the next rotate.
--   * tenant_api_keys.created_by records the human user who created the API
--     key so that, when a memory is saved over MCP with that key, the server
--     can attribute the memory to a real user instead of a synthetic principal.
DO $$ BEGIN RAISE NOTICE '[Migration 900077] Adding mcp_endpoints.token_encrypted and tenant_api_keys.created_by'; END $$;

ALTER TABLE mcp_endpoints
    ADD COLUMN IF NOT EXISTS token_encrypted TEXT NOT NULL DEFAULT '';

ALTER TABLE tenant_api_keys
    ADD COLUMN IF NOT EXISTS created_by VARCHAR(36) NOT NULL DEFAULT '';

COMMENT ON COLUMN mcp_endpoints.token_encrypted IS 'AES-256-GCM (enc:v1: prefix) copy of the bearer token for owner retrieval; blank for legacy rows written before auto-fill existed';
COMMENT ON COLUMN tenant_api_keys.created_by IS 'User id (varchar(36)) of the human who created this API key; empty for machine/synthetic creators';
