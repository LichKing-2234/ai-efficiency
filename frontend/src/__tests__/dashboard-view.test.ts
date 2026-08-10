import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import { setLocale } from '@/i18n'
import { getUserUsageDashboard, getUserUsageGroupQuotas } from '@/api/userUsage'
import { getTeamUsageScope } from '@/api/teamUsage'

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
}))

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(() => Promise.resolve({
    data: {
      data: {
        configured: false,
        range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
        stats: null,
        trend: [],
        models: [],
      },
    },
  })),
  getUserUsageGroupQuotas: vi.fn(() => Promise.resolve({
    data: {
      data: {
        group_quotas: { status: 'empty', unit_label: 'USD', groups: [] },
        quota_freshness: { as_of: '2026-07-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' },
      },
    },
  })),
}))

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageScope: vi.fn(() => Promise.resolve({
    data: {
      data: {
        is_representative: true,
        departments: [{ external_id: 'department-alpha', name: 'Department Alpha', display_path: 'Department Alpha', subtree_member_count: 2, matched_user_count: 2 }],
      },
    },
  })),
  listTeamUsageSubjects: vi.fn(() => Promise.resolve({
    data: {
      data: {
        page: 1,
        page_size: 50,
        total: 1,
        subjects: [
          { subject_type: 'self', user_id: 100, display_name: 'Me', email: 'alice@example.com', selectable: true },
        ],
      },
    },
  })),
  getTeamUsageSubjectDashboard: vi.fn(),
  getTeamUsageAudit: vi.fn(() => Promise.resolve({
    data: { data: { items: [], page: 1, page_size: 20, total: 0 } },
  })),
  updateTeamUsageRateMultiplier: vi.fn(),
}))

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetOptions: vi.fn(),
  createQuotaResetRequest: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/components/charts/LineChartCanvas.vue', () => ({
  __esModule: true,
  default: {
    props: ['data', 'options'],
    template: '<div data-test="line-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
  },
}))

vi.mock('@/components/charts/DoughnutChartCanvas.vue', () => ({
  __esModule: true,
  default: {
    props: ['data', 'options'],
    template: '<div data-test="doughnut-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
  },
}))

const usageSnapshot = {
  configured: true,
  range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
  stats: {
    total_requests: 100,
    total_input_tokens: 10000,
    total_output_tokens: 5000,
    total_cache_creation_tokens: 200,
    total_cache_read_tokens: 300,
    total_tokens: 15500,
    total_cost: 2.5,
    total_actual_cost: 2,
    today_requests: 12,
    today_input_tokens: 1000,
    today_output_tokens: 500,
    today_cache_creation_tokens: 20,
    today_cache_read_tokens: 30,
    today_tokens: 1550,
    today_cost: 0.25,
    today_actual_cost: 0.2,
    average_duration_ms: 850,
    rpm: 2,
    tpm: 3000,
  },
  trend: [{ date: '2026-06-06', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
  models: [{ model: 'example-model', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
  group_quotas: { status: 'empty', unit_label: 'USD', message: '', groups: [] },
  usage_freshness: {
    as_of: '2026-07-15T08:00:00Z',
    fresh_until: '2026-07-15T08:00:27Z',
    stale_until: '2026-07-15T08:01:48Z',
    cache_status: 'miss',
    source_status: 'ok',
  },
}

const usageSnapshotWithQuotas = {
  ...usageSnapshot,
  group_quotas: {
    status: 'ok',
    unit_label: 'USD',
    message: '',
    groups: [
      { group_id: '42', group_name: 'Group Alpha', platform: 'openai', used_amount: 32.4, quota_amount: 100, is_unlimited: false, quota_source: 'api_key' },
      { group_id: '43', group_name: 'Group Beta', platform: 'anthropic', used_amount: 18.2, quota_amount: null, is_unlimited: true, quota_source: '' },
    ],
  },
}

function quotaResponse(groupQuotas: any) {
  return {
    data: {
      data: {
        group_quotas: groupQuotas,
        quota_freshness: { as_of: '2026-07-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' },
      },
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: DashboardView },
      { path: '/usage', name: 'Usage', component: DashboardView },
      { path: '/usage/members/:user_id', name: 'UsageMember', component: DashboardView },
      { path: '/usage/team', name: 'UsageTeam', component: { template: '<div>Team Usage</div>' } },
      { path: '/usage/quota-reset', name: 'UsageQuotaReset', component: { template: '<div>Quota Reset</div>' } },
      { path: '/work-items', name: 'WorkItems', component: { template: '<div>Work Items</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

describe('DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
    ;(getUserUsageDashboard as any).mockReset().mockResolvedValue({ data: { data: usageSnapshot } })
    ;(getUserUsageGroupQuotas as any).mockReset().mockResolvedValue({
      data: {
        data: {
          group_quotas: { status: 'empty', unit_label: 'USD', groups: [] },
          quota_freshness: { as_of: '2026-07-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' },
        },
      },
    })
    ;(getTeamUsageScope as any).mockReset().mockResolvedValue({
      data: { data: { is_representative: true, departments: [] } },
    })
  })

  it('renders usage before representative scope and quota finish loading', async () => {
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    const { getTeamUsageScope } = await import('@/api/teamUsage')
    const quota = deferred<any>()
    const scope = deferred<any>()
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(getUserUsageGroupQuotas as any).mockReturnValue(quota.promise)
    ;(getTeamUsageScope as any).mockReturnValue(scope.promise)

    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('30 Days Requests')
    expect(wrapper.text()).toContain('1.55K')
    expect(wrapper.find('a[href="/usage/team"]').exists()).toBe(false)
    expect(getUserUsageGroupQuotas).toHaveBeenCalledTimes(1)
  })

  it('renders the usage date range as one radio mode group', async () => {
    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    const thirtyDays = wrapper.get('[data-test="range-30d"]')
    expect(thirtyDays.classes()).toContain('el-radio-button')
    expect((thirtyDays.get('input[type="radio"]').element as HTMLInputElement).checked).toBe(true)
  })

  it('keeps usage visible when the independent quota request fails', async () => {
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(getUserUsageGroupQuotas as any).mockRejectedValue(new Error('synthetic quota outage'))

    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('30 Days Requests')
    expect(wrapper.text()).toContain('Group quotas are temporarily unavailable.')
    expect(wrapper.text()).not.toContain('Usage dashboard is temporarily unavailable')
  })

  it('renders a localized marker only for stale usage', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          usage_freshness: { ...usageSnapshot.usage_freshness, cache_status: 'stale', source_status: 'error' },
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    const staleAlert = wrapper.findAllComponents({ name: 'ElAlert' })
      .find((component) => component.text().includes('recent snapshot'))
    expect(staleAlert).toBeDefined()
    expect(staleAlert!.props('type')).toBe('warning')
  })

  it('aborts superseded personal requests and ignores out-of-order responses', async () => {
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    const usageRequests = [deferred<any>(), deferred<any>(), deferred<any>()]
    const quotaRequests = [deferred<any>(), deferred<any>(), deferred<any>()]
    ;(getUserUsageDashboard as any)
      .mockReturnValueOnce(usageRequests[0].promise)
      .mockReturnValueOnce(usageRequests[1].promise)
      .mockReturnValueOnce(usageRequests[2].promise)
    ;(getUserUsageGroupQuotas as any)
      .mockReturnValueOnce(quotaRequests[0].promise)
      .mockReturnValueOnce(quotaRequests[1].promise)
      .mockReturnValueOnce(quotaRequests[2].promise)

    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    await wrapper.get('[data-test="range-7d"]').trigger('click')
    await wrapper.get('[data-test="range-today"]').trigger('click')
    expect((getUserUsageDashboard as any).mock.calls[0][1].aborted).toBe(true)
    expect((getUserUsageGroupQuotas as any).mock.calls[0][1].aborted).toBe(true)
    expect((getUserUsageDashboard as any).mock.calls[1][1].aborted).toBe(true)
    expect((getUserUsageGroupQuotas as any).mock.calls[1][1].aborted).toBe(true)

    usageRequests[2].resolve({ data: { data: { ...usageSnapshot, models: [{ ...usageSnapshot.models[0], model: 'latest-model' }] } } })
    quotaRequests[2].resolve({ data: { data: { group_quotas: { ...usageSnapshotWithQuotas.group_quotas, groups: [{ ...usageSnapshotWithQuotas.group_quotas.groups[0], group_name: 'Latest Group' }] }, quota_freshness: { as_of: '2026-07-15T08:00:02Z', cache_status: 'uncached', source_status: 'ok' } } } })
    await flushPromises()
    usageRequests[1].resolve({ data: { data: { ...usageSnapshot, models: [{ ...usageSnapshot.models[0], model: 'older-model' }] } } })
    quotaRequests[1].resolve({ data: { data: { group_quotas: { status: 'empty', groups: [] }, quota_freshness: { as_of: '2026-07-15T08:00:01Z', cache_status: 'uncached', source_status: 'ok' } } } })
    usageRequests[0].resolve({ data: { data: usageSnapshot } })
    quotaRequests[0].resolve({ data: { data: { group_quotas: { status: 'empty', groups: [] }, quota_freshness: { as_of: '2026-07-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' } } } })
    await flushPromises()

    await vi.waitFor(() => expect(wrapper.text()).toContain('latest-model'))
    expect(wrapper.text()).toContain('Latest Group')
    expect(wrapper.text()).not.toContain('older-model')
  })

  it('does not call personal usage or quota endpoints on a member route', async () => {
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    const { getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: { data: { ...usageSnapshot, subject: { user_id: 225, display_name: 'Pat', selectable: true }, subject_subscription_groups: [] } },
    })

    const router = createTestRouter()
    await router.push('/usage/members/225')
    await router.isReady()
    mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getTeamUsageSubjectDashboard).toHaveBeenCalled()
    expect(getUserUsageDashboard).not.toHaveBeenCalled()
    expect(getUserUsageGroupQuotas).not.toHaveBeenCalled()
  })

  it('renders personal AI usage title', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.get('h1').text()).toBe('My AI Usage')
    expect(wrapper.text()).toContain('AI Usage Center')
    const usageTabs = wrapper.get('[data-testid="usage-center-tabs"]')
    expect(usageTabs.classes()).toContain('w-full')
    expect(usageTabs.classes()).toContain('sm:w-auto')
    expect(usageTabs.classes()).toContain('el-segmented')
    const segmented = usageTabs
    expect(segmented.classes()).not.toContain('is-block')
    expect(segmented.classes()).toContain('!min-h-11')
    expect(segmented.classes()).toContain('sm:min-w-max')
    expect(segmented.findAll('.el-segmented__item-label > span')).toHaveLength(3)
    expect(segmented.findAll('.el-segmented__item-label > span').every((label) => label.classes().includes('whitespace-normal'))).toBe(true)
    expect(segmented.findAll('.el-segmented__item-label > span').every((label) => label.classes().includes('sm:whitespace-nowrap'))).toBe(true)
    const quotaResetTab = wrapper.findAll('.el-segmented__item').find((tab) => tab.text() === 'Reset Requests')
    expect(quotaResetTab).toBeTruthy()
    await quotaResetTab!.get('input').setValue()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/usage/quota-reset')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('displays loading state initially', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    // Never resolve to keep loading state
    ;(getUserUsageDashboard as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Loading usage dashboard')
  })

  it('requests the 30 day snapshot on first homepage load', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshotWithQuotas } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect((getUserUsageDashboard as any).mock.calls[0][0]).toMatchObject({
      granularity: 'day',
      start_date: expect.any(String),
      end_date: expect.any(String),
    })
  })

  it('displays dashboard data after loading', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: false,
          range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
          stats: null,
          trend: [],
          models: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).not.toContain('Complete AI setup first')
    expect(wrapper.text()).not.toContain('Complete AI service configuration')
    expect(wrapper.text()).not.toContain('Go to AI Setup & Configuration')
    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.text()).not.toContain('I am an engineer')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('does not load provider credentials to classify homepage setup state', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'claude-sonnet',
              is_primary: true,
              groups: [
                { group_id: '1', group_name: 'Group Alpha', platform: 'anthropic', credential: { state: 'existing_hidden' } },
                { group_id: '2', group_name: 'Group Beta', platform: 'openai', credential: { state: 'missing' } },
                { group_id: '3', group_name: 'Group Gamma', platform: 'anthropic', credential: { state: 'existing_hidden' } },
              ],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: true,
          range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
          stats: {
            total_requests: 0,
            total_input_tokens: 0,
            total_output_tokens: 0,
            total_cache_creation_tokens: 0,
            total_cache_read_tokens: 0,
            total_tokens: 0,
            total_cost: 0,
            total_actual_cost: 0,
            today_requests: 0,
            today_input_tokens: 0,
            today_output_tokens: 0,
            today_cache_creation_tokens: 0,
            today_cache_read_tokens: 0,
            today_tokens: 0,
            today_cost: 0,
            today_actual_cost: 0,
            average_duration_ms: 0,
            rpm: 0,
            tpm: 0,
          },
          trend: [],
          models: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).not.toContain('AI setup is ready, start your first usage')
    expect(wrapper.text()).not.toContain('AI setup')
    expect(wrapper.text()).not.toContain('Done')
    expect(wrapper.text()).not.toContain('Code association')
  })

  it('shows placeholder values when API fails', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockRejectedValue(new Error('Network error'))
    ;(getUserUsageDashboard as any).mockRejectedValue(new Error('Network error'))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Usage dashboard is temporarily unavailable')
    const unavailableAlert = wrapper.findAllComponents({ name: 'ElAlert' })
      .find((component) => component.text().includes('Usage dashboard is temporarily unavailable'))
    expect(unavailableAlert).toBeDefined()
    expect(unavailableAlert!.props('type')).toBe('error')
    expect(wrapper.text()).not.toContain('Go to AI Setup & Configuration')
  })

  it('renders the embedded usage dashboard without cost cards', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    await vi.waitFor(() => expect(wrapper.text()).toContain('Token Trend'))
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).not.toContain('7 Days Cost')
    expect(wrapper.text()).not.toContain('$0.2000')
  })

  it('renders the relay usage dashboard inside the home page', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(wrapper.text()).toContain('Token Trend'))
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
  })

  it('keeps personal usage isolated from representative member selection', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({
      data: {
        data: {
          page: 1,
          page_size: 50,
          total: 1,
          subjects: [
            {
              subject_type: 'member',
              user_id: 101,
              display_name: 'Alice',
              email: 'alice@example.com',
              department_display_path: 'Department Alpha',
              selectable: true,
            },
          ],
        },
      },
    })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="usage-subject-selector"]').exists()).toBe(false)
    expect(listTeamUsageSubjects).not.toHaveBeenCalled()
    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    expect(getTeamUsageSubjectDashboard).not.toHaveBeenCalled()
  })

  it('opens member usage from the canonical member route after subjects load', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({
      data: {
        data: {
          page: 1,
          page_size: 50,
          total: 1,
          subjects: [
            {
              subject_type: 'member',
              user_id: 101,
              display_name: 'Alice',
              email: 'alice@example.com',
              department_display_path: 'Department Alpha',
              selectable: true,
            },
          ],
        },
      },
    })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/101')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getTeamUsageSubjectDashboard).toHaveBeenCalledWith(101, expect.objectContaining({ granularity: 'day' }))
    expect(getUserUsageDashboard).not.toHaveBeenCalled()
    expect(listTeamUsageSubjects).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="usage-subject-selector"]').exists()).toBe(false)
  })

  it('renders canonical member route as an independent member page without usage center tabs or subject selector', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({
      data: {
        data: {
          page: 1,
          page_size: 50,
          total: 1,
          subjects: [
            {
              subject_type: 'member',
              user_id: 101,
              display_name: 'Alice',
              email: 'alice@example.com',
              department_display_path: 'Department Alpha',
              selectable: true,
            },
          ],
        },
      },
    })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            department_display_path: 'Department Alpha',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/101')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.get('h1').text()).toBe('Member Usage')
    expect(wrapper.text()).toContain('Member Usage')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('Team Overview')
    expect(wrapper.text().match(/Member Usage/g) ?? []).toHaveLength(1)
    expect(wrapper.find('[data-testid="usage-center-tabs"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="usage-subject-selector"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('AI setup completed')
    expect(wrapper.text()).not.toContain('My Usage')
  })

  it('wraps member usage controls on mobile and keeps them on one row from sm', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            department_display_path: 'Department Alpha / Department Beta / Department Gamma / Department Delta',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/101')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    const controls = wrapper.get('[data-test="range-today"]').element.parentElement as HTMLElement
    expect(controls.className).toContain('flex-wrap')
    expect(controls.className).toContain('sm:flex-nowrap')
    expect(controls.className).toContain('sm:overflow-x-auto')
    expect(controls.className).toContain('shrink-0')
  })

  it('shows an explicit team overview return link on canonical member route', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({ data: { data: { page: 1, page_size: 20, total: 0, subjects: [] } } })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            department_display_path: 'Department Alpha',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/101')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    const backLink = wrapper.get('[data-testid="member-usage-back"]')
    expect(backLink.text()).toContain('Back to Team Overview')
    expect(backLink.attributes('href')).toBe('/usage/team')
    const returnAreaText = backLink.element.parentElement?.textContent ?? ''
    expect(returnAreaText).not.toContain('AI Usage Center')
    expect(wrapper.text()).toContain('Team Overview')
    expect(wrapper.text().match(/Member Usage/g) ?? []).toHaveLength(1)
  })

  it('keeps member email in subtitle without duplicating it when the display name is the email', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({ data: { data: { page: 1, page_size: 20, total: 0, subjects: [] } } })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'alice@example.com',
            email: 'alice@example.com',
            department_display_path: 'Department Alpha',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/101')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text().match(/alice@example\.com/g) ?? []).toHaveLength(1)
    expect(wrapper.text()).toContain('Department Alpha')
    expect(wrapper.text().match(/Member Usage/g) ?? []).toHaveLength(1)
  })

  it('renders selected member subscription groups before quotas', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({ data: { data: { page: 1, page_size: 20, total: 0, subjects: [] } } })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshotWithQuotas,
          subject: {
            subject_type: 'member',
            user_id: 101,
            display_name: 'Alice',
            email: 'alice@example.com',
            department_display_path: 'Department Alpha',
            selectable: true,
          },
          subject_subscription_groups: [
            {
              group_id: '42',
              group_name: 'Group Alpha',
              platform: 'openai',
              subscription_status: 'active',
              inherited_default_multiplier: 1,
              system_default_multiplier: 1,
              user_multiplier: null,
              effective_multiplier: 1,
              multiplier_source: 'group',
              daily_display_used_usd: 0,
              weekly_display_used_usd: 0,
              monthly_display_used_usd: 32.4,
              daily_usage_usd: 0,
              weekly_usage_usd: 0,
              monthly_usage_usd: 32.4,
              monthly_effective_allowance_usd: 100,
              usage_value_basis: 'raw_actual_cost',
              quota_window_basis: 'sub2api_enforcement_window',
              editable: true,
            },
          ],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/101')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await vi.dynamicImportSettled()
    await flushPromises()

    const subscriptionIndex = wrapper.text().indexOf('Subscription groups')
    const quotaIndex = wrapper.text().indexOf('Monthly Quotas')
    expect(subscriptionIndex).toBeGreaterThanOrEqual(0)
    expect(quotaIndex).toBeGreaterThanOrEqual(0)
    expect(subscriptionIndex).toBeLessThan(quotaIndex)
  })

  it('loads canonical member route directly without paging representative subjects', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockRejectedValue(new Error('subjects should not be loaded for direct member route'))
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: 225,
            display_name: 'Pat',
            email: 'pat@example.com',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/usage/members/225')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(listTeamUsageSubjects).not.toHaveBeenCalled()
    expect(getUserUsageDashboard).not.toHaveBeenCalled()
    expect(getTeamUsageSubjectDashboard).toHaveBeenCalledWith(225, expect.objectContaining({ granularity: 'day' }))
    expect(wrapper.find('[data-testid="usage-subject-selector"]').exists()).toBe(false)
    expect(wrapper.get('h1').text()).toBe('Member Usage')
    expect(wrapper.text()).toContain('pat@example.com')
  })

  it('does not request or render audit entries from personal usage', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard, getTeamUsageAudit } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listTeamUsageSubjects as any).mockResolvedValue({
      data: {
        data: {
          page: 1,
          page_size: 50,
          total: 2,
          subjects: [
            { subject_type: 'member', user_id: 101, display_name: 'Alice', email: 'alice@example.com', selectable: true },
            { subject_type: 'member', user_id: 102, display_name: 'Bob', email: 'bob@example.org', selectable: true },
          ],
        },
      },
    })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(getTeamUsageAudit as any).mockResolvedValue({
      data: {
        data: {
          items: [
            {
              id: 2,
              actor_user_id: 100,
              group_id: '43',
              group_name: 'Group Beta',
              action: 'set_rate_multiplier',
              status: 'succeeded',
              changed: true,
              old_multiplier: 1,
              new_multiplier: 2,
              reason: 'Hidden audit reason',
              created_at: '2026-06-26T00:01:00Z',
              updated_at: '2026-06-26T00:01:00Z',
            },
          ],
          page: 1,
          page_size: 20,
          total: 1,
        },
      },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="usage-subject-selector"]').exists()).toBe(false)
    expect(listTeamUsageSubjects).not.toHaveBeenCalled()
    expect(getTeamUsageSubjectDashboard).not.toHaveBeenCalled()
    expect(getTeamUsageAudit).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('Audit')
    expect(wrapper.text()).not.toContain('Hidden audit reason')
  })

  it('does not render member-scoped quota rows on personal usage', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard, getTeamUsageAudit } = await import('@/api/teamUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          subject_subscription_groups: [
            {
              group_id: '42',
              group_name: 'Group Alpha',
              platform: 'openai',
              subscription_status: 'active',
              inherited_default_multiplier: 1,
              system_default_multiplier: 1,
              user_multiplier: null,
              effective_multiplier: 1,
              multiplier_source: 'group',
              daily_display_used_usd: 0,
              weekly_display_used_usd: 0,
              monthly_display_used_usd: 80,
              daily_usage_usd: 0,
              weekly_usage_usd: 0,
              monthly_usage_usd: 80,
              monthly_effective_allowance_usd: 500,
              usage_value_basis: 'raw_actual_cost',
              quota_window_basis: 'sub2api_enforcement_window',
              editable: true,
            },
          ],
        },
      },
    })
    ;(listTeamUsageSubjects as any).mockResolvedValue({
      data: {
        data: {
          page: 1,
          page_size: 50,
          total: 2,
          subjects: [
            { subject_type: 'member', user_id: 101, display_name: 'Alice', email: 'alice@example.com', selectable: true },
            { subject_type: 'member', user_id: 102, display_name: 'Bob', email: 'bob@example.org', selectable: true },
          ],
        },
      },
    })
    ;(getTeamUsageSubjectDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(getTeamUsageAudit as any).mockResolvedValue({
      data: { data: { items: [], page: 1, page_size: 20, total: 0 } },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="usage-subject-selector"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Subscription groups')
    expect(wrapper.find('[data-testid="edit-multiplier-42"]').exists()).toBe(false)
    expect(listTeamUsageSubjects).not.toHaveBeenCalled()
    expect(getTeamUsageSubjectDashboard).not.toHaveBeenCalled()
    expect(getTeamUsageAudit).not.toHaveBeenCalled()
  })

  it('renders homepage group quota cards above the usage stats', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshotWithQuotas } })
    ;(getUserUsageGroupQuotas as any).mockResolvedValue(quotaResponse(usageSnapshotWithQuotas.group_quotas))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Monthly Quotas')
    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).toContain('$32.40 / $100.00')
    expect(wrapper.text()).toContain('$18.20 / ∞')
    expect(wrapper.text()).toContain('My AI Usage')
  })

  it('constrains a single quota card to an intentional readable width', async () => {
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    const singleGroupQuotas = {
      ...usageSnapshotWithQuotas.group_quotas,
      groups: [usageSnapshotWithQuotas.group_quotas.groups[0]],
    }
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: { data: { ...usageSnapshotWithQuotas, group_quotas: singleGroupQuotas } },
    })
    ;(getUserUsageGroupQuotas as any).mockResolvedValue(quotaResponse(singleGroupQuotas))

    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    const quotaGrid = wrapper.get('.max-w-xl')
    expect(quotaGrid.classes()).toContain('max-w-xl')
    expect(quotaGrid.findAll('article')).toHaveLength(1)
  })

  it('opens quota reset request modal from group quota cards and submits a request', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    const { getQuotaResetOptions, createQuotaResetRequest } = await import('@/api/quotaReset')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshotWithQuotas } })
    ;(getUserUsageGroupQuotas as any).mockResolvedValue(quotaResponse(usageSnapshotWithQuotas.group_quotas))
    ;(getQuotaResetOptions as any).mockResolvedValue({
      data: {
        data: {
          provider_id: 1,
          groups: [
            {
              group_id: '42',
              group_name: 'Group Alpha',
              platform: 'openai',
              daily_usage_usd: 10,
              weekly_usage_usd: 20,
              monthly_usage_usd: 30,
            },
          ],
        },
      },
    })
    ;(createQuotaResetRequest as any).mockResolvedValue({ data: { data: { id: 9, status: 'pending' } } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    await wrapper.get('[data-testid="open-quota-reset-request"]').trigger('click')
    await flushPromises()
    await vi.dynamicImportSettled()
    await flushPromises()
    await wrapper.get('textarea').setValue('Need reset for a build investigation')
    await wrapper.get('[data-testid="quota-reset-submit"]').trigger('click')
    await flushPromises()

    expect(getQuotaResetOptions).toHaveBeenCalled()
    expect(createQuotaResetRequest).toHaveBeenCalledWith({
      group_id: '42',
      reason: 'Need reset for a build investigation',
    })
  })

  it('hides the quota section when the snapshot reports empty quotas', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).not.toContain('Group Quotas')
  })

  it('shows a lightweight unavailable message when quota loading degrades', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          group_quotas: { status: 'unavailable', unit_label: 'USD', message: 'Group quotas are temporarily unavailable.', groups: [] },
        },
      },
    })
    ;(getUserUsageGroupQuotas as any).mockResolvedValue(quotaResponse({
      status: 'unavailable',
      unit_label: 'USD',
      message: 'Group quotas are temporarily unavailable.',
      groups: [],
    }))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Monthly Quotas')
    expect(wrapper.text()).toContain('temporarily unavailable')
    expect(wrapper.text()).toContain('Token Trend')
    expect(wrapper.text()).toContain('Model Distribution')
  })

  it('shows quota skeleton loading while range refresh is in flight', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard, getUserUsageGroupQuotas } = await import('@/api/userUsage')
    const refresh = deferred<{ data: { data: typeof usageSnapshotWithQuotas } }>()
    const quotaRefresh = deferred<any>()
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any)
      .mockResolvedValueOnce({ data: { data: usageSnapshotWithQuotas } })
      .mockReturnValueOnce(refresh.promise)
    ;(getUserUsageGroupQuotas as any)
      .mockResolvedValueOnce(quotaResponse(usageSnapshotWithQuotas.group_quotas))
      .mockReturnValueOnce(quotaRefresh.promise)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('$32.40 / $100.00')

    await wrapper.get('[data-test="range-30d"]').trigger('click')
    wrapper.get('[data-testid="usage-group-quotas-loading"]')

    refresh.resolve({ data: { data: usageSnapshotWithQuotas } })
    quotaRefresh.resolve(quotaResponse(usageSnapshotWithQuotas.group_quotas))
    await flushPromises()
    expect(wrapper.find('[data-testid="usage-group-quotas-loading"]').exists()).toBe(false)
  })

  it('does not render a homepage guide card for users without reusable AI access', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'missing' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: false,
          range: { start_date: '2026-06-09', end_date: '2026-06-15', granularity: 'day', timezone: 'Asia/Shanghai' },
          stats: null,
          trend: [],
          models: [],
        },
      },
    })
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid^="home-guide-"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).not.toContain('Complete AI setup first')
    expect(wrapper.text()).not.toContain('Complete AI service configuration')
    expect(wrapper.text()).not.toContain('Go to AI Setup & Configuration')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('keeps missing AI access reminders out of the home page', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'missing' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: false,
          range: { start_date: '2026-06-09', end_date: '2026-06-15', granularity: 'day', timezone: 'Asia/Shanghai' },
          stats: null,
          trend: [],
          models: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid^="home-guide-"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).not.toContain('Complete AI setup first')
    expect(wrapper.text()).not.toContain('Complete AI service configuration')
    expect(wrapper.text()).not.toContain('Go to AI Setup & Configuration')
  })

  it('keeps users with access but no effective usage on the usage dashboard', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: true,
          range: { start_date: '2026-06-09', end_date: '2026-06-15', granularity: 'day', timezone: 'Asia/Shanghai' },
          stats: {
            total_requests: 0,
            total_input_tokens: 0,
            total_output_tokens: 0,
            total_cache_creation_tokens: 0,
            total_cache_read_tokens: 0,
            total_tokens: 0,
            total_cost: 0,
            total_actual_cost: 0,
            today_requests: 0,
            today_input_tokens: 0,
            today_output_tokens: 0,
            today_cache_creation_tokens: 0,
            today_cache_read_tokens: 0,
            today_tokens: 0,
            today_cost: 0,
            today_actual_cost: 0,
            average_duration_ms: 0,
            rpm: 0,
            tpm: 0,
          },
          trend: [],
          models: [],
        },
      },
    })
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid^="home-guide-"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).not.toContain('AI setup is ready, start your first usage')
    expect(wrapper.text()).not.toContain('Go to AI Setup & Configuration')
  })

  it('shows usage without a setup guide for established users', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid^="home-guide-"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('AI setup completed')
    expect(wrapper.text()).not.toContain('View setup guidance')
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.text()).not.toContain('I am an engineer')
    expect(wrapper.text()).not.toContain('7 Days Cost')
    expect(wrapper.text()).not.toContain('$0.6000')
  })

  it('does not render the developer helper toggle or cards on the home page', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
              id: 1,
              name: 'prod',
              display_name: 'Production',
              base_url: 'https://relay.example.com',
              default_model: 'gpt-5.4',
              is_primary: true,
              groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
            },
          ],
        },
      },
    })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.find('[data-testid="home-developer-toggle"]').exists()).toBe(false)
  })

  it('shows a usage unavailable state instead of setup guidance when homepage data fails', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockRejectedValue(new Error('provider network error'))
    ;(getUserUsageDashboard as any).mockRejectedValue(new Error('usage network error'))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getUserProviders).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Usage dashboard is temporarily unavailable')
    expect(wrapper.text()).not.toContain('Complete AI setup first')
    expect(wrapper.text()).not.toContain('Go to AI Setup & Configuration')
  })
})
