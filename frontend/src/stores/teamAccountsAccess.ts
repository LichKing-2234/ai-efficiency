import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { registerSessionResourceReset } from '@/stores/sessionResources'
import { listTeamAccounts } from '@/api/teamAccounts'
import { getTeamUsageScope } from '@/api/teamUsage'

/**
 * Tracks whether the signed-in person may open the organization-account page:
 * a department representative can create accounts, and anyone who already
 * manages one can review it. Administrators use the admin console instead.
 */
export const useTeamAccountsAccessStore = defineStore('teamAccountsAccess', () => {
  const loaded = ref(false)
  const loading = ref(false)
  const isRepresentative = ref(false)
  const accountCount = ref(0)

  const canOpen = computed(() => isRepresentative.value || accountCount.value > 0)

  async function ensureLoaded() {
    if (loaded.value || loading.value) return
    loading.value = true
    let resolved = false
    try {
      // Wrap each call so a missing module export in tests rejects instead of
      // throwing synchronously and leaving an unhandled rejection behind.
      const [scope, accounts] = await Promise.all([
        Promise.resolve().then(() => getTeamUsageScope()).catch(() => null),
        Promise.resolve().then(() => listTeamAccounts()).catch(() => null),
      ])
      if (scope) {
        isRepresentative.value = scope.data?.data?.is_representative ?? false
      }
      if (accounts) {
        accountCount.value = accounts.data?.data?.items?.length ?? 0
      }
      // Both lookups are required: a partial failure must be retried instead of
      // hiding the navigation entry for the rest of the session.
      resolved = Boolean(scope && accounts)
    } finally {
      // A failed lookup stays unresolved so the next mount can retry instead of
      // hiding the navigation entry for the whole session.
      loaded.value = resolved
      loading.value = false
    }
  }

  function reset() {
    loaded.value = false
    loading.value = false
    isRepresentative.value = false
    accountCount.value = 0
  }

  registerSessionResourceReset('teamAccountsAccess', reset)

  return { loaded, loading, isRepresentative, accountCount, canOpen, ensureLoaded, reset }
})
