import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ActivityView from '@/views/activity/ActivityView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/activity', () => ({
  getActivitySummary: vi.fn(),
  getActivityMember: vi.fn(),
  getActivityBucket: vi.fn(),
  normalizeMemberActivity: (value: any) => ({
    ...value,
    prs: { ...value.prs, items: value.prs?.items ?? [] },
    commits: { ...value.commits, items: value.commits?.items ?? [] },
    buckets: { ...value.buckets, items: value.buckets?.items ?? [] },
  }),
}))

function response(name = 'Alice', lowerBound = true, bucketAccess = false) {
  return {
    data: {
      data: {
        contract_version: 'activity-v1',
        window: { from: '2026-08-01T00:00:00Z', to: '2026-08-31T00:00:00Z' },
        member: { user_id: 7, display_name: name, email: 'alice@example.com', department_external_ids: [] },
        available: true,
        metrics: {
          participating_prs: { value: 2, lower_bound: lowerBound },
          merged_prs: { value: 1, lower_bound: lowerBound },
          active_repositories: 1,
          commit_count: 1,
          latest_activity: '2026-08-05T12:00:00Z',
        },
        quality: { measured_buckets: 1, unbound_buckets: 1, multi_repo_shared_buckets: 0, invalid_token_facts: 1, historical_advisory_facts: 0, coverage_gap_count: 1 },
        sync_coverage: { complete: !lowerBound, affected_repositories: lowerBound ? 1 : 0, unsynced_repositories: 1, stale_repositories: 0, partially_synced_repositories: 0, failed_repositories: 0 },
        prs: { items: [{ repo_config_id: 9, repo_name: 'acme/repo', pr_record_id: 21, scm_pr_id: 88, title: 'Improve activity', url: 'https://example.com/pr/88', status: 'merged', commits: [{ repo_config_id: 9, commit_sha: 'abcdef123456' }] }] },
        commits: { items: [{ repo_config_id: 9, repo_name: 'acme/repo', commit_sha: 'abcdef123456', latest_activity: '2026-08-05T12:00:00Z', processed_tokens: 1234, prs: [{ repo_config_id: 9, pr_record_id: 21, scm_pr_id: 88 }] }] },
        buckets: { items: [{ bucket_id: 'bucket-1', observed_end_at: '2026-08-05T12:00:00Z', processed_tokens: 1234, allocation_status: 'bound_auto' }] },
        bucket_access: bucketAccess,
      },
    },
  }
}

async function mountView(path = '/activity') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/activity', component: ActivityView },
      { path: '/activity/members/:user_id', component: ActivityView },
      { path: '/activity/teams', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(ActivityView, {
    global: {
      plugins: [router],
      stubs: { AppLayout: { template: '<main><slot /></main>' } },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('ActivityView', () => {
  beforeEach(async () => {
    setLocale('en-US')
    const api = await import('@/api/activity')
    vi.mocked(api.getActivitySummary).mockReset()
    vi.mocked(api.getActivityMember).mockReset()
    vi.mocked(api.getActivityBucket).mockReset()
    vi.mocked(api.getActivitySummary).mockResolvedValue(response() as any)
    vi.mocked(api.getActivityMember).mockResolvedValue(response() as any)
  })

  it('renders PR-first hero without making Token a headline metric', async () => {
    const { wrapper } = await mountView()
    const hero = wrapper.get('[data-testid="activity-hero"]')
    expect(hero.text()).toContain('≥2')
    expect(hero.text()).toContain('≥1')
    expect(hero.text()).toContain('Active repositories')
    expect(hero.text()).not.toContain('Token')
    const html = wrapper.html()
    expect(html.indexOf('data-testid="activity-prs"')).toBeLessThan(html.indexOf('data-testid="activity-commits"'))
    expect(wrapper.find('[data-testid="activity-buckets"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('1 repository needs PR sync')
    expect(wrapper.get('[role="alert"]').classes()).toContain('el-alert')
    expect(wrapper.get('[data-testid="activity-wide-details"]').classes()).not.toContain('overflow-x-auto')
    expect(wrapper.get('[data-testid="activity-commits"]').classes()).not.toContain('min-w-[640px]')
    const commit = wrapper.get('[data-testid="activity-commit-9-abcdef123456"]')
    expect(commit.classes()).toContain('sm:grid-cols-[minmax(10rem,1fr)_9rem_8rem]')
    expect(commit.classes()).not.toContain('min-w-[640px]')
  })

  it('renders a page load failure with an Element Plus alert', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivitySummary).mockRejectedValue(new Error('activity unavailable'))

    const { wrapper } = await mountView()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.classes()).toContain('el-alert')
    expect(alert.text()).toContain('Coding activity is temporarily unavailable.')
  })

  it('uses Element Plus external links and disclosure actions without changing their semantics', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivitySummary).mockResolvedValue(response('Alice', false, true) as any)
    const { wrapper } = await mountView()

    const pullRequestLink = wrapper.get('a[href="https://example.com/pr/88"]')
    expect(pullRequestLink.classes()).toContain('el-link')
    expect(pullRequestLink.attributes('target')).toBe('_blank')
    expect(pullRequestLink.attributes('rel')).toContain('noopener')
    expect(pullRequestLink.attributes('rel')).toContain('noreferrer')

    const commits = wrapper.findAll('button').find((button) => button.text() === 'Commits · 1')
    expect(commits?.classes()).toContain('el-button')
    expect(wrapper.get('[data-testid="activity-buckets"]').classes()).not.toContain('min-w-[640px]')
    expect(wrapper.get('[data-testid="activity-bucket-bucket-1"]').classes()).toContain('el-button')
  })

  it('suppresses an older range response after a newer request wins', async () => {
    const api = await import('@/api/activity')
    let resolveOld!: (value: any) => void
    const old = new Promise((resolve) => { resolveOld = resolve })
    vi.mocked(api.getActivitySummary)
      .mockReturnValueOnce(old as any)
      .mockResolvedValueOnce(response('Newest range') as any)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-range-7"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Newest range')
    resolveOld(response('Stale range'))
    await flushPromises()
    expect(wrapper.text()).toContain('Newest range')
    expect(wrapper.text()).not.toContain('Stale range')
  })

  it('uses the authorized member endpoint on member routes', async () => {
    const api = await import('@/api/activity')
    await mountView('/activity/members/7')
    expect(api.getActivityMember).toHaveBeenCalledWith(7, expect.objectContaining({ pr_limit: 20, commit_limit: 20, bucket_limit: 20 }))
    expect(api.getActivitySummary).not.toHaveBeenCalled()
  })

  it('reloads activity when the routed member changes in the same view instance', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityMember)
      .mockResolvedValueOnce(response('Alice') as any)
      .mockResolvedValueOnce(response('Bob') as any)

    const { wrapper, router } = await mountView('/activity/members/7')
    await router.push('/activity/members/8')
    await flushPromises()

    expect(api.getActivityMember).toHaveBeenNthCalledWith(2, 8, expect.objectContaining({
      pr_limit: 20,
      commit_limit: 20,
      bucket_limit: 20,
    }))
    expect(wrapper.text()).toContain('Bob')
    expect(wrapper.text()).not.toContain('Alice')
  })

  it('pages PRs independently without replacing commit or Bucket sections', async () => {
    const api = await import('@/api/activity')
    const first = response('Alice', false, true) as any
    first.data.data.prs.next_cursor = 'signed-pr-cursor'
    const next = response('Alice', false, true) as any
    next.data.data.prs = {
      items: [{ repo_config_id: 9, repo_name: 'acme/repo', pr_record_id: 22, scm_pr_id: 89, title: 'Second PR page', url: 'https://example.com/pr/89', status: 'open', commits: [] }],
    }
    next.data.data.commits.items[0].repo_name = 'must-not-replace-commits'
    next.data.data.buckets.items[0].bucket_id = 'must-not-replace-buckets'
    vi.mocked(api.getActivitySummary)
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(next)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-prs-next"]').trigger('click')
    await flushPromises()

    expect(api.getActivitySummary).toHaveBeenNthCalledWith(2, expect.objectContaining({
      pr_limit: 20,
      pr_cursor: 'signed-pr-cursor',
    }))
    expect(wrapper.get('[data-testid="activity-prs"]').text()).toContain('Second PR page')
    expect(wrapper.get('[data-testid="activity-commits"]').text()).toContain('acme/repo')
    expect(wrapper.get('[data-testid="activity-commits"]').text()).not.toContain('must-not-replace-commits')
    expect(wrapper.get('[data-testid="activity-buckets"]').text()).toContain('bucket-1')
    expect(wrapper.get('[data-testid="activity-buckets"]').text()).not.toContain('must-not-replace-buckets')
  })

  it('returns to an earlier PR page using the cursor saved for that page', async () => {
    const api = await import('@/api/activity')
    const first = response('Alice', false, false) as any
    first.data.data.prs.next_cursor = 'signed-pr-cursor'
    const second = response('Alice', false, false) as any
    second.data.data.prs = {
      items: [{ repo_config_id: 9, repo_name: 'acme/repo', pr_record_id: 22, scm_pr_id: 89, title: 'Second PR page', url: 'https://example.com/pr/89', status: 'open', commits: [] }],
    }
    vi.mocked(api.getActivitySummary)
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(second)
      .mockResolvedValueOnce(first)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-prs-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="activity-prs-previous"]').trigger('click')
    await flushPromises()

    expect(api.getActivitySummary).toHaveBeenNthCalledWith(3, expect.not.objectContaining({ pr_cursor: expect.anything() }))
    expect(wrapper.get('[data-testid="activity-prs"]').text()).toContain('Improve activity')
    expect(wrapper.get('[data-testid="activity-prs"]').text()).not.toContain('Second PR page')
  })

  it('loads complete Bucket evidence only after an authorized user expands it', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivitySummary).mockResolvedValue(response('Alice', false, true) as any)
    vi.mocked(api.getActivityBucket).mockResolvedValue({
      data: {
        data: {
          contract_version: 'activity-v1',
          bucket_id: 'bucket-1',
          owner_user_id: 7,
          tool: 'codex',
          model: 'gpt-5',
          observed_start_at: '2026-08-05T11:00:00Z',
          observed_end_at: '2026-08-05T12:00:00Z',
          tokens: {
            fresh_input_tokens: 100,
            cache_read_tokens: 200,
            cache_write_tokens: 300,
            output_tokens: 400,
            reasoning_tokens: 500,
            provider_total_tokens: 1500,
            processed_total_tokens: 1400,
          },
          token_quality: 'complete',
          coverage_gap_count: 0,
          extractor_version: 'codex-v2',
          normalization_version: 3,
          correlation_quality: 'request_id',
          revision: { revision_id: 'revision-1', sequence: 2, reason: 'commit_evidence', evidence_version: 'v2', restated_at: '2026-08-05T12:01:00Z', allocations: [] },
          request_ids: {
            state: 'retained',
            count: 1,
            evidence: [{ request_id: 'req_123', observed_at: '2026-08-05T11:30:00Z', transport: 'responses', failed: false }],
          },
        },
      },
    } as any)

    const { wrapper } = await mountView()
    expect(api.getActivityBucket).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="activity-bucket-bucket-1"]').trigger('click')
    await flushPromises()

    expect(api.getActivityBucket).toHaveBeenCalledOnce()
    expect(api.getActivityBucket).toHaveBeenCalledWith('bucket-1')
    const detail = wrapper.get('[data-testid="activity-bucket-detail-bucket-1"]')
    expect(detail.text()).toContain('Fresh input')
    expect(detail.text()).toContain('100')
    expect(detail.text()).toContain('Cache read')
    expect(detail.text()).toContain('Normalization version')
    expect(detail.text()).toContain('3')
    expect(detail.text()).toContain('Correlation quality')
    expect(detail.text()).toContain('request_id')
    expect(detail.text()).toContain('req_123')
  })

  it('shows a failed Bucket read with an Element Plus alert and keeps retry available', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivitySummary).mockResolvedValue(response('Alice', false, true) as any)
    vi.mocked(api.getActivityBucket).mockRejectedValueOnce(new Error('bucket unavailable'))
    const { wrapper } = await mountView()
    const bucket = wrapper.get('[data-testid="activity-bucket-bucket-1"]')

    await bucket.trigger('click')
    await flushPromises()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.classes()).toContain('el-alert')
    expect(alert.text()).toContain('Failed to load attribution detail.')
    expect(bucket.attributes('disabled')).toBeUndefined()

    await bucket.trigger('click')
    await flushPromises()
    expect(api.getActivityBucket).toHaveBeenCalledTimes(2)
  })

  it.each([
    ['expired', 'Request IDs expired after retention'],
    ['unlinked', 'No Request ID was linked'],
    ['unavailable', 'Correlation evidence is unavailable'],
  ] as const)('renders the %s Request ID state without inventing evidence', async (state, message) => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivitySummary).mockResolvedValue(response('Alice', false, true) as any)
    vi.mocked(api.getActivityBucket).mockResolvedValue({
      data: {
        data: {
          contract_version: 'activity-v1',
          bucket_id: 'bucket-1',
          owner_user_id: 7,
          tool: 'codex',
          model: 'gpt-5',
          observed_start_at: '2026-08-05T11:00:00Z',
          observed_end_at: '2026-08-05T12:00:00Z',
          tokens: {
            fresh_input_tokens: 100,
            cache_read_tokens: 200,
            cache_write_tokens: 300,
            output_tokens: 400,
            reasoning_tokens: 500,
            provider_total_tokens: 1500,
            processed_total_tokens: 1400,
          },
          token_quality: 'complete',
          coverage_gap_count: 0,
          extractor_version: 'codex-v2',
          normalization_version: 3,
          correlation_quality: state,
          revision: { revision_id: 'revision-1', sequence: 2, reason: 'commit_evidence', evidence_version: 'v2', restated_at: '2026-08-05T12:01:00Z', allocations: [] },
          request_ids: { state, count: 0, evidence: [] },
        },
      },
    } as any)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-bucket-bucket-1"]').trigger('click')
    await flushPromises()

    const detail = wrapper.get('[data-testid="activity-bucket-detail-bucket-1"]')
    expect(detail.text()).toContain('Automatically attributed')
    expect(detail.text()).toContain(message)
    expect(detail.text()).not.toContain('Request ID evidence')
  })
})
