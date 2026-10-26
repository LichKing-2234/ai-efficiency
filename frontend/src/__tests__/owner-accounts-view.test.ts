import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import OwnerAccountsView from '@/views/OwnerAccountsView.vue'
import { getTeamUsageScope } from '@/api/teamUsage'
import {
  createOwnerAccountInvitation,
  createOwnerAccountReset,
  getOwnerAccountOperation,
  getOwnerAccountScope,
  getOwnerAccountUsage,
  listOwnerAccountOperations,
  listOwnerAccounts,
  refreshOwnerAccountQuota,
  refreshOwnerAccountReferral,
  refreshOwnerAccountUsage,
} from '@/api/ownerAccounts'
import OwnerAccountResetDialog from '@/components/owner-accounts/OwnerAccountResetDialog.vue'
import OwnerAccountInviteDialog from '@/components/owner-accounts/OwnerAccountInviteDialog.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/teamUsage', () => ({ getTeamUsageScope: vi.fn() }))
vi.mock('@/api/ownerAccounts', () => ({
  getOwnerAccountScope: vi.fn(),
  listOwnerAccounts: vi.fn(),
  getOwnerAccountUsage: vi.fn(),
  refreshOwnerAccountUsage: vi.fn(),
  refreshOwnerAccountQuota: vi.fn(),
  refreshOwnerAccountReferral: vi.fn(),
  createOwnerAccountReset: vi.fn(),
  createOwnerAccountInvitation: vi.fn(),
  getOwnerAccountOperation: vi.fn(),
  listOwnerAccountOperations: vi.fn(),
}))
vi.mock('@/components/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' },
}))

const mountedWrappers: Array<{ unmount: () => void }> = []

const scopeWithAccounts = { data: { data: { account_count: 1, mutations_enabled: true } } }
const scopeWithoutAccounts = { data: { data: { account_count: 0, mutations_enabled: true } } }

function createHarness(initialPath = '/usage') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/usage',
        name: 'Usage',
        component: DashboardView,
        children: [
          { path: 'team', name: 'UsageTeam', component: { template: '<div data-testid="team-content">Team</div>' } },
          { path: 'quota-reset', name: 'UsageQuotaReset', component: { template: '<div data-testid="reset-content">Reset</div>' } },
          { path: 'accounts', name: 'usage-accounts', component: OwnerAccountsView },
        ],
      },
    ],
  })
  return router.push(initialPath).then(() => router.isReady()).then(() => {
    const wrapper = mount(
      { template: '<RouterView />', components: { RouterView } },
      {
        global: {
          plugins: [router],
          stubs: {
            teleport: true,
            ElPagination: {
              props: ['currentPage', 'pageSize', 'total', 'pageSizes', 'disabled'],
              emits: ['current-change', 'size-change'],
              template: '<div v-bind="$attrs"><button data-testid="pagination-next" @click="$emit(\'current-change\', currentPage + 1)"></button><button data-testid="pagination-size" @click="$emit(\'size-change\', 100)"></button></div>',
            },
          },
        },
      },
    )
    mountedWrappers.push(wrapper)
    return { router, wrapper }
  })
}

function installMatchMedia(matches: boolean) {
  const mediaQuery = {
    matches,
    media: '(min-width: 1280px)',
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  }
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn(() => mediaQuery) })
}

afterEach(() => {
  while (mountedWrappers.length) mountedWrappers.pop()!.unmount()
  document.body.innerHTML = ''
  Reflect.deleteProperty(window, 'matchMedia')
})

describe('owner account console tab visibility', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue({
      data: { data: { items: [], total: 0, page: 1, page_size: 20, mutations_enabled: true } },
    } as any)
  })

  it('shows the owner accounts tab when the scope count is positive', async () => {
    const { wrapper } = await createHarness()
    await flushPromises()

    expect(wrapper.text()).toContain('My Accounts')
  })

  it('hides the owner accounts tab when the scope count is zero', async () => {
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithoutAccounts as any)
    const { wrapper } = await createHarness()
    await flushPromises()

    expect(wrapper.text()).not.toContain('My Accounts')
  })

  it('hides the owner accounts tab without an error banner when the scope probe fails', async () => {
    vi.mocked(getOwnerAccountScope).mockRejectedValue(new Error('synthetic scope failure'))
    const { wrapper } = await createHarness()
    await flushPromises()

    expect(wrapper.text()).not.toContain('My Accounts')
    expect(wrapper.find('[data-testid="owner-accounts-scope-error"]').exists()).toBe(false)
  })

  it('renders an empty state on direct navigation without redirecting', async () => {
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithoutAccounts as any)
    const { router, wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('usage-accounts')
    expect(wrapper.find('[data-testid="owner-accounts-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="usage-center-banner"]').exists()).toBe(true)
  })
})

function ownerAccount(id: number, overrides: Record<string, unknown> = {}) {
  return {
    id,
    name: `Account ${id}`,
    email: `alice${id}@example.com`,
    platform: 'openai',
    type: 'oauth',
    status: 'active',
    group_names: ['Group Alpha'],
    quota: {
      reset_credits_remaining: 2,
      reset_credit_expiries: ['2026-11-01T00:00:00Z'],
      codex_credits_unlimited: false,
      codex_credits_balance: 120,
      auto_reset_credit: false,
      observed_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
    },
    referral: {
      eligible: true,
      program_id: 'program-alpha',
      program_name: 'Program Alpha',
      remaining_invites: 3,
      consent_required: true,
      observed_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
    },
    health: {
      schedulable: true,
      reauth_required: false,
      banned: false,
      forbidden: false,
      rate_limit_reset_at: null,
    },
    blocked_actions: [],
    ...overrides,
  }
}

function accountPage(items: ReturnType<typeof ownerAccount>[], total = items.length, page = 1, pageSize = 20) {
  return { data: { data: { items, total, page, page_size: pageSize, mutations_enabled: true } } }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

describe('owner account list and pagination', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(1)]) as any)
  })

  it('shows a loading state before the first page arrives', async () => {
    const pending = deferred<any>()
    vi.mocked(listOwnerAccounts).mockReturnValue(pending.promise)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-accounts-loading"]').exists()).toBe(true)

    pending.resolve(accountPage([ownerAccount(1)]))
    await flushPromises()
    expect(wrapper.find('[data-testid="owner-accounts-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="owner-account-1"]').exists()).toBe(true)
  })

  it('renders account identity, groups and a visible page range', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(7)], 3, 1, 1) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    const account = wrapper.get('[data-testid="owner-account-7"]')
    expect(account.text()).toContain('Account 7')
    expect(account.text()).toContain('alice7@example.com')
    expect(account.text()).toContain('Group Alpha')
    expect(wrapper.text()).toContain('Showing 1-1 of 3')
  })

  it('restores page and page size from the URL and reloads on page change', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(9)], 200, 2, 50) as any)
    const { router, wrapper } = await createHarness('/usage/accounts?page=2&page_size=50')
    await flushPromises()

    expect(listOwnerAccounts).toHaveBeenCalledWith({ page: 2, page_size: 50 })
    expect(wrapper.find('[data-testid="owner-account-9"]').exists()).toBe(true)

    await wrapper.get('[data-testid="pagination-next"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.query).toMatchObject({ page: '3', page_size: '50' })
    expect(listOwnerAccounts).toHaveBeenLastCalledWith({ page: 3, page_size: 50 })
  })

  it('resets to page 1 when the page size changes', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(9)], 200, 2, 20) as any)
    const { router, wrapper } = await createHarness('/usage/accounts?page=2')
    await flushPromises()

    await wrapper.get('[data-testid="pagination-size"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.query).toEqual({ page_size: '100' })
    expect(listOwnerAccounts).toHaveBeenLastCalledWith({ page: 1, page_size: 100 })
  })

  it('omits navigation for a single page of results', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(1)], 1) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-accounts-pagination"]').exists()).toBe(false)
  })
})

const usagePayload = {
  five_hour: {
    utilization_percent: 42,
    resets_at: '2026-10-23T14:30:00Z',
    requests: 12,
    tokens: 3456,
    api_cost_usd: 1.25,
    standard_cost_usd: 9.99,
    user_cost_usd: 2.5,
  },
  seven_day: {
    utilization_percent: 68,
    resets_at: '2026-10-26T12:00:00Z',
    requests: 98,
    tokens: 987654,
    api_cost_usd: 7.5,
    standard_cost_usd: 59.99,
    user_cost_usd: 15,
  },
  observed_at: '2026-10-23T11:55:00Z',
}

function usageBatch(items: Array<{ account_id: number; usage: unknown }>, errors: Array<{ account_id: number; code: string }> = []) {
  return { data: { data: { items, errors } } }
}

function axiosError(status: number, details: Record<string, unknown> = {}) {
  return { response: { status, data: { details } } }
}

describe('owner account sections', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] })
    vi.setSystemTime(new Date('2026-10-23T12:00:00Z'))
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(1)]) as any)
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([{ account_id: 1, usage: usagePayload }]) as any)
    vi.mocked(refreshOwnerAccountUsage).mockResolvedValue({ data: { data: { account_id: 1, usage: usagePayload } } } as any)
    vi.mocked(refreshOwnerAccountQuota).mockResolvedValue({ data: { data: { account_id: 1, quota: ownerAccount(1).quota } } } as any)
    vi.mocked(refreshOwnerAccountReferral).mockResolvedValue({ data: { data: { account_id: 1, referral: ownerAccount(1).referral } } } as any)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders 5h and 7d usage with countdown, requests, tokens and A/U costs', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    const usage = wrapper.get('[data-testid="owner-account-usage-1"]')
    expect(usage.text()).toContain('5-hour window')
    expect(usage.text()).toContain('42%')
    expect(usage.text()).toContain('Resets in 2h 30m')
    expect(usage.text()).toContain('12')
    expect(usage.text()).toContain('3.46K')
    expect(usage.text()).toContain('A $')
    expect(usage.text()).toContain('1.25')
    expect(usage.text()).not.toContain('9.99')
    expect(usage.text()).toContain('U $')
    expect(usage.text()).toContain('2.50')
    expect(usage.text()).toContain('Observed')
    const apiCost = wrapper.get('[data-testid="owner-account-usage-api-cost-5h-1"]')
    expect(apiCost.findComponent({ name: 'ElTooltip' }).props('content')).toContain('9.99')
  })

  it('hides A $ instead of substituting standard cost when upstream omits cost', async () => {
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([{
      account_id: 1,
      usage: {
        ...usagePayload,
        five_hour: { ...usagePayload.five_hour, api_cost_usd: null, standard_cost_usd: 9.99 },
      },
    }]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    const cost = wrapper.get('[data-testid="owner-account-usage-api-cost-5h-1"]')
    expect(cost.text()).toBe('-')
    expect(cost.text()).not.toContain('9.99')
  })

  it('renders every reset-credit expiry instead of only the first', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, {
        quota: {
          ...ownerAccount(1).quota,
          reset_credits_remaining: 2,
          reset_credit_expiries: ['2027-01-01T00:00:00Z', '2028-01-01T00:00:00Z'],
        },
      }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    const card = wrapper.get('[data-testid="owner-account-quota-1"]').text()
    expect(card).toContain('2027')
    expect(card).toContain('2028')

    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()
    const dialog = wrapper.get('[data-testid="owner-account-reset-dialog"]').text()
    expect(dialog).toContain('2027')
    expect(dialog).toContain('2028')
  })

  it('blocks the reset dialog when the quota observation is older than 15 minutes', async () => {
    const observed = new Date(Date.now() - 16 * 60 * 1000).toISOString()
    const quota = { ...ownerAccount(1).quota, observed_at: observed }
    const wrapper = mount(OwnerAccountResetDialog, {
      props: {
        open: true,
        account: ownerAccount(1),
        usage: usagePayload,
        quota,
        submitting: false,
      },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-reset-precondition"]').text()).toContain('older than 15 minutes')
    expect(wrapper.get('[data-testid="owner-account-reset-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('maps a compliance acknowledgement failure to a specific message', async () => {
    vi.mocked(createOwnerAccountReset).mockRejectedValue(axiosError(423, { error_code: 'compliance-ack-required' }))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-ack"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-confirm"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-reset-error"]').text()).toContain('compliance acknowledgement')
  })

  it('shows a per-section loading state while usage loads', async () => {
    const pending = deferred<any>()
    vi.mocked(getOwnerAccountUsage).mockReturnValue(pending.promise)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="owner-account-usage-loading-1"]').exists()).toBe(true)

    pending.resolve(usageBatch([{ account_id: 1, usage: usagePayload }]))
    await flushPromises()
    expect(wrapper.find('[data-testid="owner-account-usage-loading-1"]').exists()).toBe(false)
  })

  it('keeps quota and referral facts when only the usage section fails', async () => {
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([], [{ account_id: 1, code: 'upstream-unavailable' }]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-usage-error-1"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-quota-1"]').text()).toContain('2 remaining')
    expect(wrapper.get('[data-testid="owner-account-referral-1"]').text()).toContain('3 invites remaining')
  })

  it('keeps the account list when the whole usage batch fails', async () => {
    vi.mocked(getOwnerAccountUsage).mockRejectedValue(new Error('synthetic usage failure'))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="owner-account-usage-error-1"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-quota-1"]').text()).toContain('Codex credits')
  })

  it('shows remaining reset credits, Codex points and referral capacity', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    const quota = wrapper.get('[data-testid="owner-account-quota-1"]')
    expect(quota.text()).toContain('2 remaining')
    expect(quota.text()).toContain('2026')
    expect(quota.text()).toContain('120')
    expect(quota.text()).toContain('Auto reset disabled')
    const referral = wrapper.get('[data-testid="owner-account-referral-1"]')
    expect(referral.text()).toContain('Eligible to invite')
    expect(referral.text()).toContain('Program Alpha')
    expect(referral.text()).toContain('3 invites remaining')
  })

  it('keeps health facts collapsed until requested', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-health-1"]').exists()).toBe(false)
    await wrapper.get('[data-testid="owner-account-health-toggle-1"]').trigger('click')

    const health = wrapper.get('[data-testid="owner-account-health-1"]')
    expect(health.text()).toContain('Schedulable')
    expect(health.text()).toContain('Re-authentication required')
  })

  it('blocks account actions when mutations are disabled by the platform', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue({
      data: { data: { items: [ownerAccount(1)], total: 1, page: 1, page_size: 20, mutations_enabled: false } },
    } as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-use-reset-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="owner-account-invite-1"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Account actions are disabled by the platform.')
  })

  it('queries usage, quota and referral for one account and reloads its facts', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    vi.mocked(listOwnerAccounts).mockClear()

    await wrapper.get('[data-testid="owner-account-query-1"]').trigger('click')
    await flushPromises()

    expect(refreshOwnerAccountUsage).toHaveBeenCalledWith(1)
    expect(refreshOwnerAccountQuota).toHaveBeenCalledWith(1)
    expect(refreshOwnerAccountReferral).toHaveBeenCalledWith(1)
    expect(listOwnerAccounts).toHaveBeenCalledTimes(1)
  })

  it('shows a cooldown for the failed section without hiding the others', async () => {
    vi.mocked(refreshOwnerAccountUsage).mockRejectedValue(axiosError(429, { error_code: 'refresh-cooldown', retry_after_seconds: 30 }))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-query-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-cooldown-1"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-quota-1"]').text()).toContain('2 remaining')
    expect(wrapper.get('[data-testid="owner-account-query-1"]').attributes('disabled')).toBeDefined()
  })

  it('shows a quota-section loading state while the query is running', async () => {
    const pending = deferred<any>()
    vi.mocked(refreshOwnerAccountQuota).mockReturnValue(pending.promise)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-query-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-quota-loading-1"]').exists()).toBe(true)

    pending.resolve({ data: { data: { account_id: 1, quota: ownerAccount(1).quota } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="owner-account-quota-loading-1"]').exists()).toBe(false)
  })

  it('disables the query action while the account cooldown from the server is active', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, { refresh_cooldown_until: '2026-10-23T12:01:00Z' }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-query-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="owner-account-cooldown-1"]').exists()).toBe(true)
  })
})

describe('reset credit flow', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] })
    vi.setSystemTime(new Date('2026-10-23T12:00:00Z'))
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(1)] ) as any)
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([{ account_id: 1, usage: usagePayload }]) as any)
    vi.mocked(refreshOwnerAccountUsage).mockResolvedValue({ data: { data: { account_id: 1, usage: usagePayload } } } as any)
    vi.mocked(refreshOwnerAccountQuota).mockResolvedValue({ data: { data: { account_id: 1, quota: ownerAccount(1).quota } } } as any)
    vi.mocked(refreshOwnerAccountReferral).mockResolvedValue({ data: { data: { account_id: 1, referral: ownerAccount(1).referral } } } as any)
    vi.mocked(createOwnerAccountReset).mockResolvedValue({ data: { data: { operation_id: 77 } } } as any)
    vi.mocked(getOwnerAccountOperation).mockResolvedValue({
      data: { data: { id: 77, account_id: 1, action: 'reset_credit', status: 'succeeded', result: { windows_reset: 2 }, started_at: '2026-10-23T12:00:00Z' } },
    } as any)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('disables the reset action without unexpired credits and explains why', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, {
        quota: { ...ownerAccount(1).quota, reset_credits_remaining: 0 },
      }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="owner-account-reset-hint-1"]').text()).toContain('No unexpired reset credit')
  })

  it('disables the reset action until a fresh quota observation exists', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, { quota: { ...ownerAccount(1).quota, observed_at: null } }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="owner-account-reset-hint-1"]').text()).toContain('Query this account')
  })

  it('shows the dialog preconditions and blocks confirmation until acknowledged', async () => {
    const wrapper = mount(OwnerAccountResetDialog, {
      props: {
        open: true,
        account: ownerAccount(1, { quota: { ...ownerAccount(1).quota, reset_credits_remaining: 0 } }),
        usage: usagePayload,
        quota: { ...ownerAccount(1).quota, reset_credits_remaining: 0 },
        submitting: false,
      },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-reset-precondition"]').text()).toContain('at least one unexpired credit')
    expect(wrapper.get('[data-testid="owner-account-reset-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('shows observation, expiry, auto-reset caveat and irreversible warning, then submits', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValueOnce(accountPage([
      ownerAccount(1, { quota: { ...ownerAccount(1).quota, auto_reset_credit: true } }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()

    const dialog = wrapper.get('[data-testid="owner-account-reset-dialog"]')
    expect(dialog.text()).toContain('Quota observed')
    expect(dialog.text()).toContain('Credit expiry')
    expect(dialog.text()).toContain('2026')
    expect(dialog.text()).toContain('Auto reset is enabled')
    expect(dialog.text()).toContain('cannot be undone')
    expect(wrapper.get('[data-testid="owner-account-reset-confirm"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="owner-account-reset-ack"] input').setValue(true)
    await flushPromises()
    expect(wrapper.get('[data-testid="owner-account-reset-confirm"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="owner-account-reset-confirm"]').trigger('click')
    await flushPromises()

    expect(createOwnerAccountReset).toHaveBeenCalledWith(1, { confirm: true })
    expect(wrapper.find('[data-testid="owner-account-reset-dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="owner-account-operation-1"]').exists()).toBe(true)
  })

  it('polls the reset operation and reports how many windows were reset', async () => {
    vi.mocked(getOwnerAccountOperation)
      .mockResolvedValueOnce({
        data: { data: { id: 77, account_id: 1, action: 'reset_credit', status: 'running', started_at: '2026-10-23T12:00:00Z' } },
      } as any)
      .mockResolvedValueOnce({
        data: { data: { id: 77, account_id: 1, action: 'reset_credit', status: 'succeeded', result: { windows_reset: 2 }, started_at: '2026-10-23T12:00:00Z' } },
      } as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-ack"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-confirm"]').trigger('click')
    await flushPromises()

    expect(getOwnerAccountOperation).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(getOwnerAccountOperation).toHaveBeenCalledWith(77)
    expect(wrapper.get('[data-testid="owner-account-operation-1"]').text()).toContain('Operation in progress')

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-operation-1"]').text()).toContain('2 window(s) reset')
    expect(wrapper.find('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeUndefined()
  })

  it('shows a persistent unknown banner and blocks reset until a successful query', async () => {
    vi.mocked(getOwnerAccountOperation).mockResolvedValue({
      data: { data: { id: 77, account_id: 1, action: 'reset_credit', status: 'unknown', started_at: '2026-10-23T12:00:00Z' } },
    } as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-ack"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-confirm"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-unknown-1"]').text()).toContain('may have consumed')
    expect(wrapper.get('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="owner-account-query-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-unknown-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeUndefined()
  })

  it('shows a non-blocking interrupted state instead of a fabricated unknown outcome', async () => {
    vi.mocked(getOwnerAccountOperation).mockResolvedValue({
      data: { data: { id: 77, account_id: 1, action: 'reset_credit', status: 'running', started_at: '2026-10-23T12:00:00Z' } },
    } as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-ack"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-confirm"]').trigger('click')
    await flushPromises()

    await vi.advanceTimersByTimeAsync(90000)
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-unknown-1"]').exists()).toBe(false)
    const interrupted = wrapper.get('[data-testid="owner-account-poll-interrupted-1"]')
    expect(interrupted.text()).toContain('not blocked')
    expect(vi.mocked(getOwnerAccountOperation).mock.calls.length).toBeLessThanOrEqual(46)
    // The server never said unknown, so the action is not blocked.
    expect(wrapper.get('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeUndefined()

    // Retrying polling recovers the terminal result without a page reload.
    vi.mocked(getOwnerAccountOperation).mockResolvedValue({
      data: { data: { id: 77, account_id: 1, action: 'reset_credit', status: 'succeeded', result: { windows_reset: 2 }, started_at: '2026-10-23T12:00:00Z' } },
    } as any)
    await wrapper.get('[data-testid="owner-account-retry-poll-1"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-operation-1"]').text()).toContain('2 window(s) reset')
    expect(wrapper.find('[data-testid="owner-account-poll-interrupted-1"]').exists()).toBe(false)
  })

  it('treats a poll error as interrupted rather than a blocked unknown outcome', async () => {
    vi.mocked(getOwnerAccountOperation).mockRejectedValue(new Error('network down'))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-use-reset-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-ack"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-reset-confirm"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-unknown-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="owner-account-poll-interrupted-1"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="owner-account-history-1"]').exists()).toBe(true)
  })
})

function inviteReferral(overrides: Record<string, unknown> = {}) {
  return {
    eligible: true,
    program_id: 'program-alpha',
    program_name: 'Program Alpha',
    remaining_invites: 3,
    consent_required: true,
    observed_at: new Date(Date.now() - 2 * 60 * 1000).toISOString(),
    ...overrides,
  }
}

describe('invitation flow', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] })
    vi.setSystemTime(new Date('2026-10-23T12:00:00Z'))
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, { referral: inviteReferral() }),
    ]) as any)
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([{ account_id: 1, usage: usagePayload }]) as any)
    vi.mocked(refreshOwnerAccountUsage).mockResolvedValue({ data: { data: { account_id: 1, usage: usagePayload } } } as any)
    vi.mocked(refreshOwnerAccountQuota).mockResolvedValue({ data: { data: { account_id: 1, quota: ownerAccount(1).quota } } } as any)
    vi.mocked(refreshOwnerAccountReferral).mockResolvedValue({ data: { data: { account_id: 1, referral: inviteReferral() } } } as any)
    vi.mocked(createOwnerAccountInvitation).mockResolvedValue({ data: { data: { operation_id: 88 } } } as any)
    vi.mocked(getOwnerAccountOperation).mockResolvedValue({
      data: { data: { id: 88, account_id: 1, action: 'send_invitation', status: 'succeeded', result: { sent: true }, started_at: '2026-10-23T12:00:00Z' } },
    } as any)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows program, capacity, identity notice, irreversible warning and consent', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-invite-1"]').trigger('click')
    await flushPromises()

    const dialog = wrapper.get('[data-testid="owner-account-invite-dialog"]')
    expect(dialog.text()).toContain('Program Alpha')
    expect(dialog.text()).toContain('3 invitations remaining')
    expect(dialog.text()).toContain('recipient sees the inviting account')
    expect(dialog.text()).toContain('cannot be revoked')
    expect(wrapper.find('[data-testid="owner-account-invite-consent"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-invite-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('requires consent when the referral snapshot omits confirmation data', async () => {
    const wrapper = mount(OwnerAccountInviteDialog, {
      props: {
        open: true,
        account: ownerAccount(1),
        referral: inviteReferral({ consent_required: null }),
        submitting: false,
      },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-invite-consent"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-invite-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('does not ask for consent when the referral snapshot explicitly reports no consent requirement', async () => {
    const wrapper = mount(OwnerAccountInviteDialog, {
      props: {
        open: true,
        account: ownerAccount(1),
        referral: inviteReferral({ consent_required: false }),
        submitting: false,
      },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-invite-consent"]').exists()).toBe(false)
    await wrapper.get('[data-testid="owner-account-invite-email"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="owner-account-invite-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('confirm')?.[0]).toEqual([{
      email: 'bob@example.org',
      program_id: 'program-alpha',
      consent: true,
    }])
  })

  it('rejects an invalid email without calling the API', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-email"]').setValue('not-an-email')
    await wrapper.get('[data-testid="owner-account-invite-consent"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-confirm"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-invite-error"]').text()).toContain('valid email')
    expect(createOwnerAccountInvitation).not.toHaveBeenCalled()
  })

  it('submits email, program and consent, then reports the invitation result', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-email"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="owner-account-invite-consent"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-confirm"]').trigger('click')
    await flushPromises()

    expect(createOwnerAccountInvitation).toHaveBeenCalledWith(1, {
      email: 'bob@example.org',
      program_id: 'program-alpha',
      consent: true,
    })
    expect(wrapper.find('[data-testid="owner-account-invite-dialog"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.get('[data-testid="owner-account-operation-1"]').text()).toContain('Invitation sent')
  })

  it('blocks invitations while eligibility or capacity is missing', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, { referral: inviteReferral({ remaining_invites: 0 }) }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-invite-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="owner-account-invite-hint-1"]').text()).toContain('No invitations remain')
  })

  it('treats an unknown invitation outcome as a persistent block until a query succeeds', async () => {
    vi.mocked(getOwnerAccountOperation).mockResolvedValue({
      data: { data: { id: 88, account_id: 1, action: 'send_invitation', status: 'unknown', started_at: '2026-10-23T12:00:00Z' } },
    } as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-email"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="owner-account-invite-consent"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-confirm"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-unknown-1"]').text()).toContain('may have been sent')
    expect(wrapper.get('[data-testid="owner-account-invite-1"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="owner-account-query-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-unknown-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="owner-account-invite-1"]').attributes('disabled')).toBeUndefined()
  })
  it('blocks the invite dialog when referral eligibility is older than 10 minutes', async () => {
    const wrapper = mount(OwnerAccountInviteDialog, {
      props: {
        open: true,
        account: ownerAccount(1),
        referral: inviteReferral({ observed_at: new Date(Date.now() - 11 * 60 * 1000).toISOString() }),
        submitting: false,
      },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-invite-precondition"]').text()).toContain('older than 10 minutes')
    expect(wrapper.get('[data-testid="owner-account-invite-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('maps invite freshness to a specific message', async () => {
    vi.mocked(createOwnerAccountInvitation).mockRejectedValue(axiosError(409, { error_code: 'invite-freshness-required' }))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-email"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="owner-account-invite-consent"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-invite-confirm"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="owner-account-invite-submit-error"]').text()).toContain('Query referral eligibility again')
  })
})

function operation(id: number, overrides: Record<string, unknown> = {}) {
  return {
    id,
    account_id: 1,
    action: 'reset_credit',
    status: 'succeeded',
    result: { windows_reset: 2 },
    warning_code: 'auto_reset_suppressed',
    started_at: '2026-10-23T11:00:00Z',
    finished_at: '2026-10-23T11:00:05Z',
    created_at: '2026-10-23T11:00:00Z',
    ...overrides,
  }
}

function operationPage(items: ReturnType<typeof operation>[], total = items.length, page = 1) {
  return { data: { data: { items, total, page, page_size: 20 } } }
}

describe('operation history drawer', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(1)]) as any)
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([{ account_id: 1, usage: usagePayload }]) as any)
    vi.mocked(listOwnerAccountOperations).mockResolvedValue(operationPage([
      operation(11),
      operation(12, { action: 'send_invitation', status: 'unknown', result: null, invite_email: 'bob@example.org', warning_code: null, error_code: 'invite_unknown' }),
      operation(13, { action: 'refresh_quota', status: 'failed', result: null, warning_code: null, error_code: 'upstream_unavailable' }),
    ]) as any)
  })

  it('opens the history drawer and renders operation facts', async () => {
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-history-1"]').trigger('click')
    await flushPromises()

    expect(listOwnerAccountOperations).toHaveBeenCalledWith(1, { page: 1, page_size: 20 })
    expect(wrapper.find('[data-testid="owner-operation-history-drawer"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-operation-row-11"]').text()).toContain('Reset credit')
    expect(wrapper.get('[data-testid="owner-operation-row-11"]').text()).toContain('Succeeded')
    expect(wrapper.get('[data-testid="owner-operation-row-11"]').text()).toContain('Windows reset: 2')
    expect(wrapper.get('[data-testid="owner-operation-row-11"]').text()).toContain('auto_reset_suppressed')
    expect(wrapper.get('[data-testid="owner-operation-row-12"]').text()).toContain('Recipient: bob@example.org')
    expect(wrapper.get('[data-testid="owner-operation-row-13"]').text()).toContain('Failed')
  })

  it('shows an empty state for an account without operations', async () => {
    vi.mocked(listOwnerAccountOperations).mockResolvedValue(operationPage([]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-history-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-operation-history-empty"]').exists()).toBe(true)
  })

  it('shows a drawer-local failure without closing the history surface', async () => {
    vi.mocked(listOwnerAccountOperations).mockRejectedValue(new Error('synthetic history failure'))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-history-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-operation-history-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="owner-operation-history-drawer"]').exists()).toBe(true)
  })

  it('pages through local history and returns to page 1 when reopened', async () => {
    vi.mocked(listOwnerAccountOperations).mockResolvedValue(operationPage([operation(11)], 45, 1) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-history-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-operation-history-pagination"]').exists()).toBe(true)
    await wrapper.get('[data-testid="owner-operation-history-pagination"] [data-testid="pagination-next"]').trigger('click')
    await flushPromises()
    expect(listOwnerAccountOperations).toHaveBeenLastCalledWith(1, { page: 2, page_size: 20 })

    await wrapper.get('[data-testid="owner-operation-history-close"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="owner-account-history-1"]').trigger('click')
    await flushPromises()
    expect(listOwnerAccountOperations).toHaveBeenLastCalledWith(1, { page: 1, page_size: 20 })
  })
})

describe('responsive composition and server state', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.mocked(getTeamUsageScope).mockResolvedValue({ data: { data: { is_representative: false } } } as any)
    vi.mocked(getOwnerAccountScope).mockResolvedValue(scopeWithAccounts as any)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([ownerAccount(1)]) as any)
    vi.mocked(getOwnerAccountUsage).mockResolvedValue(usageBatch([{ account_id: 1, usage: usagePayload }]) as any)
  })

  it('uses cards below the extra-large breakpoint', async () => {
    installMatchMedia(false)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-accounts-cards"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="owner-accounts-table"]').exists()).toBe(false)
  })

  it('uses a scan-friendly table at the extra-large breakpoint', async () => {
    installMatchMedia(true)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-accounts-table"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="owner-accounts-cards"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="owner-accounts-table"]').text()).toContain('Account 1')
  })

  it('shows reset-credit expiry dates in the wide table', async () => {
    installMatchMedia(true)
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, {
        quota: {
          ...ownerAccount(1).quota,
          reset_credits_remaining: 2,
          reset_credit_expiries: ['2027-01-01T00:00:00Z', '2028-01-01T00:00:00Z'],
        },
      }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    const table = wrapper.get('[data-testid="owner-accounts-table"]').text()
    expect(table).toContain('2027')
    expect(table).toContain('2028')
  })

  it('labels the rate-limit reset row without leaking an interpolation placeholder', async () => {
    installMatchMedia(false)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    await wrapper.get('[data-testid="owner-account-health-toggle-1"]').trigger('click')
    await flushPromises()

    const health = wrapper.get('[data-testid="owner-account-health-1"]').text()
    expect(health).toContain('Rate limit reset')
    expect(health).not.toContain('{time}')
  })

  it('updates the reset countdown as time passes', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-10-23T12:00:00Z'))
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()
    expect(wrapper.get('[data-testid="owner-account-usage-1"]').text()).toContain('Resets in 2h 30m')

    vi.setSystemTime(new Date('2026-10-23T12:30:00Z'))
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(wrapper.get('[data-testid="owner-account-usage-1"]').text()).toContain('Resets in 1h 59m')
    vi.useRealTimers()
  })

  it('honors an unknown reset block reported by the server on first load', async () => {
    vi.mocked(listOwnerAccounts).mockResolvedValue(accountPage([
      ownerAccount(1, {
        blocked_actions: [{ action: 'reset_credit', reason: 'unknown', operation_id: 5 }],
      }),
    ]) as any)
    const { wrapper } = await createHarness('/usage/accounts')
    await flushPromises()

    expect(wrapper.find('[data-testid="owner-account-unknown-1"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="owner-account-use-reset-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="owner-account-history-1"]').exists()).toBe(true)
  })
})

describe('usage accounts route', () => {
  it('registers the account console at /usage/accounts', async () => {
    const { default: router } = await import('@/router')
    expect(router.resolve('/usage/accounts').name).toBe('usage-accounts')
  })
})
