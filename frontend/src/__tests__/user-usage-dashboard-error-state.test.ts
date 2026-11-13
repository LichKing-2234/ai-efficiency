import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import { setLocale } from '@/i18n'
import {
  getUserUsageDashboard,
  getUserUsageGroupPoolUsage,
  getUserUsageGroupQuotas,
} from '@/api/userUsage'

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(),
  getUserUsageGroupQuotas: vi.fn(),
  getUserUsageGroupPoolUsage: vi.fn(),
  getUserKiroUsage: vi.fn(() => Promise.resolve({ data: { data: null } })),
}))

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageSubjectDashboard: vi.fn(),
  updateTeamUsageRateMultiplier: vi.fn(),
}))

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetOptions: vi.fn(),
  createQuotaResetRequest: vi.fn(),
}))

function quotaResponse() {
  return {
    data: {
      data: {
        group_quotas: { status: 'empty', unit_label: 'USD', groups: [] },
        quota_freshness: { as_of: '2026-07-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' },
      },
    },
  }
}

function poolResponse() {
  return {
    data: {
      data: {
        group_pool_usage: { status: 'empty', groups: [] },
        pool_usage_freshness: { as_of: null, cache_status: 'uncached', source_status: 'ok' },
      },
    },
  }
}

const configuredSnapshot = {
  configured: true,
  range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
  stats: null,
  trend: [],
  models: [],
}

function httpError(status: number) {
  return { response: { status } }
}

function createTestRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/usage', name: 'Usage', component: UserUsageDashboard },
      { path: '/user', name: 'User', component: { template: '<div data-testid="user-setup-page">User</div>' } },
    ],
  })
  return router
}

async function mountDashboard() {
  const router = createTestRouter()
  await router.push('/usage')
  await router.isReady()
  const wrapper = mount(UserUsageDashboard, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}

describe('UserUsageDashboard failure states', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    await setLocale('en-US')
    ;(getUserUsageDashboard as any).mockReset()
    ;(getUserUsageGroupQuotas as any).mockReset().mockResolvedValue(quotaResponse())
    ;(getUserUsageGroupPoolUsage as any).mockReset().mockResolvedValue(poolResponse())
  })

  it('shows transient upstream copy and retries without asking about AI setup for a 502', async () => {
    ;(getUserUsageDashboard as any)
      .mockRejectedValueOnce(httpError(502))
      .mockResolvedValueOnce({ data: { data: configuredSnapshot } })

    const wrapper = await mountDashboard()

    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Usage service is temporarily busy')
    expect(wrapper.text()).toContain('The upstream usage service timed out. Please try again later.')
    expect(wrapper.text()).not.toContain('Usage dashboard is temporarily unavailable')
    expect(wrapper.text()).not.toContain('Try refreshing after checking your setup.')
    expect(wrapper.text()).not.toContain('Relay credentials need attention')
    expect(wrapper.find('a[href="/user"]').exists()).toBe(false)

    const retry = wrapper.get('[data-testid="usage-error-retry"]')
    expect(retry.text()).toBe('Retry')
    await retry.trigger('click')
    await flushPromises()

    expect(getUserUsageDashboard).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('Usage service is temporarily busy')
    expect(wrapper.text()).not.toContain('The upstream usage service timed out. Please try again later.')
  })

  it('keeps the credential copy and setup link for a 409 without a retry button', async () => {
    ;(getUserUsageDashboard as any).mockRejectedValue(httpError(409))

    const wrapper = await mountDashboard()

    expect(wrapper.text()).toContain('Relay credentials need attention')
    expect(wrapper.text()).toContain('Try refreshing after checking your setup.')
    expect(wrapper.text()).not.toContain('Usage service is temporarily busy')
    const setupLink = wrapper.get('a[href="/user"]')
    expect(setupLink.text()).toBe('Open AI Setup')
    expect(wrapper.find('[data-testid="usage-error-retry"]').exists()).toBe(false)
  })

  it('uses neutral non-configuration copy with retry for a 422', async () => {
    ;(getUserUsageDashboard as any).mockRejectedValue(httpError(422))

    const wrapper = await mountDashboard()

    expect(wrapper.text()).toContain('Usage dashboard is temporarily unavailable')
    expect(wrapper.text()).toContain('Please try again later. If the problem persists, contact an administrator.')
    expect(wrapper.text()).not.toContain('Try refreshing after checking your setup.')
    expect(wrapper.text()).not.toContain('Usage service is temporarily busy')
    expect(wrapper.find('[data-testid="usage-error-retry"]').exists()).toBe(true)
  })

  it('renders the transient copy in Chinese with a 重试 action', async () => {
    await setLocale('zh-CN')
    ;(getUserUsageDashboard as any).mockRejectedValue(httpError(502))

    const wrapper = await mountDashboard()

    expect(wrapper.text()).toContain('用量服务暂时繁忙')
    expect(wrapper.text()).toContain('上游用量服务响应超时，请稍后重试。')
    expect(wrapper.get('[data-testid="usage-error-retry"]').text()).toBe('重试')
  })

  it('does not tell a member route to check personal AI setup on a transient failure', async () => {
    const { getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getTeamUsageSubjectDashboard as any).mockRejectedValue(httpError(502))

    const memberRouter = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/usage/members/:user_id', name: 'UsageMember', component: UserUsageDashboard },
        { path: '/usage/team', name: 'UsageTeam', component: { template: '<div>Team</div>' } },
        { path: '/user', name: 'User', component: { template: '<div>User</div>' } },
      ],
    })
    await memberRouter.push('/usage/members/42')
    await memberRouter.isReady()
    const wrapper = mount(UserUsageDashboard, {
      props: { memberRoute: true, embedded: true },
      global: { plugins: [memberRouter] },
    })
    await flushPromises()

    expect(getUserUsageDashboard).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Usage service is temporarily busy')
    expect(wrapper.text()).not.toContain('Usage dashboard is temporarily unavailable')
    expect(wrapper.find('[data-testid="usage-error-retry"]').exists()).toBe(true)
  })
})
