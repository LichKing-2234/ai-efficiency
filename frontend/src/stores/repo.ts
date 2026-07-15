import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listRepos, getRepoInventory, createRepo as apiCreateRepo, deleteRepo as apiDeleteRepo } from '@/api/repo'
import type { RepoConfig, RepoInventoryProviderSummary, RepoListParams, RepoListSelection } from '@/types'

export const useRepoStore = defineStore('repo', () => {
  const repos = ref<RepoConfig[]>([])
  const currentRepo = ref<RepoConfig | null>(null)
  const loading = ref(false)
  const loaded = ref(false)
  const inventoryLoading = ref(false)
  const inventoryLoaded = ref(false)
  const error = ref<string | null>(null)
  const inventoryError = ref<string | null>(null)
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const inventory = ref<RepoInventoryProviderSummary[]>([])
  const selection = ref<RepoListSelection | null>(null)

  async function fetchRepos(params: RepoListParams = {}) {
    loading.value = true
    error.value = null
    const requestParams = {
      ...params,
      page: params.page ?? 1,
      pageSize: params.pageSize ?? 20,
    }
    try {
      const res = await listRepos(requestParams)
      const data = res.data.data
      repos.value = data?.items ?? []
      total.value = data?.total ?? repos.value.length
      page.value = data?.page ?? requestParams.page
      pageSize.value = data?.page_size ?? requestParams.pageSize
      selection.value = data?.selection ?? null
    } catch (e: any) {
      error.value = e.response?.data?.message || 'Failed to fetch repos'
    } finally {
      loading.value = false
      loaded.value = true
    }
  }

  async function fetchInventory() {
    inventoryLoading.value = true
    inventoryError.value = null
    try {
      const res = await getRepoInventory()
      inventory.value = res.data.data ?? []
    } catch (e: any) {
      inventoryError.value = e.response?.data?.message || 'Failed to fetch repo inventory'
    } finally {
      inventoryLoading.value = false
      inventoryLoaded.value = true
    }
  }

  async function createRepo(data: Partial<RepoConfig>) {
    const res = await apiCreateRepo(data)
    if (res.data.data) {
      repos.value.push(res.data.data)
    }
  }

  async function deleteRepo(id: number) {
    await apiDeleteRepo(id)
    repos.value = repos.value.filter((r) => r.id !== id)
    total.value = Math.max(0, total.value - 1)
  }

  return {
    repos,
    currentRepo,
    loading,
    loaded,
    inventoryLoading,
    inventoryLoaded,
    error,
    inventoryError,
    total,
    page,
    pageSize,
    inventory,
    selection,
    fetchRepos,
    fetchInventory,
    createRepo,
    deleteRepo,
  }
})
