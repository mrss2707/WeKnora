/**
 * Per-agent MCP config snippets for connecting a client to the WeKnora MCP
 * server (Streamable HTTP, Authorization: Bearer). Pure builders so the
 * "MCP Config" tab can render a copy-paste block per target agent.
 *
 * Each client stores MCP config differently:
 *  - Claude Code:  project `.mcp.json` — mcpServers entry with `type: "http"`.
 *  - Codex:        `~/.codex/config.toml` — a `[mcp_servers.<name>]` TOML table.
 *  - Antigravity:  `~/.gemini/config/mcp_config.json` (or `.agents/mcp_config.json`)
 *                  — mcpServers entry keyed on `serverUrl` (not `url`).
 *
 * `X-API-Key` carries a Tenant API Key (All Settings → API Integration) so the
 * server can attribute memory writes to the human who created that key. It is
 * always emitted with the `<API Key>` placeholder for the user to replace.
 */
export const MCP_API_KEY_PLACEHOLDER = '<API Key>'


/** Placeholders used when no MCP endpoint resolves for the current knowledge base. */
export const MCP_URL_PLACEHOLDER = 'https://your-weknora.example.com/mcp/<endpoint_id>'
export const MCP_SERVER_NAME_PLACEHOLDER = 'weknora'

function buildHeaders(token: string, apiKey: string): Record<string, string> {
  return { Authorization: `Bearer ${token}`, 'X-API-Key': apiKey }
}

export function buildClaudeCodeMcpConfig(name: string, url: string, token: string, apiKey: string = MCP_API_KEY_PLACEHOLDER): string {
  return JSON.stringify(
    {
      mcpServers: {
        [name]: {
          type: 'http',
          url,
          headers: buildHeaders(token, apiKey),
        },
      },
    },
    null,
    2,
  )
}

export function buildCodexMcpConfig(name: string, url: string, token: string, apiKey: string = MCP_API_KEY_PLACEHOLDER): string {
  const headers = Object.entries(buildHeaders(token, apiKey))
    .map(([key, value]) => `"${key}" = "${value}"`)
    .join(', ')
  return [`[mcp_servers.${name}]`, `url = "${url}"`, `http_headers = { ${headers} }`].join('\n')
}

export function buildAntigravityMcpConfig(name: string, url: string, token: string, apiKey: string = MCP_API_KEY_PLACEHOLDER): string {
  return JSON.stringify(
    {
      mcpServers: {
        [name]: {
          serverUrl: url,
          headers: buildHeaders(token, apiKey),
        },
      },
    },
    null,
    2,
  )
}
