import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import { setLocale } from '@/i18n'

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
}))

vi.mock('@/api/teamUsage', () => ({
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

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<div data-test="line-chart" />' },
  Doughnut: { template: '<div data-test="doughnut-chart" />' },
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
  })

  it('renders personal AI usage title', async () => {
    const { getUserProviders } = await import('@/api/user')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('h1').text()).toContain('Complete AI setup first')
    expect(wrapper.text()).toContain('AI Usage Center')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('displays loading state initially', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    // Never resolve to keep loading state
    ;(getUserProviders as any).mockReturnValue(new Promise(() => {}))
    ;(getUserUsageDashboard as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Loading your AI usage')
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

    expect(wrapper.text()).toContain('Complete AI setup first')
    expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.text()).not.toContain('I am an engineer')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('uses provider credentials to mark AI setup done inside the guide card', async () => {
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

    expect(getUserProviders).toHaveBeenCalled()
    expect(wrapper.text()).toContain('AI setup is ready, start your first usage')
    expect(wrapper.text()).toContain('AI setup')
    expect(wrapper.text()).toContain('Done')
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

    expect(wrapper.text()).toContain('Unable to load your current homepage status')
    expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
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

    expect(wrapper.text()).toContain('Token Trend')
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
    expect(wrapper.text()).toContain('Token Trend')
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
  })

  it('keeps My Usage selected when representative subjects omit self', async () => {
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

    const selector = wrapper.get('[data-testid="usage-subject-selector"]')
    expect(selector.text()).toContain('My Usage')
    expect(selector.text()).toContain('Alice')
    expect((selector.element as HTMLSelectElement).value).toBe('self:0')
    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    expect(getTeamUsageSubjectDashboard).not.toHaveBeenCalled()

    await selector.setValue('member:101')
    await flushPromises()

    expect(getTeamUsageSubjectDashboard).toHaveBeenCalledWith(101, expect.objectContaining({ granularity: 'day' }))
  })

  it('consumes subject_user_id query after subjects load and opens member usage in scope', async () => {
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
    await router.push({ path: '/', query: { subject_user_id: '101' } })
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(getTeamUsageSubjectDashboard).toHaveBeenCalledWith(101, expect.objectContaining({ granularity: 'day' }))
    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    expect((wrapper.get('[data-testid="usage-subject-selector"]').element as HTMLSelectElement).value).toBe('member:101')
  })

  it('loads enough representative subjects for subject_user_id deep links beyond the first default page', async () => {
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
          page_size: 500,
          total: 125,
          subjects: [
            {
              subject_type: 'member',
              user_id: 125,
              display_name: 'Pat',
              email: 'pat@example.com',
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
            user_id: 125,
            display_name: 'Pat',
            email: 'pat@example.com',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    })

    const router = createTestRouter()
    await router.push({ path: '/', query: { subject_user_id: '125' } })
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(listTeamUsageSubjects).toHaveBeenCalledWith({ page_size: 500 })
    expect(getTeamUsageSubjectDashboard).toHaveBeenCalledWith(125, expect.objectContaining({ granularity: 'day' }))
    expect((wrapper.get('[data-testid="usage-subject-selector"]').element as HTMLSelectElement).value).toBe('member:125')
  })

  it('ignores stale audit responses after switching selected members', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard, getTeamUsageAudit } = await import('@/api/teamUsage')
    const auditAlice = deferred<any>()
    const auditBob = deferred<any>()
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
    ;(getTeamUsageSubjectDashboard as any).mockImplementation((userID: number) => Promise.resolve({
      data: {
        data: {
          ...usageSnapshot,
          subject: {
            subject_type: 'member',
            user_id: userID,
            display_name: userID === 101 ? 'Alice' : 'Bob',
            email: userID === 101 ? 'alice@example.com' : 'bob@example.org',
            selectable: true,
          },
          subject_subscription_groups: [],
        },
      },
    }))
    ;(getTeamUsageAudit as any).mockImplementation((params: { target_user_id: number }) => {
      return params.target_user_id === 101 ? auditAlice.promise : auditBob.promise
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    const selector = wrapper.get('[data-testid="usage-subject-selector"]')
    await selector.setValue('member:101')
    await flushPromises()
    expect(getTeamUsageAudit).toHaveBeenLastCalledWith(expect.objectContaining({ target_user_id: 101 }))

    await wrapper.get('[data-testid="usage-subject-selector"]').setValue('member:102')
    await flushPromises()
    expect(getTeamUsageAudit).toHaveBeenLastCalledWith(expect.objectContaining({ target_user_id: 102 }))

    auditBob.resolve({
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
              reason: 'Bob adjustment',
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
    await flushPromises()
    expect(wrapper.text()).toContain('Group Beta')

    auditAlice.resolve({
      data: {
        data: {
          items: [
            {
              id: 1,
              actor_user_id: 100,
              group_id: '42',
              group_name: 'Group Alpha',
              action: 'set_rate_multiplier',
              status: 'succeeded',
              changed: true,
              old_multiplier: 1,
              new_multiplier: 3,
              reason: 'Alice adjustment',
              created_at: '2026-06-26T00:00:00Z',
              updated_at: '2026-06-26T00:00:00Z',
            },
          ],
          page: 1,
          page_size: 20,
          total: 1,
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Group Beta')
    expect(wrapper.text()).not.toContain('Alice adjustment')
  })

  it('clears member-scoped rows and audit immediately when switching members', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const { listTeamUsageSubjects, getTeamUsageSubjectDashboard, getTeamUsageAudit } = await import('@/api/teamUsage')
    const bobDashboard = deferred<any>()
    const bobAudit = deferred<any>()
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
    ;(getTeamUsageSubjectDashboard as any).mockImplementation((userID: number) => {
      if (userID === 102) return bobDashboard.promise
      return Promise.resolve({
        data: {
          data: {
            ...usageSnapshot,
            subject: { subject_type: 'member', user_id: 101, display_name: 'Alice', email: 'alice@example.com', selectable: true },
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
    })
    ;(getTeamUsageAudit as any).mockImplementation((params: { target_user_id: number }) => {
      if (params.target_user_id === 102) return bobAudit.promise
      return Promise.resolve({
        data: {
          data: {
            items: [
              {
                id: 1,
                actor_user_id: 100,
                group_id: '42',
                group_name: 'Group Alpha',
                action: 'set_rate_multiplier',
                status: 'succeeded',
                changed: true,
                old_multiplier: 1,
                new_multiplier: 2,
                reason: 'Alice adjustment',
                created_at: '2026-06-26T00:00:00Z',
                updated_at: '2026-06-26T00:00:00Z',
              },
            ],
            page: 1,
            page_size: 20,
            total: 1,
          },
        },
      })
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    await wrapper.get('[data-testid="usage-subject-selector"]').setValue('member:101')
    await flushPromises()
    expect(wrapper.text()).toContain('Subscription groups')
    expect(wrapper.text()).toContain('Alice adjustment')
    expect(wrapper.find('[data-testid="edit-multiplier-42"]').exists()).toBe(true)

    await wrapper.get('[data-testid="usage-subject-selector"]').setValue('member:102')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Subscription groups')
    expect(wrapper.text()).not.toContain('Alice adjustment')
    expect(wrapper.find('[data-testid="edit-multiplier-42"]').exists()).toBe(false)

    bobDashboard.resolve({
      data: {
        data: {
          ...usageSnapshot,
          subject: { subject_type: 'member', user_id: 102, display_name: 'Bob', email: 'bob@example.org', selectable: true },
          subject_subscription_groups: [
            {
              group_id: '43',
              group_name: 'Group Beta',
              platform: 'openai',
              subscription_status: 'active',
              inherited_default_multiplier: 1,
              system_default_multiplier: 1,
              user_multiplier: null,
              effective_multiplier: 1,
              multiplier_source: 'group',
              daily_display_used_usd: 0,
              weekly_display_used_usd: 0,
              monthly_display_used_usd: 20,
              daily_usage_usd: 0,
              weekly_usage_usd: 0,
              monthly_usage_usd: 20,
              monthly_effective_allowance_usd: 100,
              usage_value_basis: 'raw_actual_cost',
              quota_window_basis: 'sub2api_enforcement_window',
              editable: true,
            },
          ],
        },
      },
    })
    bobAudit.resolve({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })
    await flushPromises()

    expect(wrapper.text()).toContain('Group Beta')
    expect(wrapper.find('[data-testid="edit-multiplier-43"]').exists()).toBe(true)
  })

  it('renders homepage group quota cards above the usage stats', async () => {
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
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshotWithQuotas } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Monthly Quotas')
    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).toContain('$32.40 / $100.00')
    expect(wrapper.text()).toContain('$18.20 / ∞')
    expect(wrapper.text()).toContain('My Usage')
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
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          ...usageSnapshot,
          group_quotas: { status: 'unavailable', unit_label: 'USD', message: 'Group quotas are temporarily unavailable.', groups: [] },
        },
      },
    })

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
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const refresh = deferred<{ data: { data: typeof usageSnapshotWithQuotas } }>()
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

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('$32.40 / $100.00')

    await wrapper.get('[data-test="range-30d"]').trigger('click')
    wrapper.get('[data-testid="usage-group-quotas-loading"]')

    refresh.resolve({ data: { data: usageSnapshotWithQuotas } })
    await flushPromises()
    expect(wrapper.find('[data-testid="usage-group-quotas-loading"]').exists()).toBe(false)
  })

  it('shows the expanded guide card for users without any reusable AI access', async () => {
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

    expect(wrapper.text()).toContain('Complete AI setup first')
    expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('keeps the guide card expanded when AI access exists but no effective usage data is available', async () => {
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

    expect(wrapper.text()).toContain('AI setup is ready, start your first usage')
    expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
  })

  it('shows usage first and collapses the guide card for established users', async () => {
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

    expect(wrapper.text()).toContain('AI setup completed')
    expect(wrapper.text()).toContain('View setup guidance')
    expect(wrapper.text()).toContain('My Usage')
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

  it('shows a degraded-state warning instead of misclassifying provider or usage failures as setup-needed', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockRejectedValue(new Error('provider network error'))
    ;(getUserUsageDashboard as any).mockRejectedValue(new Error('usage network error'))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Unable to load your current homepage status')
    expect(wrapper.text()).not.toContain('Complete AI setup first')
  })
})
