import { ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { getMemory, type AgentMemory } from '@/api/memory/index'
import { useMemoryStore } from '@/stores/memory'

/**
 * Drawer and cross-tab navigation of the knowledge base Memory tab, kept out
 * of KnowledgeBase.vue so the host screen only wires props and events.
 */
export function useMemoryTabNavigation() {
  const { t } = useI18n()
  const memoryStore = useMemoryStore()
  const memoryDrawerVisible = ref(false)
  const selectedMemory = ref<AgentMemory | null>(null)
  const graphFocusId = ref<string | null>(null)

  function openMemoryDrawer(memory: AgentMemory) {
    selectedMemory.value = memory
    memoryDrawerVisible.value = true
  }

  /** Opens a memory known only by id (graph node, history entry, health issue). */
  async function openMemoryById(id: string) {
    const cached = memoryStore.memories.find((m) => m.id === id)
    if (cached) {
      openMemoryDrawer(cached)
      return
    }
    try {
      const resp = await getMemory(id)
      if (resp.data) openMemoryDrawer(resp.data)
    } catch {
      MessagePlugin.warning(t('memory.drawer.notFound'))
    }
  }

  function closeMemoryDrawer() {
    memoryDrawerVisible.value = false
    selectedMemory.value = null
  }

  function handleMemoryUpdate(updated: AgentMemory) {
    memoryStore.replaceMemory(updated)
  }

  function viewMemoryInGraph(id: string) {
    closeMemoryDrawer()
    graphFocusId.value = id
    memoryStore.setSubTab('graph')
  }

  return {
    memoryDrawerVisible,
    selectedMemory,
    graphFocusId,
    openMemoryDrawer,
    openMemoryById,
    closeMemoryDrawer,
    handleMemoryUpdate,
    viewMemoryInGraph,
  }
}
