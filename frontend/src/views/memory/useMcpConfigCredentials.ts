import { ref, watch, type Ref } from 'vue'
import { getMcpEndpointToken } from '@/api/mcp-endpoint'

/**
 * Credentials for the MCP Config tab.
 *  - `token` is auto-filled from the endpoint (owner-only retrieve API) so the
 *    logged-in user never has to copy it by hand.
 */
export function useMcpConfigCredentials(endpointId: Ref<string | null | undefined>) {
  const token = ref('')
  const tokenRetrievable = ref(false)

  watch(
    endpointId,
    async (id) => {
      token.value = ''
      tokenRetrievable.value = false
      if (!id) return
      try {
        const res = await getMcpEndpointToken(id)
        const data = res?.data
        if (data?.retrievable && data.token) {
          token.value = data.token
          tokenRetrievable.value = true
        }
      } catch {
        // Not an admin, legacy endpoint, or key unavailable: fall back to the
        // placeholder and let the user rotate/paste the token manually.
      }
    },
    { immediate: true },
  )

  return { token, tokenRetrievable }
}
