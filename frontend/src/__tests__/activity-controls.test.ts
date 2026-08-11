import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import ActivityDateRange from '@/components/activity/ActivityDateRange.vue'
import RepositoryActivitySection from '@/components/activity/RepositoryActivitySection.vue'
import { useRepositoryActivity } from '@/composables/useRepositoryActivity'
import { setLocale } from '@/i18n'

vi.mock('@/composables/useRepositoryActivity', () => ({
  useRepositoryActivity: vi.fn(),
}))

describe('Activity controls', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('uses Element Plus range controls while preserving the exclusive custom end date', async () => {
    const wrapper = mount(ActivityDateRange, {
      props: {
        from: '2026-08-01T00:00:00Z',
        to: '2026-08-08T00:00:00Z',
        loading: false,
      },
    })

    expect(wrapper.get('[data-testid="activity-range-7"]').classes()).toContain('el-radio-button')
    expect(wrapper.get('[data-testid="activity-range-custom"]').classes()).toContain('el-radio-button')
    expect(wrapper.get('[data-testid="activity-range-refresh"]').classes()).toContain('el-button')

    await wrapper.get('[data-testid="activity-range-custom"]').trigger('click')
    const range = wrapper.get('[data-testid="activity-date-range"]')
    expect(range.get('[data-testid="activity-custom-panel"]')).toBeTruthy()
    const from = wrapper.get('input[data-testid="activity-custom-from"]')
    const to = wrapper.get('input[data-testid="activity-custom-to"]')
    expect(from.classes()).toContain('el-input__inner')
    expect(to.classes()).toContain('el-input__inner')
    await from.setValue('2026-08-01')
    await to.setValue('2026-08-03')
    const apply = wrapper.get('[data-testid="activity-range-apply"]')
    expect(apply.classes()).toContain('el-button')
    await apply.trigger('click')

    const changes = wrapper.emitted('change') ?? []
    const change = changes[changes.length - 1]?.[0] as { from: string; to: string }
    const changeFrom = new Date(change.from)
    const changeTo = new Date(change.to)
    expect([changeFrom.getFullYear(), changeFrom.getMonth(), changeFrom.getDate()]).toEqual([2026, 7, 1])
    expect([changeTo.getFullYear(), changeTo.getMonth(), changeTo.getDate()]).toEqual([2026, 7, 3])
  })

  it('explains and blocks an invalid custom range', async () => {
    const wrapper = mount(ActivityDateRange, { props: { from: '2026-08-01', to: '2026-08-08' } })
    await wrapper.get('[data-testid="activity-range-custom"]').trigger('click')
    await wrapper.get('input[data-testid="activity-custom-from"]').setValue('2026-01-01')
    await wrapper.get('input[data-testid="activity-custom-to"]').setValue('2026-08-08')

    expect(wrapper.get('[role="alert"]').text()).toContain('90 days')
    expect(wrapper.get('[data-testid="activity-range-apply"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="activity-range-apply"]').trigger('click')
    expect(wrapper.emitted('change')).toBeUndefined()
  })

  it('uses Element Plus for repository PR links and disclosure actions', async () => {
    vi.mocked(useRepositoryActivity).mockReturnValue({
      activity: ref({
        participating_members: 1,
        metrics: {
          participating_prs: { value: 1, lower_bound: false },
          merged_prs: { value: 0, lower_bound: false },
          commit_count: 1,
          latest_activity: '2026-08-05T12:00:00Z',
        },
        sync_coverage: { complete: true, affected_repositories: 0 },
        prs: {
          items: [{
            repo_config_id: 9,
            pr_record_id: 21,
            scm_pr_id: 88,
            title: 'Repository activity PR',
            url: 'https://example.com/pr/88',
            commits: [{ repo_config_id: 9, commit_sha: 'abcdef123456' }],
          }],
        },
        commits: { items: [] },
      }),
      range: ref({ from: '2026-08-01T00:00:00Z', to: '2026-08-08T00:00:00Z' }),
      loading: ref(false),
      prLoading: ref(false),
      error: ref(''),
      prPageIndex: ref(0),
      load: vi.fn(),
      loadNextPRPage: vi.fn(),
      loadPreviousPRPage: vi.fn(),
      selectRange: vi.fn(),
    } as any)
    const wrapper = mount(RepositoryActivitySection, { props: { repoId: 9 } })

    const link = wrapper.get('a[href="https://example.com/pr/88"]')
    expect(link.classes()).toContain('el-link')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toContain('noopener')
    expect(link.attributes('rel')).toContain('noreferrer')
    const toggle = wrapper.get('[data-testid="repo-activity-pr-toggle-21"]')
    expect(toggle.classes()).toContain('el-button')

    await toggle.trigger('click')
    expect(wrapper.get('[data-testid="repo-activity-pr-commits-21"]').text()).toContain('abcdef1234')
  })

  it('renders a repository Activity load failure as an Element Plus alert', () => {
    vi.mocked(useRepositoryActivity).mockReturnValue({
      activity: ref(null),
      range: ref({ from: '2026-08-01T00:00:00Z', to: '2026-08-08T00:00:00Z' }),
      loading: ref(false),
      prLoading: ref(false),
      error: ref('repository activity unavailable'),
      prPageIndex: ref(0),
      load: vi.fn(),
      loadNextPRPage: vi.fn(),
      loadPreviousPRPage: vi.fn(),
      selectRange: vi.fn(),
    } as any)
    const wrapper = mount(RepositoryActivitySection, { props: { repoId: 9 } })

    const alert = wrapper.get('[role="alert"]')
    expect(alert.classes()).toContain('el-alert')
    expect(alert.text()).toContain('Coding activity is temporarily unavailable.')
  })

  it('uses Element Plus loading and empty states for repository Activity', () => {
    vi.mocked(useRepositoryActivity).mockReturnValue({
      activity: ref(null),
      range: ref({ from: '2026-08-01T00:00:00Z', to: '2026-08-08T00:00:00Z' }),
      loading: ref(true),
      prLoading: ref(false),
      error: ref(false),
      prPageIndex: ref(0),
      load: vi.fn(),
      loadNextPRPage: vi.fn(),
      loadPreviousPRPage: vi.fn(),
      selectRange: vi.fn(),
    } as any)
    const loading = mount(RepositoryActivitySection, { props: { repoId: 9 } })
    expect(loading.find('.el-skeleton').exists()).toBe(true)
    loading.unmount()

    vi.mocked(useRepositoryActivity).mockReturnValue({
      activity: ref({
        participating_members: 0,
        metrics: {
          participating_prs: { value: 0, lower_bound: false },
          merged_prs: { value: 0, lower_bound: false },
          commit_count: 0,
        },
        sync_coverage: { complete: true, affected_repositories: 0 },
        prs: { items: [] },
        commits: { items: [] },
      }),
      range: ref({ from: '2026-08-01T00:00:00Z', to: '2026-08-08T00:00:00Z' }),
      loading: ref(false),
      prLoading: ref(false),
      error: ref(false),
      prPageIndex: ref(0),
      load: vi.fn(),
      loadNextPRPage: vi.fn(),
      loadPreviousPRPage: vi.fn(),
      selectRange: vi.fn(),
    } as any)
    const empty = mount(RepositoryActivitySection, { props: { repoId: 9 } })
    expect(empty.get('[data-testid="repo-activity-prs"]').find('.el-empty').exists()).toBe(true)
    expect(empty.get('[data-testid="repo-activity-commits"]').find('.el-empty').exists()).toBe(true)
  })

  it('keeps repository Activity visible while surfacing a failed refresh with retry', async () => {
    const load = vi.fn()
    vi.mocked(useRepositoryActivity).mockReturnValue({
      activity: ref({
        participating_members: 1,
        metrics: {
          participating_prs: { value: 1, lower_bound: false },
          merged_prs: { value: 0, lower_bound: false },
          commit_count: 1,
          latest_activity: '2026-08-05T12:00:00Z',
        },
        sync_coverage: { complete: true, affected_repositories: 0 },
        prs: { items: [] },
        commits: { items: [] },
      }),
      range: ref({ from: '2026-08-01T00:00:00Z', to: '2026-08-08T00:00:00Z' }),
      loading: ref(false),
      prLoading: ref(false),
      error: ref(true),
      prPageIndex: ref(0),
      load,
      loadNextPRPage: vi.fn(),
      loadPreviousPRPage: vi.fn(),
      selectRange: vi.fn(),
    } as any)
    const wrapper = mount(RepositoryActivitySection, { props: { repoId: 9 } })

    expect(wrapper.get('[data-testid="repo-activity"]')).toBeTruthy()
    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('Coding activity is temporarily unavailable.')
    const retry = wrapper.findAll('button').find((button) => button.text() === 'Retry')
    expect(retry).toBeTruthy()

    load.mockClear()
    await retry!.trigger('click')
    expect(load).toHaveBeenCalledOnce()
  })
})
