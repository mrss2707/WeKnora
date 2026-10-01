# MCP attribution module migration (PostgreSQL)

Adds `mcp_endpoints.token_encrypted` (AES-256-GCM copy of the bearer token for
owner auto-fill) and `tenant_api_keys.created_by` (human creator, used to
attribute MCP memory saves).

| Version | File | Purpose |
|---------|------|---------|
| 900077 | `mcp_token_and_api_key_owner` | `ADD COLUMN IF NOT EXISTS` on both tables |

Backend scope: Lite/SQLite never reads `modules/*/postgres/`; the equivalent
columns live in `sqlite/900077_mcp_token_and_api_key_owner` (same module, Lite mode only reads `modules/*/sqlite/`).

Legacy endpoints keep `token_encrypted = ''` (not retrievable) until rotated.
Every statement is idempotent. See `migrations/modules/memory_v2/README.md` for
the shared upgrade/rollback guide.
