import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useRepoStore } from '@/stores/repo'

vi.mock('@/api/repo', () => ({
  listRepos: vi.fn(),
  createRepo: vi.fn(),
  deleteRepo: vi.fn(),
}))

describe('Repo Store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('starts with empty repos', () => {
    const store = useRepoStore()
    expect(store.repos).toEqual([])
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
    expect(store.currentRepo).toBeNull()
  })

  it('fetchRepos populates repos list', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockResolvedValue({
      data: {
        data: {
          items: [
            { id: 1, name: 'repo-a', full_name: 'org/repo-a', status: 'active', ai_score: 80 },
            { id: 2, name: 'repo-b', full_name: 'org/repo-b', status: 'active', ai_score: 50 },
          ],
          total: 2,
          page: 1,
          page_size: 20,
        },
      },
    })

    const store = useRepoStore()
    await store.fetchRepos()

    expect(store.repos).toHaveLength(2)
    expect(store.repos[0].full_name).toBe('org/repo-a')
    expect(store.loading).toBe(false)
  })

  it('fetchRepos handles empty response', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockResolvedValue({
      data: { data: null },
    })

    const store = useRepoStore()
    await store.fetchRepos()

    expect(store.repos).toEqual([])
  })

  it('createRepo adds to list', async () => {
    const { createRepo } = await import('@/api/repo')
    const newRepo = { id: 3, name: 'repo-c', full_name: 'org/repo-c', status: 'active', ai_score: 0 }
    ;(createRepo as any).mockResolvedValue({ data: { data: newRepo } })

    const store = useRepoStore()
    await store.createRepo({ name: 'repo-c', full_name: 'org/repo-c' })

    expect(store.repos).toHaveLength(1)
    expect(store.repos[0].name).toBe('repo-c')
  })

  it('deleteRepo removes from list', async () => {
    const { deleteRepo } = await import('@/api/repo')
    ;(deleteRepo as any).mockResolvedValue({ data: { data: null } })

    const store = useRepoStore()
    store.repos = [
      { id: 1, repo_key: 'github.com/org/repo-a', name: 'repo-a', full_name: 'org/repo-a', clone_url: '', default_branch: 'main', ai_score: 0, status: 'active', binding_state: 'bound', last_scan_at: null, group_id: 0, created_at: '' },
      { id: 2, repo_key: 'github.com/org/repo-b', name: 'repo-b', full_name: 'org/repo-b', clone_url: '', default_branch: 'main', ai_score: 0, status: 'active', binding_state: 'bound', last_scan_at: null, group_id: 0, created_at: '' },
    ]

    await store.deleteRepo(1)

    expect(store.repos).toHaveLength(1)
    expect(store.repos[0].id).toBe(2)
  })

  // --- New tests for uncovered lines ---

  it('fetchRepos sets error on failure', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockRejectedValue({
      response: { data: { message: 'Server error' } },
    })

    const store = useRepoStore()
    await store.fetchRepos()

    expect(store.error).toBe('Server error')
    expect(store.loading).toBe(false)
    expect(store.repos).toEqual([])
  })

  it('fetchRepos sets generic error when no message', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockRejectedValue(new Error('network'))

    const store = useRepoStore()
    await store.fetchRepos()

    expect(store.error).toBe('Failed to fetch repos')
    expect(store.loading).toBe(false)
  })

  it('fetchRepos with custom page and pageSize', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: [], total: 0, page: 2, page_size: 10 } },
    })

    const store = useRepoStore()
    await store.fetchRepos(2, 10)

    expect(listRepos).toHaveBeenCalledWith(2, 10)
  })

  it('fetchRepos clears previous error on success', async () => {
    const { listRepos } = await import('@/api/repo')

    // First call fails
    ;(listRepos as any).mockRejectedValueOnce(new Error('fail'))
    const store = useRepoStore()
    await store.fetchRepos()
    expect(store.error).toBe('Failed to fetch repos')

    // Second call succeeds
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: [], total: 0, page: 1, page_size: 20 } },
    })
    await store.fetchRepos()
    expect(store.error).toBeNull()
  })
})
