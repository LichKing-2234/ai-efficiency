import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import ActivityView from '@/views/activity/ActivityView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/activity', () => ({
  getActivityV2Overview: vi.fn(),
  listActivityV2Repositories: vi.fn(),
  listActivityV2PullRequests: vi.fn(),
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

async function mountView(path = '/activity') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/activity', component: ActivityView },
    { path: '/activity/members/:user_id', component: ActivityView },
    { path: '/usage', component: { template: '<div />' } },
    { path: '/user', component: { template: '<div />' } },
    { path: '/work-items', component: { template: '<div />' } },
  ] })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(ActivityView, { global: { plugins: [createPinia(), router] } })
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
    ['true_zero_committed', { committed_tokens: 0, total_tokens: 1000, percent: 0 }, '0.0%', true],
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
