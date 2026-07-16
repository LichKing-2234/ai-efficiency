import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useRepoStore } from '@/stores/repo'

vi.mock('@/api/repo', () => ({
  listRepos: vi.fn(),
  getRepoInventory: vi.fn(),
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
    expect(store.total).toBe(0)
    expect(store.page).toBe(1)
    expect(store.pageSize).toBe(20)
    expect(store.inventory).toEqual([])
    expect(store.selection).toBeNull()
    expect(store.loaded).toBe(false)
    expect(store.inventoryError).toBeNull()
  })

  it('fetchRepos populates repos list', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockResolvedValue({
      data: {
        data: {
          items: [
            { id: 1, name: 'repo-a', full_name: 'org/repo-a', status: 'active' },
            { id: 2, name: 'repo-b', full_name: 'org/repo-b', status: 'active' },
          ],
          total: 2,
          page: 1,
          page_size: 20,
          selection: {
            provider_key: 'scm_provider:1',
            provider_id: 1,
            provider_name: 'GitHub',
            provider_type: 'github',
            scope: 'org',
            binding_state: 'bound',
          },
        },
      },
    })

    const store = useRepoStore()
    await store.fetchRepos()

    expect(store.repos).toHaveLength(2)
    expect(store.repos[0].full_name).toBe('org/repo-a')
    expect(store.total).toBe(2)
    expect(store.page).toBe(1)
    expect(store.pageSize).toBe(20)
    expect(store.loading).toBe(false)
    expect(store.loaded).toBe(true)
    expect(store.selection?.scope).toBe('org')
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
    const newRepo = { id: 3, name: 'repo-c', full_name: 'org/repo-c', status: 'active' }
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
      { id: 1, repo_key: 'github.com/org/repo-a', name: 'repo-a', full_name: 'org/repo-a', clone_url: '', default_branch: 'main', status: 'active', binding_state: 'bound', group_id: 0, created_at: '' },
      { id: 2, repo_key: 'github.com/org/repo-b', name: 'repo-b', full_name: 'org/repo-b', clone_url: '', default_branch: 'main', status: 'active', binding_state: 'bound', group_id: 0, created_at: '' },
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

  it('fetchRepos passes scoped list params and stores pagination', async () => {
    const { listRepos } = await import('@/api/repo')
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: [], total: 0, page: 2, page_size: 10 } },
    })

    const store = useRepoStore()
    await store.fetchRepos({ page: 2, pageSize: 10, scmProviderId: 7, scope: 'org', bindingState: 'bound' })

    expect(listRepos).toHaveBeenCalledWith({
      page: 2,
      pageSize: 10,
      scmProviderId: 7,
      scope: 'org',
      bindingState: 'bound',
    })
    expect(store.page).toBe(2)
    expect(store.pageSize).toBe(10)
  })

  it('ignores an older list response after a newer request starts', async () => {
    const { listRepos } = await import('@/api/repo')
    let resolveFirst!: (value: any) => void
    let resolveSecond!: (value: any) => void
    ;(listRepos as any)
      .mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
      .mockReturnValueOnce(new Promise((resolve) => { resolveSecond = resolve }))

    const store = useRepoStore()
    const firstRequest = store.fetchRepos({ page: 1 })
    const secondRequest = store.fetchRepos({ page: 2 })

    resolveFirst({ data: { data: { items: [{ id: 1, full_name: 'org/old' }], total: 1, page: 1, page_size: 20 } } })
    await firstRequest
    expect(store.repos).toEqual([])
    expect(store.loading).toBe(true)
    expect(store.loaded).toBe(false)

    resolveSecond({ data: { data: { items: [{ id: 2, full_name: 'org/current' }], total: 1, page: 2, page_size: 20 } } })
    await secondRequest
    expect(store.repos.map((item) => item.id)).toEqual([2])
    expect(store.page).toBe(2)
    expect(store.loading).toBe(false)
    expect(store.loaded).toBe(true)
  })

  it('fetchInventory populates platform and scope summaries', async () => {
    const { getRepoInventory } = await import('@/api/repo')
    ;(getRepoInventory as any).mockResolvedValue({
      data: {
        data: [
          {
            provider_key: 'scm_provider:1',
            provider_id: 1,
            name: 'GitHub',
            type: 'github',
            total_repos: 2,
            bound_repos: 2,
            unbound_repos: 0,
            active_repos: 2,
            webhook_failed_repos: 0,
            scopes: [{ scope: 'org', total_repos: 2, bound_repos: 2, unbound_repos: 0, active_repos: 2, webhook_failed_repos: 0 }],
          },
        ],
      },
    })

    const store = useRepoStore()
    await store.fetchInventory()

    expect(store.inventory).toHaveLength(1)
    expect(store.inventory[0].scopes[0].scope).toBe('org')
    expect(store.inventoryLoading).toBe(false)
    expect(store.inventoryError).toBeNull()
  })

  it('keeps list rows and list error independent when inventory fails', async () => {
    const { listRepos, getRepoInventory } = await import('@/api/repo')
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: [{ id: 7, full_name: 'org/repo-a' }], total: 1, page: 1, page_size: 20 } },
    })
    ;(getRepoInventory as any).mockRejectedValue({ response: { data: { message: 'Inventory unavailable' } } })

    const store = useRepoStore()
    await store.fetchRepos()
    await store.fetchInventory()

    expect(store.repos).toHaveLength(1)
    expect(store.error).toBeNull()
    expect(store.inventoryError).toBe('Inventory unavailable')
    expect(store.inventory).toEqual([])
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
