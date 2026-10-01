<template>
  <div class="memory-mcp-config">
    <!-- Connection: per-agent MCP config guides (Claude Code / Codex / Antigravity) -->
    <section class="mcp-config-section">
      <h4 class="mcp-config-section__title">{{ $t('memory.mcpConfig.connectionTitle') }}</h4>
      <p class="mcp-config-section__desc">{{ $t('memory.mcpConfig.desc') }}</p>

      <t-loading v-if="loading" :loading="true" />
      <template v-else>
        <p v-if="!primaryEndpoint" class="mcp-config-note">{{ $t('memory.mcpConfig.noEndpoint') }}</p>
        <p v-if="primaryEndpoint && !tokenRetrievable" class="mcp-config-note">
          {{ $t('memory.mcpConfig.tokenNotRetrievable') }}
        </p>
        <t-tabs v-model="agentTab" size="small" class="mcp-config-agent-tabs">
        <t-tab-panel v-for="agent in agents" :key="agent.key" :value="agent.key" :label="agent.label">
          <div class="mcp-config-agent">
            <p class="mcp-config-agent__file">
              {{ $t('memory.mcpConfig.configFile') }}: <code>{{ agent.file }}</code>
            </p>
            <p class="mcp-config-agent__hint">{{ agent.hint }}</p>
            <div class="code-block">
              <pre class="code-block__pre">{{ agent.config }}</pre>
              <t-button class="code-block__copy" size="small" variant="text" shape="square"
                        :title="$t('common.copy')" @click="copy(agent.config)">
                <t-icon name="file-copy" size="16px" />
              </t-button>
            </div>
          </div>
        </t-tab-panel>
      </t-tabs>
      </template>
    </section>

    <!-- Instruction: paste into the agent's project file -->
    <section class="mcp-config-section">
      <h4 class="mcp-config-section__title">{{ $t('memory.mcpConfig.instructionTitle') }}</h4>
      <p class="mcp-config-section__desc">{{ $t('memory.mcpConfig.instructionDesc') }}</p>
      <div class="code-block code-block--tall">
        <pre class="code-block__pre">{{ MEMORY_PROTOCOL_INSTRUCTION }}</pre>
        <t-button class="code-block__copy" size="small" variant="text" shape="square"
                  :title="$t('common.copy')" @click="copy(MEMORY_PROTOCOL_INSTRUCTION)">
          <t-icon name="file-copy" size="16px" />
        </t-button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { copyWithToast } from '@/utils/clipboard'
import { useApiBaseUrlDisplay } from '@/composables/useApiBaseUrlDisplay'
import { listMcpEndpoints, type McpEndpoint } from '@/api/mcp-endpoint'
import {
  buildMcpEndpointUrl,
  mcpServerKey,
  MCP_TOKEN_PLACEHOLDER,
} from '@/views/integrations/mcpServerIntegration'
import {
  buildAntigravityMcpConfig,
  buildClaudeCodeMcpConfig,
  buildCodexMcpConfig,
  MCP_SERVER_NAME_PLACEHOLDER,
  MCP_URL_PLACEHOLDER,
} from './agentMcpConfigs'
import { MEMORY_PROTOCOL_INSTRUCTION } from './memoryProtocolInstruction'
import { useMcpConfigCredentials } from './useMcpConfigCredentials'

const props = defineProps<{ kbId: string }>()

const { t } = useI18n()
const { apiBaseUrlDisplay } = useApiBaseUrlDisplay()

const loading = ref(true)
const endpoints = ref<McpEndpoint[]>([])
const agentTab = ref('claudeCode')

/** Endpoints that cover this KB: empty scope means "all knowledge bases". */
const matchingEndpoints = computed(() =>
  endpoints.value.filter(
    (ep) => !ep.knowledge_base_ids?.length || ep.knowledge_base_ids.includes(props.kbId),
  ),
)

const primaryEndpoint = computed(() => matchingEndpoints.value[0] || null)

// Token is auto-filled from the endpoint; the config carries an <API Key> placeholder.
const { token, tokenRetrievable } = useMcpConfigCredentials(
  computed(() => primaryEndpoint.value?.id),
)

function endpointUrl(ep: McpEndpoint): string {
  return buildMcpEndpointUrl(apiBaseUrlDisplay.value, ep.path)
}

/** Per-agent config guides: label (product name), config file path, hint, snippet.
 *  Always rendered so the guide is visible even before an endpoint covers this KB;
 *  the URL falls back to a placeholder when none resolves. */
const agents = computed(() => {
  const ep = primaryEndpoint.value
  const name = ep ? mcpServerKey(ep.name, ep.id) : MCP_SERVER_NAME_PLACEHOLDER
  const url = ep ? endpointUrl(ep) : MCP_URL_PLACEHOLDER
  const tokenValue = tokenRetrievable.value && token.value ? token.value : MCP_TOKEN_PLACEHOLDER
  return [
    {
      key: 'claudeCode',
      label: 'Claude Code',
      file: '.mcp.json',
      hint: t('memory.mcpConfig.hintClaudeCode'),
      config: buildClaudeCodeMcpConfig(name, url, tokenValue),
    },
    {
      key: 'codex',
      label: 'Codex',
      file: '~/.codex/config.toml',
      hint: t('memory.mcpConfig.hintCodex'),
      config: buildCodexMcpConfig(name, url, tokenValue),
    },
    {
      key: 'antigravity',
      label: 'Antigravity',
      file: '~/.gemini/config/mcp_config.json',
      hint: t('memory.mcpConfig.hintAntigravity'),
      config: buildAntigravityMcpConfig(name, url, tokenValue),
    },
  ]
})

function copy(text: string) {
  copyWithToast(text, 'memory.mcpConfig.copied')
}

onMounted(async () => {
  try {
    const res = await listMcpEndpoints()
    endpoints.value = res?.data ?? []
  } catch {
    endpoints.value = []
  } finally {
    loading.value = false
  }
})
</script>

<style scoped lang="less">
.memory-mcp-config {
  display: flex;
  flex-direction: column;
  gap: var(--app-space-6);
  padding: var(--app-space-2) var(--app-space-1);
}

.mcp-config-section {
  &__title {
    margin: 0 0 var(--app-space-2);
    font-size: var(--app-text-lg);
    font-weight: 600;
  }
  &__desc {
    margin: 0 0 var(--app-space-3);
    font-size: var(--app-text-md);
    color: var(--td-text-color-secondary);
    line-height: 1.6;
  }
}

.mcp-config-note {
  margin: 0 0 var(--app-space-2);
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}

.mcp-config-agent {
  margin-top: var(--app-space-2);

  &__file {
    margin: 0 0 var(--app-space-1);
    font-size: var(--app-text-md);

    code {
      padding: 0 var(--app-space-1);
      background: var(--td-bg-color-secondarycontainer);
      border-radius: var(--app-radius-xs);
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: var(--app-text-sm);
    }
  }
  &__hint {
    margin: 0 0 var(--app-space-2);
    font-size: var(--app-text-sm);
    color: var(--td-text-color-secondary);
    line-height: 1.6;
  }
}

.code-block {
  position: relative;
  margin-top: var(--app-space-2);

  &__pre {
    margin: 0;
    padding: var(--app-space-3) var(--app-space-10) var(--app-space-3) var(--app-space-3);
    background: var(--td-bg-color-secondarycontainer);
    border-radius: var(--app-radius-sm);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: var(--app-text-sm);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 160px;
    overflow: auto;
  }

  &--tall .code-block__pre {
    max-height: 360px;
  }

  &__copy {
    position: absolute;
    top: 6px;
    right: 6px;
  }
}
</style>
