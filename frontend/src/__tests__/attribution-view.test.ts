import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import AttributionView from '@/views/attribution/AttributionView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/attribution', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/attribution')>()),
  getAttributionReport: vi.fn(),
}))

const sampleReport = {
  from: '2026-08-01T00:00:00Z',
  to: '2026-08-08T00:00:00Z',
  measured_tokens: 150,
  bound_tokens: 100,
  unbound_tokens: 50,
  shared_tokens: 20,
  historical_advisory_tokens: 0,
  allocation_rate: 2 / 3,
  coverage_gap_count: 1,
  request_id_coverage_count: 1,
  bucket_count: 2,
  evidence: {
    measured_buckets: 2,
    historical_advisory_buckets: 0,
    invalid_buckets: 1,
    exact_correlation_buckets: 0,
    advisory_correlation_buckets: 1,
    unlinked_correlation_buckets: 1,
  },
  repositories: [{
    repo_config_id: 7,
    repo_key: 'repo:example',
    name: 'example/repo',
    tokens: 100,
    processed_tokens: 150,
    unbound_tokens: 50,
    shared_tokens: 20,
    inherited_tokens: 25,
    worktrees: ['workspace-a'],
    branches: ['feature/a'],
    commits: [{
      commit_sha: '0123456789abcdef',
      lineage: 'rebase',
      tokens: 100,
      inherited_tokens: 25,
      inherited_from_commit_shas: ['fedcba9876543210'],
      prs: [{ id: 9, scm_pr_id: 42, title: 'Compact ledger', url: 'https://example.com/pr/42', status: 'open' }],
    }],
  }],
  buckets: [{
    bucket_id: 'bucket-abcdefghijklmnop',
    tool: 'codex',
    model: 'gpt-test',
    observed_start_at: '2026-08-05T10:00:00Z',
    observed_end_at: '2026-08-05T10:01:00Z',
    tokens: {
      fresh_input_tokens: 70,
      cache_read_tokens: 30,
      cache_write_tokens: 0,
      output_tokens: 50,
      reasoning_tokens: 10,
      provider_total_tokens: 150,
      processed_total_tokens: 150,
    },
    request_count: 1,
    token_quality: 'measured',
    request_correlation_quality: 'advisory',
    request_id_coverage_count: 1,
    coverage_gap_count: 0,
    allocation_status: 'bound_auto',
    allocation_revision: 2,
    allocation_revision_reason: 'rewrite',
  }],
}

async function mountView(reportData: any = sampleReport) {
  const { getAttributionReport } = await import('@/api/attribution')
  ;(getAttributionReport as any).mockResolvedValue({ data: { data: reportData } })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/attribution', component: AttributionView }],
  })
  await router.push('/attribution')
  await router.isReady()
  const wrapper = mount(AttributionView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()
  return { wrapper, getAttributionReport }
}

describe('AttributionView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
  })

  it('shows a conserved seven-day repository to commit to PR ledger', async () => {
    const { wrapper, getAttributionReport } = await mountView()
    expect(getAttributionReport).toHaveBeenCalledTimes(1)
    const params = (getAttributionReport as any).mock.calls[0][0]
    expect(new Date(params.to).getTime() - new Date(params.from).getTime()).toBe(7 * 24 * 60 * 60 * 1000)
    expect(wrapper.get('[data-testid="attribution-measured"]').text()).toBe('150')
    expect(wrapper.get('[data-testid="attribution-conservation"]').text()).toContain('Conserved')
    expect(wrapper.text()).toContain('example/repo')
    expect(wrapper.text()).not.toContain('0123456789')

    await wrapper.get('[data-testid="attribution-repo-7"]').trigger('click')
    expect(wrapper.text()).toContain('0123456789')
    expect(wrapper.text()).toContain('#42 Compact ledger')
  })

  it('does not render raw prompts, paths, commands, or spans in the evidence table', async () => {
    const { wrapper } = await mountView()
    const text = wrapper.text()
    expect(text).toContain('Compact buckets only')
    expect(text).not.toContain('/Users/')
    expect(text).not.toContain('must-not-be-retained')
  })

  it('normalizes nullable compact collections instead of crashing the ledger', async () => {
    const { wrapper } = await mountView({
      ...sampleReport,
      repositories: [{
        ...sampleReport.repositories[0],
        worktrees: null,
        branches: null,
        commits: null,
      }],
      buckets: null,
    })

    expect(wrapper.get('[data-testid="attribution-measured"]').text()).toBe('150')
    expect(wrapper.text()).toContain('example/repo')
    expect(wrapper.text()).toContain('No commit-bound Tokens yet.')
  })
})
