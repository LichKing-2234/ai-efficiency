import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElMessage, ElMessageBox } from 'element-plus'
import TeamAccountSection from '@/components/team-accounts/TeamAccountSection.vue'
import { setLocale } from '@/i18n'
import { cleanupTeleportedContent, withTeleportedContent } from './helpers/teleport'
import type {
  TeamAccountDetail,
  TeamAccountEvent,
  TeamAccountSummary,
  TeamAccountUsage,
} from '@/types'

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageScope: vi.fn(),
}))

vi.mock('@/api/teamAccounts', () => ({
  listTeamAccounts: vi.fn(),
  createTeamAccount: vi.fn(),
  getTeamAccount: vi.fn(),
  revealTeamAccountCredentials: vi.fn(),
  createTeamAccountKey: vi.fn(),
  revokeTeamAccountKey: vi.fn(),
  addTeamAccountManager: vi.fn(),
  removeTeamAccountManager: vi.fn(),
  transferTeamAccount: vi.fn(),
  getTeamAccountUsage: vi.fn(),
  listTeamAccountEvents: vi.fn(),
  listAdminTeamAccounts: vi.fn(),
  createAdminTeamAccount: vi.fn(),
  getAdminTeamAccount: vi.fn(),
  revealAdminTeamAccountCredentials: vi.fn(),
  createAdminTeamAccountKey: vi.fn(),
  revokeAdminTeamAccountKey: vi.fn(),
  addAdminTeamAccountManager: vi.fn(),
  removeAdminTeamAccountManager: vi.fn(),
  transferAdminTeamAccount: vi.fn(),
  getAdminTeamAccountUsage: vi.fn(),
  listAdminTeamAccountEvents: vi.fn(),
  setAdminTeamAccountStatus: vi.fn(),
  listTeamAccountDepartmentGroups: vi.fn(),
  upsertTeamAccountDepartmentGroup: vi.fn(),
  deleteTeamAccountDepartmentGroup: vi.fn(),
}))

const messageSuccess = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as any)
const messageError = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as any)
const messageBoxConfirm = vi.spyOn(ElMessageBox, 'confirm')

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

const account: TeamAccountSummary = {
  id: 7,
  name: 'Data Platform',
  department_external_id: 'dept-data',
  department_display_path: 'Engineering / Data Platform',
  relay_username: 'dept-data-platform',
  relay_user_id: 41,
  owner_user_id: 3,
  status: 'active',
  created_at: '2026-10-01T08:00:00Z',
}

const key = {
  id: 11,
  relay_api_key_id: 51,
  relay_group_id: 9,
  name: 'CI pipeline',
  secret_preview: 'sk-abc...wxyz',
  status: 'active',
  created_at: '2026-10-02T08:00:00Z',
}

const manager = {
  user_id: 3,
  username: 'alice',
  email: 'alice@example.com',
  role: 'owner',
}

function detail(overrides: Partial<TeamAccountDetail> = {}): TeamAccountDetail {
  return {
    ...account,
    keys: [key],
    managers: [manager],
    ...overrides,
  }
}

function usage(): TeamAccountUsage {
  return {
    total_tokens: 1200,
    total_cost: 1.5,
    days: [{ date: '2026-10-02', tokens: 1200, cost: 1.5 }],
    keys: [{ id: 11, name: 'CI pipeline', relay_group_id: 9, tokens: 1200, cost: 1.5 }],
  }
}

function events(): TeamAccountEvent[] {
  return [
    {
      id: 1,
      action: 'key_created',
      actor_user_id: 3,
      payload: {},
      created_at: '2026-10-02T08:00:00Z',
    },
  ]
}

function response<T>(data: T) {
  return { data: { code: 0, data } }
}

async function mockApi(representative = true, items: TeamAccountSummary[] = [account]) {
  const teamUsage = await import('@/api/teamUsage')
  vi.mocked(teamUsage.getTeamUsageScope).mockResolvedValue({
    data: {
      code: 0,
      data: {
        is_representative: representative,
        departments: representative
          ? [{
            external_id: 'dept-data',
            name: 'Data Platform',
            display_path: 'Engineering / Data Platform',
            subtree_member_count: 4,
            matched_user_count: 4,
          }]
          : [],
      },
    },
  } as any)
  const api = await import('@/api/teamAccounts')
  vi.mocked(api.listTeamAccounts).mockResolvedValue(response({ items }) as any)
  vi.mocked(api.getTeamAccount).mockResolvedValue(response(detail()) as any)
  vi.mocked(api.revealTeamAccountCredentials).mockResolvedValue(response({
    username: 'dept-data-platform',
    email: 'dept-data-platform@invalid.account',
    password: 'test-password',
  }) as any)
  vi.mocked(api.getTeamAccountUsage).mockResolvedValue(response(usage()) as any)
  vi.mocked(api.listTeamAccountEvents).mockResolvedValue(response({ items: events() }) as any)
  vi.mocked(api.revokeTeamAccountKey).mockResolvedValue(response({ revoked: true }) as any)
  vi.mocked(api.createTeamAccountKey).mockResolvedValue(response({ key, secret: 'sk-new-secret' }) as any)
  vi.mocked(api.addTeamAccountManager).mockResolvedValue(response({ added: true }) as any)
  vi.mocked(api.removeTeamAccountManager).mockResolvedValue(response({ removed: true }) as any)
  vi.mocked(api.transferTeamAccount).mockResolvedValue(response({ transferred: true }) as any)
  vi.mocked(api.createTeamAccount).mockResolvedValue(response(account) as any)
  return api
}

async function mountSection(representative = true, items: TeamAccountSummary[] = [account]) {
  const api = await mockApi(representative, items)
  const wrapper = withTeleportedContent(mount(TeamAccountSection))
  await flushPromises()
  return { wrapper, api }
}

async function openFirstCard(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[data-testid="team-account-card-7"]').trigger('click')
  await flushPromises()
}

describe('TeamAccountSection', () => {
  afterEach(() => {
    cleanupTeleportedContent()
  })

  beforeEach(() => {
    vi.clearAllMocks()
    messageBoxConfirm.mockResolvedValue('confirm' as any)
    void setLocale('en-US')
  })

  it('renders account cards from the API', async () => {
    const { wrapper, api } = await mountSection()

    expect(api.listTeamAccounts).toHaveBeenCalledTimes(1)
    const card = wrapper.get('[data-testid="team-account-card-7"]')
    expect(card.text()).toContain('Data Platform')
    expect(card.text()).toContain('Engineering / Data Platform')
    expect(card.text()).toContain('Active')
    expect(card.text()).toContain('Keys')
  })

  it('only fetches credentials after the explicit reveal click', async () => {
    const { wrapper, api } = await mountSection()

    expect(api.revealTeamAccountCredentials).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="team-account-password"]').exists()).toBe(false)

    await openFirstCard(wrapper)

    expect(api.getTeamAccount).toHaveBeenCalledWith(7)
    expect(api.revealTeamAccountCredentials).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="team-account-reveal-credentials"]').trigger('click')
    await flushPromises()

    expect(api.revealTeamAccountCredentials).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-testid="team-account-password"]').text()).toBe('test-password')
  })

  it('creates an account for the representative department without asking for one', async () => {
    const { wrapper, api } = await mountSection()

    await wrapper.get('[data-testid="team-account-create-open"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="team-account-create-department"]').exists()).toBe(false)

    await wrapper.get('[data-testid="team-account-create-name"]').setValue('New Account')
    await wrapper.get('[data-testid="team-account-create-submit"]').trigger('click')
    await flushPromises()

    expect(api.createTeamAccount).toHaveBeenCalledWith({
      name: 'New Account',
      department_external_id: 'dept-data',
    })
  })

  it('hides the create action for users who are not representatives', async () => {
    const { wrapper } = await mountSection(false)

    expect(wrapper.find('[data-testid="team-account-create-open"]').exists()).toBe(false)
  })

  it('hides the whole section when the user has neither a role nor a department', async () => {
    const { wrapper } = await mountSection(false, [])

    expect(wrapper.find('[data-testid="team-account-section"]').exists()).toBe(false)
  })

  it('offers a one-click sign in on the card', async () => {
    const { wrapper } = await mountSection()
    const button = wrapper.get('[data-testid="team-account-login-as-7"]')
    expect(button.text()).toContain('Sign in as this account')
  })

  it('revokes a key through the API after confirmation', async () => {
    const { wrapper, api } = await mountSection()

    await openFirstCard(wrapper)
    await wrapper.get('[data-testid="team-account-revoke-key-11"]').trigger('click')
    await flushPromises()

    expect(messageBoxConfirm).toHaveBeenCalledTimes(1)
    expect(api.revokeTeamAccountKey).toHaveBeenCalledWith(7, 11)
    expect(messageSuccess).toHaveBeenCalled()
  })
})
