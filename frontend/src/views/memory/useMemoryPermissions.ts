import { computed, type Ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useOrganizationStore } from '@/stores/organization'
import { permissionCanEditKB, permissionCanManageKB } from '@/utils/kbPermission'

// Memory routes grant home-workspace KB access to all members, then enforce
// Contributor/Admin role floors. Document ownership rules are not applicable.
export function useMemoryPermissions(kb: Ref<any>) {
  const auth = useAuthStore()
  const organizations = useOrganizationStore()
  const home = computed(() => !!kb.value?.tenant_id && String(kb.value.tenant_id) === String(auth.effectiveTenantId))
  const permission = computed(() => organizations.getKBPermission(kb.value?.id || '') || kb.value?.my_permission || '')
  return {
    canEditMemory: computed(() => auth.hasRole('contributor') && (home.value || permissionCanEditKB(permission.value))),
    canManageMemory: computed(() => auth.hasRole('admin') && (home.value || permissionCanManageKB(permission.value))),
  }
}
