import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getWorkItemCounts } from '@/api/workItems'
import type { WorkItemCounts } from '@/types'

const emptyCounts: WorkItemCounts = {
  quota_reset_approval_count: 0,
  quota_reset_admin_count: 0,
  offboarding_count: 0,
  total_count: 0,
}

function normalizeCounts(data?: Partial<WorkItemCounts> | null): WorkItemCounts {
  return {
    quota_reset_approval_count: Number(data?.quota_reset_approval_count ?? 0),
    quota_reset_admin_count: Number(data?.quota_reset_admin_count ?? 0),
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

  const totalCount = computed(() => counts.value.total_count)
  const badgeLabel = computed(() => formatWorkItemCount(totalCount.value))

  async function loadCounts() {
    loading.value = true
    error.value = ''
    try {
      const res = await getWorkItemCounts()
      counts.value = normalizeCounts(res.data.data)
      loaded.value = true
    } catch {
      error.value = 'failed'
      counts.value = { ...emptyCounts }
    } finally {
      loading.value = false
    }
  }

  return { counts, loading, loaded, error, totalCount, badgeLabel, loadCounts }
})
