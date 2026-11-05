import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '@/i18n'
import TeamAccountSection from '@/components/team-accounts/TeamAccountSection.vue'

vi.mock('@/api/teamAccounts', () => ({
  listTeamAccounts: vi.fn(),
  getTeamAccount: vi.fn(),
  getTeamAccountGroupOptions: vi.fn(),
  createTeamAccount: vi.fn(),
  revealTeamAccountCredentials: vi.fn(),
  createTeamAccountKey: vi.fn(),
  revokeTeamAccountKey: vi.fn(),
  addTeamAccountManager: vi.fn(),
  removeTeamAccountManager: vi.fn(),
  getTeamAccountUsage: vi.fn(),
  listTeamAccountEvents: vi.fn(),
}))
vi.mock('@/api/teamUsage', () => ({ getTeamUsageScope: vi.fn() }))

const account = {
  id: 6, name: 'test', department_external_id: '1684078',
  department_display_path: '声网 / SDK框架与效能组', status: 'active',
  owner_user_id: 447, owner_username: 'sdkrep', created_at: '2026-11-05T10:00:00Z',
}

describe('TeamAccountSection refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
    void setLocale('zh-CN')
  })

  it('keeps a manager section and its error visible when refresh fails', async () => {
    const accounts = await import('@/api/teamAccounts')
    const usage = await import('@/api/teamUsage')
    vi.mocked(usage.getTeamUsageScope).mockResolvedValue({ data: { code: 0, data: { is_representative: false, departments: [] } } } as any)
    vi.mocked(accounts.listTeamAccounts).mockResolvedValue({ data: { code: 0, data: { items: [account] } } } as any)
    vi.mocked(accounts.getTeamAccount).mockResolvedValue({ data: { code: 0, data: { ...account, keys: [], managers: [], available_groups: [] } } } as any)

    const wrapper = mount(TeamAccountSection, {
      global: { plugins: [createPinia()], stubs: { teleport: true } },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="team-account-section"]').exists()).toBe(true)

    vi.mocked(accounts.listTeamAccounts).mockRejectedValue(new Error('offline'))
    await wrapper.get('[data-testid="team-account-reload"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="team-account-section"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-account-load-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('test')
  })
})
