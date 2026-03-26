import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listRepos, createRepo as apiCreateRepo, deleteRepo as apiDeleteRepo } from '@/api/repo'
import type { RepoConfig } from '@/types'

export const useRepoStore = defineStore('repo', () => {
  const repos = ref<RepoConfig[]>([])
  const currentRepo = ref<RepoConfig | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchRepos(page = 1, pageSize = 20) {
    loading.value = true
    error.value = null
    try {
      const res = await listRepos(page, pageSize)
      repos.value = res.data.data?.items ?? []
    } catch (e: any) {
      error.value = e.response?.data?.message || 'Failed to fetch repos'
    } finally {
      loading.value = false
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
  }

  return { repos, currentRepo, loading, error, fetchRepos, createRepo, deleteRepo }
})
