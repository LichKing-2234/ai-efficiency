// Preview fixture. Replaces the real composable through a Vite alias so the
// page renders with no backend, no router, and no login. Delete with the
// preview/ directory.
import { computed, ref } from 'vue'
import type {
  ActivityV2Overview, ActivityV2Page, ActivityV2PullRequestRow,
  ActivityV2RepositoryRow, ActivityV2TrendPoint,
} from '@/types/activity'

export type PreviewDataset = 'codexOnly' | 'mixed'
export const dataset = ref<PreviewDataset>('mixed')

function days(count: number): string[] {
  const out: string[] = []
  const end = new Date(2026, 7, 31)
  for (let i = count - 1; i >= 0; i -= 1) {
    const day = new Date(end)
    day.setDate(day.getDate() - i)
    out.push(`${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, '0')}-${String(day.getDate()).padStart(2, '0')}`)
  }
  return out
}

const credited = (index: number, total: number) => index < total - 5 ? 0 : [0.31, 0.08, 0.94, 0.12, 1.77][index - (total - 5)]
const tokened = (index: number, total: number) => index < total - 4 ? 0 : [0, 41200, 18200, 122400][index - (total - 4)]

function trend(withCredit: boolean): ActivityV2TrendPoint[] {
  const labels = days(30)
  return labels.map((date, index) => ({
    date,
    direct_tokens: tokened(index, labels.length),
    shared_tokens: 0,
    involved_tokens: 0,
    direct_credit: withCredit ? credited(index, labels.length) : 0,
    shared_credit: 0,
    involved_credit: 0,
  }))
}

function overview(withCredit: boolean): ActivityV2Overview {
  return {
    contract_version: 'activity-v2',
    scope_version: 'preview',
    from: '2026-08-02', to: '2026-08-31', timezone: 'Asia/Shanghai',
    committed_tokens: 181800,
    committed_credit: withCredit ? 3.22 : 0,
    claim_coverage: { complete: true, lower_bound: false },
    scm_coverage: { complete: true } as ActivityV2Overview['scm_coverage'],
    ratio: {
      state: 'exact', committed_tokens: 181800, total_tokens: 430_000_000,
      percent: 0.0423, percentage_point_change: 0.02,
    },
    credit_ratio: withCredit
      ? { state: 'exact' as const, committed_credit: 3.22, total_credit: 11.48, percent: 28.05 }
      : { state: 'complete_zero_usage' as const, committed_credit: 0, total_credit: 0 },
    trend: trend(withCredit),
    readiness: { state: 'active', first_accepted_at: '2026-08-26T08:16:22Z' },
  }
}

function repositories(withCredit: boolean): ActivityV2RepositoryRow[] {
  return [
    { repo_config_id: 98, name: '~huangshen_agora.io/ae-sandbox', direct_tokens: 181800, direct_share: 100, shared_tokens: 0,
      direct_credit: withCredit ? 0.94 : 0, shared_credit: 0, credit_share: withCredit ? 29.2 : undefined,
      token_change: 181800, credit_change: withCredit ? 0.31 : undefined },
    { repo_config_id: 41, name: 'agora/ai-efficiency', direct_tokens: 0, direct_share: 0, shared_tokens: 0,
      direct_credit: withCredit ? 1.77 : 0, shared_credit: 0, credit_share: withCredit ? 54.9 : undefined,
      token_change: 0, credit_change: withCredit ? 1.24 : undefined },
    { repo_config_id: 55, name: 'agora/rtc-sdk-tools', direct_tokens: 0, direct_share: 0, shared_tokens: 0,
      direct_credit: withCredit ? 0.51 : 0, shared_credit: 0, credit_share: withCredit ? 15.8 : undefined,
      token_change: 0, credit_change: withCredit ? -0.08 : undefined },
  ]
}

function pullRequests(withCredit: boolean): ActivityV2PullRequestRow[] {
  return [
    { pr_record_id: 7, repo_config_id: 41, repository_name: 'agora/ai-efficiency', scm_pr_id: 412,
      title: 'fix(backend): carry credit past the claim entry', url: '#', status: 'open',
      involved_tokens: 0, involved_credit: withCredit ? 1.24 : 0, overlap_state: 'direct',
      token_change: 0, credit_change: withCredit ? 1.24 : undefined, commits: [{ repo_config_id: 41, commit_sha: '447f735d1c0a' }] },
  ]
}

export function useActivityAnalytics() {
  const withCredit = computed(() => dataset.value === 'mixed')
  const range = ref({ from: '2026-08-02', to: '2026-08-31' })
  const noop = () => undefined
  const page = <T,>(items: T[]): ActivityV2Page<T> => ({ items })
  return {
    range,
    ratioOverview: computed(() => overview(withCredit.value)),
    trendOverview: computed(() => overview(withCredit.value)),
    repoTop: computed(() => repositories(withCredit.value)),
    prTop: computed(() => pullRequests(withCredit.value)),
    repositories: computed(() => page(repositories(withCredit.value))),
    pullRequests: computed(() => page(pullRequests(withCredit.value))),
    loading: ref({ ratio: false, trend: false, repoTop: false, prTop: false, repositories: false, pullRequests: false }),
    errors: ref({ ratio: '', trend: '', repoTop: '', prTop: '', repositories: '', pullRequests: '' }),
    activeList: ref<'repositories' | 'pullRequests'>('repositories'),
    search: ref(''), sort: ref<'tokens' | 'name'>('tokens'),
    repoPage: ref(0), prPage: ref(0),
    expandedPR: computed(() => null), selectedRepoID: computed(() => null), selectedPRID: computed(() => null),
    overallQuery: computed(() => ({ scope: 'personal' as const, from: range.value.from, to: range.value.to, timezone: 'Asia/Shanghai' })),
    refreshing: computed(() => false), selectedPRRow: computed(() => null),
    loadRatio: noop, loadTrend: noop, loadRepoTop: noop, loadPRTop: noop, loadRepositories: noop, loadPullRequests: noop,
    refresh: noop, selectRange: noop, selectRepository: noop, selectPullRequest: noop, clearFilter: noop,
    applyListControls: noop, nextPage: noop, previousPage: noop,
  }
}
