import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getWorkItemCounts } from '@/api/workItems'
import type { WorkItemCounts } from '@/types'

const emptyCounts: WorkItemCounts = {
  quota_reset_approval_count: 0,
  quota_reset_admin_count: 0,
  ai_access_setup_count: 0,
  offboarding_count: 0,
  total_count: 0,
}

function normalizeCounts(data?: Partial<WorkItemCounts> | null): WorkItemCounts {
  return {
    quota_reset_approval_count: Number(data?.quota_reset_approval_count ?? 0),
    quota_reset_admin_count: Number(data?.quota_reset_admin_count ?? 0),
    ai_access_setup_count: Number(data?.ai_access_setup_count ?? 0),
    offboarding_count: Number(data?.offboarding_count ?? 0),
    total_count: Number(data?.total_count ?? 0),
  }
}

export function formatWorkItemCount(count: number) {
  return count > 99 ? '99+' : String(Math.max(0, count))
}

export const useWorkItemsStore = defineStore('workItems', () => {
  const counts = ref<WorkItemCounts>({ ...emptyCounts })
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  let loadPromise: Promise<void> | null = null

  const totalCount = computed(() => counts.value.total_count)
  const badgeLabel = computed(() => formatWorkItemCount(totalCount.value))

  function loadCounts() {
    if (loadPromise) return loadPromise
    loading.value = true
    error.value = ''
    loadPromise = (async () => {
      try {
        const res = await getWorkItemCounts()
        counts.value = normalizeCounts(res.data.data)
        loaded.value = true
      } catch {
        error.value = 'failed'
        if (!loaded.value) {
          counts.value = { ...emptyCounts }
        }
      } finally {
        loading.value = false
        loadPromise = null
      }
    })()
    return loadPromise
  }

  return { counts, loading, loaded, error, totalCount, badgeLabel, loadCounts }
})
