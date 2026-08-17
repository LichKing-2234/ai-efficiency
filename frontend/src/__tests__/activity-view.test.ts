import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import ActivityView from '@/views/activity/ActivityView.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/activity', () => ({
  getActivityV2Overview: vi.fn(),
  listActivityV2Repositories: vi.fn(),
  listActivityV2PullRequests: vi.fn(),
}))
vi.mock('@/api/attribution', () => ({
  getReportingReadiness: vi.fn(),
}))
vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn().mockResolvedValue({ data: { data: { total_count: 0 } } }),
}))

const overview = {
  contract_version: 'activity-v2', scope_version: 'scope-1', from: '2026-07-14', to: '2026-08-12', timezone: 'Asia/Shanghai', committed_tokens: 400,
  claim_coverage: { complete: true, lower_bound: false }, scm_coverage: { complete: true, affected_repositories: 0 },
  ratio: { state: 'exact', committed_tokens: 400, total_tokens: 1000, percent: 40, percentage_point_change: 8 },
  trend: [{ date: '2026-08-12', direct_tokens: 400, shared_tokens: 0, involved_tokens: 0 }], readiness: { state: 'active' },
}
const repositories = { items: [{ repo_config_id: 9, name: 'example/repo', direct_tokens: 400, direct_share: 100, shared_tokens: 50 }] }
const pullRequests = { items: [{ pr_record_id: 21, repo_config_id: 9, repository_name: 'example/repo', scm_pr_id: 88, title: 'Improve Activity', url: 'https://example.com/pr/88', status: 'merged', involved_tokens: 400, overlap_state: 'shared', commits: [{ repo_config_id: 9, commit_sha: 'abcdef1234567890' }] }] }
const mountedWrappers: Array<{ unmount: () => void }> = []

async function mountView(path = '/activity', reportingCapabilities?: { setup_available: boolean; readiness_available: boolean }) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/activity', component: ActivityView },
    { path: '/activity/members/:user_id', component: ActivityView },
    { path: '/usage', component: { template: '<div />' } },
    { path: '/user', component: { template: '<div />' } },
    { path: '/work-items', component: { template: '<div />' } },
  ] })
  await router.push(path)
  await router.isReady()
  const pinia = createPinia()
  const auth = useAuthStore(pinia)
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'relay_sso', reporting_capabilities: reportingCapabilities }
  const wrapper = mount(ActivityView, { global: { plugins: [pinia, router] } })
  mountedWrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

describe('ActivityView v2', () => {
  beforeEach(async () => {
    setLocale('en-US')
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityV2Overview).mockReset().mockResolvedValue({ data: { data: overview } } as any)
    vi.mocked(api.listActivityV2Repositories).mockReset().mockResolvedValue({ data: { data: repositories } } as any)
    vi.mocked(api.listActivityV2PullRequests).mockReset().mockResolvedValue({ data: { data: pullRequests } } as any)
    const attributionApi = await import('@/api/attribution')
    vi.mocked(attributionApi.getReportingReadiness).mockReset()
  })

  afterEach(() => {
    mountedWrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  })

  it('does not mount readiness or call its endpoint when the capability is off', async () => {
    const attributionApi = await import('@/api/attribution')
    const { wrapper } = await mountView()
    expect(wrapper.find('[data-testid="reporting-compact-guide"]').exists()).toBe(false)
    expect(attributionApi.getReportingReadiness).not.toHaveBeenCalled()
  })

  it('shows persistent active readiness only on personal Activity', async () => {
    const attributionApi = await import('@/api/attribution')
    vi.mocked(attributionApi.getReportingReadiness).mockResolvedValue({
      data: { data: { state: 'active', retryable: false, latest_accepted_at: '2026-08-10T09:30:00Z' } },
    } as any)
    const capabilities = { setup_available: true, readiness_available: true }
    const { wrapper } = await mountView('/activity', capabilities)
    const guide = wrapper.get('[data-testid="reporting-compact-guide"]')
    expect(guide.get('[data-testid="reporting-active-state"]').text()).toContain('Codex activity reporting is active')
    expect(guide.text()).toContain('Latest accepted activity')
    expect(guide.findComponent({ name: 'ElCollapse' }).exists()).toBe(false)
    expect(guide.text()).not.toContain('ae-cli')

    vi.mocked(attributionApi.getReportingReadiness).mockClear()
    const member = await mountView('/activity/members/7', capabilities)
    expect(member.wrapper.find('[data-testid="reporting-compact-guide"]').exists()).toBe(false)
    expect(attributionApi.getReportingReadiness).not.toHaveBeenCalled()
  })

  it('keeps a readiness failure local while Activity analytics still render', async () => {
    const attributionApi = await import('@/api/attribution')
    vi.mocked(attributionApi.getReportingReadiness).mockRejectedValue(new Error('readiness unavailable'))
    const { wrapper } = await mountView('/activity', { setup_available: true, readiness_available: true })
    expect(wrapper.get('[data-testid="reporting-compact-guide"]').text()).toContain('Reporting status is temporarily unavailable')
    expect(wrapper.text()).toContain('Token used for actual code')
    expect(wrapper.find('[data-testid="activity-ratio-chart"]').exists()).toBe(true)
  })

  it('polls waiting readiness every 30 seconds, pauses while hidden, checks on visibility, and stops active', async () => {
    vi.useFakeTimers()
    let visibility: DocumentVisibilityState = 'visible'
    const visibilitySpy = vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility)
    try {
      const attributionApi = await import('@/api/attribution')
      const waiting = { data: { data: { state: 'waiting_for_data', retryable: false } } } as any
      const active = { data: { data: { state: 'active', retryable: false, latest_accepted_at: '2026-08-10T09:30:00Z' } } } as any
      vi.mocked(attributionApi.getReportingReadiness)
        .mockResolvedValueOnce(waiting)
        .mockResolvedValueOnce(waiting)
        .mockResolvedValueOnce(active)

      const { wrapper } = await mountView('/activity', { setup_available: true, readiness_available: true })
      expect(attributionApi.getReportingReadiness).toHaveBeenCalledTimes(1)
      expect(wrapper.get('[data-testid="reporting-compact-guide"]').text()).toContain('Waiting for the first accepted commit')

      await vi.advanceTimersByTimeAsync(30_000)
      expect(attributionApi.getReportingReadiness).toHaveBeenCalledTimes(2)

      visibility = 'hidden'
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(60_000)
      expect(attributionApi.getReportingReadiness).toHaveBeenCalledTimes(2)

      visibility = 'visible'
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(attributionApi.getReportingReadiness).toHaveBeenCalledTimes(3)
      expect(wrapper.get('[data-testid="reporting-active-state"]').text()).toContain('Codex activity reporting is active')

      await vi.advanceTimersByTimeAsync(60_000)
      expect(attributionApi.getReportingReadiness).toHaveBeenCalledTimes(3)
    } finally {
      visibilitySpy.mockRestore()
      vi.useRealTimers()
    }
  })

  it('renders ratio, trend, overall Top 5 and full lists without Request detail', async () => {
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=Asia%2FShanghai')
    expect(wrapper.text()).toContain('Token used for actual code')
    expect(wrapper.text()).toContain('Used for committed code')
    expect(wrapper.text()).toContain('Other Token')
    expect(wrapper.text()).toContain('40.0%')
    expect(wrapper.text()).toContain('+8.0 percentage points')
    expect(wrapper.text()).toContain('Repository Top 5')
    expect(wrapper.text()).toContain('PR Top 5')
    expect(wrapper.text()).not.toContain('Request ID')
    expect(wrapper.find('[data-testid="activity-ratio-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="activity-trend-chart"]').exists()).toBe(true)
  })

  it('does not render a non-zero committed ratio as zero', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityV2Overview).mockResolvedValue({
      data: { data: { ...overview, ratio: { state: 'exact', committed_tokens: 6_890_621, total_tokens: 18_175_641_094, percent: 0.037912413 } } },
    } as any)
    const { wrapper } = await mountView('/activity?from=2026-07-16&to=2026-08-14&timezone=Asia%2FShanghai')
    const ratioCard = wrapper.get('[aria-labelledby="activity-ratio-heading"]')
    expect(ratioCard.text()).toContain('0.04%')
    expect(ratioCard.text()).not.toContain('0.0%')
  })

  it('keeps overall rankings while a repository filters overview and PR list', async () => {
    const api = await import('@/api/activity')
    const { wrapper, router } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    await wrapper.findAll('button').find((button) => button.text().includes('example/repo'))!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.repo_id).toBe('9')
    expect(wrapper.find('[data-testid="activity-filter-chip"]').exists()).toBe(true)
    const overviewCalls = vi.mocked(api.getActivityV2Overview).mock.calls
    expect(overviewCalls).toHaveLength(3)
    expect(overviewCalls[overviewCalls.length - 2]?.[0]).not.toHaveProperty('repo_id')
    expect(overviewCalls[overviewCalls.length - 1]?.[0]).toEqual(expect.objectContaining({ repo_id: 9 }))
    expect(vi.mocked(api.listActivityV2PullRequests)).toHaveBeenLastCalledWith(expect.objectContaining({ repo_id: 9 }))
    expect(vi.mocked(api.listActivityV2Repositories)).toHaveBeenLastCalledWith(expect.not.objectContaining({ repo_id: 9 }))
  })

  it('uses member scope and preserves URL range/timezone', async () => {
    const api = await import('@/api/activity')
    await mountView('/activity/members/7?from=2026-08-01&to=2026-08-07&timezone=America%2FLos_Angeles')
    expect(vi.mocked(api.getActivityV2Overview)).toHaveBeenCalledWith(expect.objectContaining({ scope: 'member', subject_user_id: 7, from: '2026-08-01', to: '2026-08-07', timezone: 'America/Los_Angeles' }))
  })

  it.each([
    [7, '2026-08-06'],
    [30, '2026-07-14'],
    [90, '2026-05-15'],
  ])('applies an inclusive %i-local-day preset through the page URL and API', async (days, expectedFrom) => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 7, 12, 12))
    try {
      const api = await import('@/api/activity')
      const { wrapper, router } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
      await wrapper.get(`[data-testid="activity-range-${days}"]`).trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.query).toEqual(expect.objectContaining({ from: expectedFrom, to: '2026-08-12', timezone: 'UTC' }))
      expect(vi.mocked(api.getActivityV2Overview)).toHaveBeenLastCalledWith(expect.objectContaining({ from: expectedFrom, to: '2026-08-12', timezone: 'UTC' }))
    } finally {
      vi.useRealTimers()
    }
  })

  it('suppresses an older overview response after a newer range wins', async () => {
    const api = await import('@/api/activity')
    let resolveOld!: (value: any) => void
    vi.mocked(api.getActivityV2Overview)
      .mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve }) as any)
      .mockResolvedValueOnce({ data: { data: overview } } as any)
      .mockResolvedValueOnce({ data: { data: { ...overview, committed_tokens: 777, ratio: { ...overview.ratio, committed_tokens: 777 } } } } as any)
      .mockResolvedValueOnce({ data: { data: { ...overview, committed_tokens: 777, ratio: { ...overview.ratio, committed_tokens: 777 } } } } as any)
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    await wrapper.get('[data-testid="activity-range-7"]').trigger('click')
    await flushPromises()
    resolveOld({ data: { data: { ...overview, committed_tokens: 1, ratio: { ...overview.ratio, committed_tokens: 1 } } } })
    await flushPromises()
    expect(wrapper.text()).toContain('777')
    expect(wrapper.text()).not.toContain('1\n')
  })

  it('loads ratio and trend independently', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityV2Overview)
      .mockRejectedValueOnce(new Error('ratio failed'))
      .mockResolvedValueOnce({ data: { data: overview } } as any)
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    expect(wrapper.text()).toContain('Activity is temporarily unavailable.')
    expect(wrapper.find('[data-testid="activity-ratio-chart"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="activity-trend-chart"]').exists()).toBe(true)
  })

  it('preserves ratio data and surfaces a lane-local refresh failure', async () => {
    const api = await import('@/api/activity')
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    vi.mocked(api.getActivityV2Overview).mockRejectedValueOnce(new Error('refresh failed'))
    await wrapper.get('button[aria-label="Retry ratio"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="activity-ratio-chart"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Activity is temporarily unavailable.')
  })

  it('visibly highlights the selected PR while keeping overall context', async () => {
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    const pr = wrapper.findAll('button').find((button) => button.text().includes('Improve Activity'))!
    await pr.trigger('click')
    await flushPromises()
    const selected = wrapper.findAll('button').find((button) => button.text().includes('Improve Activity'))!
    expect(selected.attributes('aria-pressed')).toBe('true')
    expect(selected.classes()).toContain('bg-cyan-50')
    expect(wrapper.text()).toContain('Repository Top 5')
  })

  it('derives related-commit expansion from PR URL state and clears it with the filter', async () => {
    const { wrapper, router } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC&repo_id=9&pr_record_id=21')
    await wrapper.get('#tab-pullRequests').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Related commits')
    expect(wrapper.text()).toContain('abcdef123456')

    await router.push({ path: '/activity', query: { from: '2026-07-14', to: '2026-08-12', timezone: 'UTC', repo_id: '9' } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Related commits')
  })

  it.each([
    ['complete_zero_usage', { committed_tokens: 0, total_tokens: 0 }, 'No AI Token this period', false],
    ['true_zero_committed', { committed_tokens: 0, total_tokens: 1000, percent: 0 }, '0%', true],
    ['denominator_unavailable', { committed_tokens: 400 }, 'Complete Usage data is required', false],
    ['lower_bound', { committed_tokens: 400, total_tokens: 1000, percent: 40 }, '≥40.0%', true],
  ])('renders the %s ratio state exactly', async (state, ratio, expected, chart) => {
    const api = await import('@/api/activity')
    const stateOverview = { ...overview, ratio: { state, ...ratio } }
    vi.mocked(api.getActivityV2Overview).mockResolvedValue({ data: { data: stateOverview } } as any)
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    expect(wrapper.text()).toContain(expected)
    expect(wrapper.find('[data-testid="activity-ratio-chart"]').exists()).toBe(chart)
    expect(wrapper.text()).not.toContain('percentage points')
  })

  it('uses the server cursor and current search/sort for the full Repository list', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.listActivityV2Repositories)
      .mockResolvedValueOnce({ data: { data: repositories } } as any)
      .mockResolvedValueOnce({ data: { data: { ...repositories, next_cursor: 'next-page' } } } as any)
      .mockResolvedValueOnce({ data: { data: repositories } } as any)
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    await wrapper.get('input[data-testid="activity-list-search"]').setValue('example')
    await wrapper.get('[data-testid="activity-repositories-next"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(api.listActivityV2Repositories)).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'next-page', search: 'example', sort: 'tokens' }))
  })

  it('reloads a tab after resetting its shared list controls', async () => {
    const api = await import('@/api/activity')
    const { wrapper } = await mountView('/activity?from=2026-07-14&to=2026-08-12&timezone=UTC')
    const input = wrapper.get('input[data-testid="activity-list-search"]')
    await input.setValue('example')
    await input.trigger('keyup.enter')
    await flushPromises()
    expect(vi.mocked(api.listActivityV2Repositories)).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'example' }))

    await wrapper.get('#tab-pullRequests').trigger('click')
    await flushPromises()
    await wrapper.get('#tab-repositories').trigger('click')
    await flushPromises()
    expect(wrapper.get('input[data-testid="activity-list-search"]').element).toHaveProperty('value', '')
    expect(vi.mocked(api.listActivityV2Repositories)).toHaveBeenLastCalledWith(expect.objectContaining({ search: undefined, sort: 'tokens' }))
  })
})
