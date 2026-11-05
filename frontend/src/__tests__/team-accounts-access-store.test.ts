import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/teamUsage', () => ({ getTeamUsageScope: vi.fn() }))
vi.mock('@/api/teamAccounts', () => ({ listTeamAccounts: vi.fn() }))

describe('team accounts access store', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.resetModules()
    setActivePinia(createPinia())
  })

  it('opens the nav only when both lookups succeed', async () => {
    const usage = await import('@/api/teamUsage')
    const accounts = await import('@/api/teamAccounts')
    vi.mocked(usage.getTeamUsageScope).mockResolvedValue({ data: { code: 0, data: { is_representative: true } } } as any)
    vi.mocked(accounts.listTeamAccounts).mockRejectedValue(new Error('boom'))
    const { useTeamAccountsAccessStore } = await import('@/stores/teamAccountsAccess')
    const store = useTeamAccountsAccessStore()
    await store.ensureLoaded()
    expect(store.loaded).toBe(false)
    expect(store.canOpen).toBe(false)

    vi.mocked(accounts.listTeamAccounts).mockResolvedValue({ data: { code: 0, data: { items: [] } } } as any)
    await store.ensureLoaded()
    expect(store.loaded).toBe(true)
    expect(store.canOpen).toBe(true)
  })

  it('resets when the session changes', async () => {
    const usage = await import('@/api/teamUsage')
    const accounts = await import('@/api/teamAccounts')
    vi.mocked(usage.getTeamUsageScope).mockResolvedValue({ data: { code: 0, data: { is_representative: true } } } as any)
    vi.mocked(accounts.listTeamAccounts).mockResolvedValue({ data: { code: 0, data: { items: [] } } } as any)
    const { useTeamAccountsAccessStore } = await import('@/stores/teamAccountsAccess')
    const store = useTeamAccountsAccessStore()
    await store.ensureLoaded()
    expect(store.canOpen).toBe(true)

    store.reset()
    expect(store.loaded).toBe(false)
    expect(store.canOpen).toBe(false)
  })
})
