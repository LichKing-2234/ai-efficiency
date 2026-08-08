import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ActivityTeamView from '@/views/activity/ActivityTeamView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/activity', () => ({
  getActivityTeam: vi.fn(),
  normalizeTeam: (value: any) => ({
    ...value,
    members: { ...value.members, items: value.members?.items ?? [] },
  }),
}))

function teamResponse() {
  return {
    data: {
      data: {
        contract_version: 'activity-v1',
        scope_version: 'scope-1',
        window: { from: '2026-07-07T00:00:00Z', to: '2026-08-06T00:00:00Z' },
        team: {
          external_id: 'team-alpha',
          name: 'Team Alpha',
          display_path: 'Engineering / Team Alpha',
          member_count: 2,
        },
        active_members: 2,
        metrics: {
          participating_prs: { value: 2, lower_bound: true },
          merged_prs: { value: 1, lower_bound: true },
          active_repositories: 1,
          commit_count: 3,
          latest_activity: '2026-08-05T12:00:00Z',
        },
        sync_coverage: {
          complete: false,
          affected_repositories: 1,
          unsynced_repositories: 1,
          stale_repositories: 0,
          partially_synced_repositories: 0,
          failed_repositories: 0,
        },
        members: {
          items: [
            {
              member: { user_id: 7, display_name: 'Alice', email: 'alice@example.com', department_external_ids: ['team-alpha'] },
              available: true,
              metrics: {
                participating_prs: { value: 2, lower_bound: true },
                merged_prs: { value: 1, lower_bound: true },
                active_repositories: 1,
                commit_count: 3,
                latest_activity: '2026-08-05T12:00:00Z',
              },
              quality: { measured_buckets: 1, unbound_buckets: 0, multi_repo_shared_buckets: 0, invalid_token_facts: 0, historical_advisory_facts: 0, coverage_gap_count: 0 },
            },
            {
              member: { user_id: 0, directory_member_external_id: 'directory-bob', display_name: 'Bob', email: 'bob@example.org', department_external_ids: ['team-alpha'] },
              available: false,
              metrics: {
                participating_prs: { value: 0, lower_bound: false },
                merged_prs: { value: 0, lower_bound: false },
                active_repositories: 0,
                commit_count: 0,
              },
              quality: { measured_buckets: 0, unbound_buckets: 0, multi_repo_shared_buckets: 0, invalid_token_facts: 0, historical_advisory_facts: 0, coverage_gap_count: 0 },
            },
          ],
        },
      },
    },
  }
}

async function mountView(waitForRequests = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/activity/teams/:team_id', component: ActivityTeamView },
      { path: '/activity/members/:user_id', component: { template: '<div />' } },
    ],
  })
  await router.push('/activity/teams/team-alpha')
  await router.isReady()
  const wrapper = mount(ActivityTeamView, {
    global: {
      plugins: [router],
      stubs: { AppLayout: { template: '<main><slot /></main>' } },
    },
  })
  if (waitForRequests) await flushPromises()
  return { wrapper, router }
}

describe('ActivityTeamView', () => {
  beforeEach(async () => {
    await setLocale('en-US')
    vi.clearAllMocks()
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityTeam).mockResolvedValue(teamResponse() as any)
  })

  it('shows a 30-day team summary and member activity without ranking or Token columns', async () => {
    const api = await import('@/api/activity')
    const { wrapper } = await mountView()

    expect(api.getActivityTeam).toHaveBeenCalledOnce()
    expect(api.getActivityTeam).toHaveBeenCalledWith('team-alpha', expect.objectContaining({ limit: 50 }))
    const params = vi.mocked(api.getActivityTeam).mock.calls[0][1]!
    expect(new Date(params.to!).getTime() - new Date(params.from!).getTime()).toBe(30 * 24 * 60 * 60 * 1000)

    const summary = wrapper.get('[data-testid="activity-team-summary"]')
    expect(summary.text()).toContain('Active members')
    expect(summary.text()).toContain('≥2')
    expect(summary.text()).toContain('≥1')
    expect(summary.text()).not.toContain('Token')
    expect(summary.text()).not.toContain('Rank')
    expect(summary.text()).not.toContain('Cost')

    const alice = wrapper.get('[data-testid="activity-member-7"]')
    expect(alice.text()).toContain('Alice')
    expect(alice.attributes('href')).toBe('/activity/members/7')
    const bob = wrapper.get('[data-testid="activity-member-directory-bob"]')
    expect(bob.text()).toContain('Bob')
    expect(bob.text()).toContain('No activity data')
    expect(bob.attributes('href')).toBeUndefined()
    expect(wrapper.text()).toContain('1 repository needs PR sync')
  })

  it('pages members independently without replacing the team summary', async () => {
    const api = await import('@/api/activity')
    const first = teamResponse() as any
    first.data.data.members.next_cursor = 'signed-member-cursor'
    const next = teamResponse() as any
    next.data.data.active_members = 99
    next.data.data.metrics.participating_prs = { value: 99, lower_bound: false }
    next.data.data.members = {
      items: [{
        member: { user_id: 8, display_name: 'Carol', email: 'carol@example.net', department_external_ids: ['team-alpha'] },
        available: true,
        metrics: {
          participating_prs: { value: 1, lower_bound: false },
          merged_prs: { value: 0, lower_bound: false },
          active_repositories: 1,
          commit_count: 1,
          latest_activity: '2026-08-06T12:00:00Z',
        },
        quality: { measured_buckets: 1, unbound_buckets: 0, multi_repo_shared_buckets: 0, invalid_token_facts: 0, historical_advisory_facts: 0, coverage_gap_count: 0 },
      }],
    }
    vi.mocked(api.getActivityTeam)
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(next)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-team-members-next"]').trigger('click')
    await flushPromises()

    expect(api.getActivityTeam).toHaveBeenNthCalledWith(2, 'team-alpha', expect.objectContaining({
      limit: 50,
      cursor: 'signed-member-cursor',
    }))
    expect(wrapper.get('[data-testid="activity-team-summary"]').text()).toContain('≥2')
    expect(wrapper.get('[data-testid="activity-team-summary"]').text()).not.toContain('99')
    expect(wrapper.get('[data-testid="activity-member-8"]').text()).toContain('Carol')
    expect(wrapper.find('[data-testid="activity-member-7"]').exists()).toBe(false)
  })

  it('returns to the first member page with the cursor saved for that page', async () => {
    const api = await import('@/api/activity')
    const first = teamResponse() as any
    first.data.data.members.next_cursor = 'signed-member-cursor'
    const second = teamResponse() as any
    second.data.data.members = {
      items: [{
        member: { user_id: 8, display_name: 'Carol', email: 'carol@example.net', department_external_ids: ['team-alpha'] },
        available: true,
        metrics: {
          participating_prs: { value: 1, lower_bound: false },
          merged_prs: { value: 0, lower_bound: false },
          active_repositories: 1,
          commit_count: 1,
        },
        quality: { measured_buckets: 1, unbound_buckets: 0, multi_repo_shared_buckets: 0, invalid_token_facts: 0, historical_advisory_facts: 0, coverage_gap_count: 0 },
      }],
    }
    vi.mocked(api.getActivityTeam)
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(second)
      .mockResolvedValueOnce(first)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-team-members-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="activity-team-members-previous"]').trigger('click')
    await flushPromises()

    expect(api.getActivityTeam).toHaveBeenNthCalledWith(3, 'team-alpha', expect.not.objectContaining({ cursor: expect.anything() }))
    expect(wrapper.get('[data-testid="activity-member-7"]').text()).toContain('Alice')
    expect(wrapper.find('[data-testid="activity-member-8"]').exists()).toBe(false)
  })

  it('loads a newly routed team and ignores the previous team response when it arrives late', async () => {
    const api = await import('@/api/activity')
    let resolveAlpha!: (value: any) => void
    const alpha = new Promise((resolve) => { resolveAlpha = resolve })
    const beta = teamResponse() as any
    beta.data.data.team = {
      external_id: 'team-beta',
      name: 'Team Beta',
      display_path: 'Engineering / Team Beta',
      member_count: 1,
    }
    beta.data.data.members.items = [{
      member: { user_id: 9, display_name: 'Beta Member', email: 'beta@example.com', department_external_ids: ['team-beta'] },
      available: true,
      metrics: {
        participating_prs: { value: 1, lower_bound: false },
        merged_prs: { value: 0, lower_bound: false },
        active_repositories: 1,
        commit_count: 1,
      },
      quality: { measured_buckets: 1, unbound_buckets: 0, multi_repo_shared_buckets: 0, invalid_token_facts: 0, historical_advisory_facts: 0, coverage_gap_count: 0 },
    }]
    vi.mocked(api.getActivityTeam)
      .mockReturnValueOnce(alpha as any)
      .mockResolvedValueOnce(beta)

    const { wrapper, router } = await mountView(false)
    await router.push('/activity/teams/team-beta')
    await flushPromises()

    expect(api.getActivityTeam).toHaveBeenNthCalledWith(2, 'team-beta', expect.objectContaining({ limit: 50 }))
    expect(wrapper.text()).toContain('Team Beta')
    expect(wrapper.text()).toContain('Beta Member')

    resolveAlpha(teamResponse())
    await flushPromises()
    expect(wrapper.text()).toContain('Team Beta')
    expect(wrapper.text()).not.toContain('Team Alpha')
  })

  it('restarts the current team member list when its snapshot cursor expires', async () => {
    const api = await import('@/api/activity')
    const first = teamResponse() as any
    first.data.data.members.next_cursor = 'expired-member-cursor'
    const refreshed = teamResponse() as any
    refreshed.data.data.members.items[0].member.display_name = 'Fresh Alice'
    vi.mocked(api.getActivityTeam)
      .mockResolvedValueOnce(first)
      .mockRejectedValueOnce({ response: { status: 409, data: { message: 'snapshot_expired' } } })
      .mockResolvedValueOnce(refreshed)

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-team-members-next"]').trigger('click')
    await flushPromises()

    expect(api.getActivityTeam).toHaveBeenNthCalledWith(2, 'team-alpha', expect.objectContaining({
      cursor: 'expired-member-cursor',
    }))
    expect(api.getActivityTeam).toHaveBeenNthCalledWith(3, 'team-alpha', expect.not.objectContaining({
      cursor: expect.anything(),
    }))
    expect(wrapper.text()).toContain('Fresh Alice')
    expect(wrapper.find('[data-testid="activity-team-members-previous"]').exists()).toBe(false)
  })
})
