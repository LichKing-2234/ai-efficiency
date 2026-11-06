import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '@/i18n'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import TeamAccountDetailDialog from '@/components/team-accounts/TeamAccountDetailDialog.vue'
import TeamAccountUserPicker from '@/components/team-accounts/TeamAccountUserPicker.vue'

vi.mock('@/api/teamAccounts', () => ({
  getTeamAccount: vi.fn(),
  getTeamAccountUsage: vi.fn(),
  listTeamAccountEvents: vi.fn(),
  revealTeamAccountCredentials: vi.fn(),
  createTeamAccountKey: vi.fn(),
  revokeTeamAccountKey: vi.fn(),
  addTeamAccountManager: vi.fn(),
  removeTeamAccountManager: vi.fn(),
  getAdminTeamAccount: vi.fn(),
  getAdminTeamAccountUsage: vi.fn(),
  listAdminTeamAccountEvents: vi.fn(),
  revealAdminTeamAccountCredentials: vi.fn(),
  createAdminTeamAccountKey: vi.fn(),
  revokeAdminTeamAccountKey: vi.fn(),
  addAdminTeamAccountManager: vi.fn(),
  removeAdminTeamAccountManager: vi.fn(),
}))

const detail = {
  id: 6,
  name: 'test',
  department_external_id: '1684078',
  department_display_path: '声网 / SDK框架与效能组',
  relay_username: 'team-1684078',
  relay_email: 'team-1684078@invalid.account',
  owner_user_id: 447,
  owner_username: 'sdkrep',
  owner_email: 'sdkrep@invalid.account',
  status: 'active',
  created_at: '2026-11-05T10:00:00Z',
  keys: [],
  managers: [
    { user_id: 447, username: 'sdkrep', email: 'sdkrep@invalid.account', role: 'owner' },
    { user_id: 448, username: 'sdkmember', email: 'sdkmember@invalid.account', role: 'manager' },
  ],
  available_groups: [{ id: 10, name: 'China', platform: 'composite' }],
}

async function mountAs(userID: number) {
  // Teleported dialogs from earlier tests would otherwise answer the body queries.
  document.body.innerHTML = ''
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = { id: userID, username: 'u', email: 'u@example.com', role: 'user' } as any
  const api = await import('@/api/teamAccounts')
  vi.mocked(api.getTeamAccount).mockResolvedValue({ data: { code: 0, data: detail } } as any)
  vi.mocked(api.getTeamAccountUsage).mockResolvedValue({ data: { code: 0, data: { total_tokens: 0, total_cost: 0, days: [], keys: [] } } } as any)
  vi.mocked(api.listTeamAccountEvents).mockResolvedValue({ data: { code: 0, data: { items: [] } } } as any)
  const wrapper = mount(TeamAccountDetailDialog, {
    props: { modelValue: true, accountId: 6, mode: 'user' },
    global: {
      plugins: [pinia],
      config: { errorHandler: (error: unknown) => { console.error('RENDER ERROR', error) } },
    },
    attachTo: document.body,
  })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  return { wrapper, api, body }
}

describe('TeamAccountDetailDialog permissions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    void setLocale('zh-CN')
  })

  it('lets a manager create keys but hides owner-only controls', async () => {
    const { body } = await mountAs(448)
    expect(body.find('[data-testid="team-account-key-group"]').exists()).toBe(true)
    expect(body.find('[data-testid="team-account-transfer"]').exists()).toBe(false)
    expect(body.find('[data-testid="team-account-add-manager"]').exists()).toBe(false)
    expect(body.find('[data-testid="team-account-remove-manager-448"]').exists()).toBe(false)
  })

  it('shows owner-only controls to the owner', async () => {
    const { body } = await mountAs(447)
    expect(body.find('[data-testid="team-account-transfer"]').exists()).toBe(true)
    expect(body.find('[data-testid="team-account-add-manager"]').exists()).toBe(true)
    expect(body.find('[data-testid="team-account-remove-manager-448"]').exists()).toBe(true)
  })

  it('reports that a person is already a manager instead of claiming success', async () => {
    const { wrapper, body, api } = await mountAs(447)
    vi.mocked(api.addTeamAccountManager).mockResolvedValue({ data: { code: 0, data: { added: false } } } as any)
    const success = vi.spyOn(ElMessage, 'success')
    const picker = wrapper.findComponent(TeamAccountUserPicker)
    picker.vm.$emit('update:modelValue', 448)
    await flushPromises()
    await body.get('[data-testid="team-account-add-manager"]').trigger('click')
    await flushPromises()

    expect(api.addTeamAccountManager).toHaveBeenCalledWith(6, 448)
    expect(success).toHaveBeenCalledWith('该用户已经是管理员')
  })

  it('keeps the one-time key secret when a refresh fails', async () => {
    const { body, api } = await mountAs(447)
    vi.mocked(api.createTeamAccountKey).mockResolvedValue({
      data: { code: 0, data: { key: { id: 1, name: 'ci', group_id: 10 }, secret: 'sk-secret' } },
    } as any)
    vi.mocked(api.getTeamAccount).mockRejectedValueOnce(new Error('boom'))

    ;(body.get('[data-testid="team-account-key-name"]').element as HTMLInputElement).value = 'ci'
    await body.get('[data-testid="team-account-key-name"]').trigger('input')
    await body.get('[data-testid="team-account-create-key"]').trigger('click')
    await flushPromises()
    expect(body.text()).toContain('sk-secret')

    await body.get('[data-testid="team-account-detail-refresh"]').trigger('click')
    await flushPromises()
    expect(body.text()).toContain('sk-secret')
  })
})
