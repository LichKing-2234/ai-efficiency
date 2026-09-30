<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import ActivityDateRange from '@/components/activity/ActivityDateRange.vue'
import CursorPager from '@/components/CursorPager.vue'
import { useWideContentLayout } from '@/composables/useMediaQuery'
import { useActivityAnalytics } from '@/composables/useActivityAnalytics'
import { useI18n } from '@/i18n'
import { activityV2Text, activityV2TextKeys, type ActivityV2TextKey } from './activityV2Text'
import type { MessageKey } from '@/i18n'
import type {
  ActivityV2PullRequestRow,
  ActivityV2RepositoryRow,
  ActivityV2Scope,
} from '@/types/activity'
import type { ChartData, ChartOptions } from 'chart.js'

const props = defineProps<{
  scope: ActivityV2Scope
  subjectUserId?: number
  teamId?: string
}>()

const DoughnutChartCanvas = defineAsyncComponent(() => import('@/components/charts/DoughnutChartCanvas.vue'))
const LineChartCanvas = defineAsyncComponent(() => import('@/components/charts/LineChartCanvas.vue'))
const { locale, t: baseT } = useI18n()
function t(key: string, params?: Record<string, string | number>) {
  const short = key.startsWith('activity.') ? key.slice('activity.'.length) : key
  return activityV2TextKeys.has(short as ActivityV2TextKey)
    ? activityV2Text(locale.value, short as ActivityV2TextKey, params)
    : baseT(key as MessageKey, params)
}
const wide = useWideContentLayout()
const {
  range, ratioOverview, trendOverview, repoTop, prTop, repositories, pullRequests,
  loading, errors, activeList, search, sort, repoPage, prPage, expandedPR,
  selectedRepoID, selectedPRID, overallQuery, refreshing, selectedPRRow,
  loadRatio, loadTrend, loadRepoTop, loadPRTop, loadRepositories, loadPullRequests,
  refresh, selectRange, selectRepository, selectPullRequest, clearFilter,
  applyListControls, nextPage, previousPage,
} = useActivityAnalytics(props, () => t('activity.v2LoadFailed'))
// Token and credit are independent units that are never converted to each
// other, so the page reads one of them at a time. Every amount below goes
// through these accessors rather than branching at each render site.
const unit = ref<'token' | 'credit'>('token')
const isCredit = computed(() => unit.value === 'credit')
const repoPrimary = (row: ActivityV2RepositoryRow) => isCredit.value ? row.direct_credit : row.direct_tokens
const repoShared = (row: ActivityV2RepositoryRow) => isCredit.value ? row.shared_credit : row.shared_tokens
const repoShare = (row: ActivityV2RepositoryRow) => isCredit.value ? row.credit_share : row.direct_share
const repoChange = (row: ActivityV2RepositoryRow) => isCredit.value ? row.credit_change : row.token_change
const prPrimary = (row: ActivityV2PullRequestRow) => isCredit.value ? row.involved_credit : row.involved_tokens
const prChange = (row: ActivityV2PullRequestRow) => isCredit.value ? row.credit_change : row.token_change

// The active ratio is normalized so the card renders one shape. Its two sources
// differ in authority, not in structure: the Token denominator is the gateway's
// billing record, the credit denominator is what the agent reported locally.
const activeRatio = computed(() => {
  const overview = ratioOverview.value
  if (!overview) return null
  if (isCredit.value) {
    // No period-over-period change: the credit denominator has no comparison
    // contract behind it, so the card shows the share without one.
    const value = overview.credit_ratio
    return { state: value.state, committed: value.committed_credit, total: value.total_credit, percent: value.percent, change: undefined as number | undefined }
  }
  const value = overview.ratio
  return { state: value.state, committed: value.committed_tokens, total: value.total_tokens, percent: value.percent, change: value.percentage_point_change }
})

const topRepositories = computed(() => repoTop.value.slice(0, 5))
const topPullRequests = computed(() => prTop.value.slice(0, 5))
const maxRepoAmount = computed(() => Math.max(1, ...topRepositories.value.map(repoPrimary)))
const maxPRAmount = computed(() => Math.max(1, ...topPullRequests.value.map(prPrimary)))
const ratioChartable = computed(() => activeRatio.value?.percent != null && activeRatio.value.total != null && activeRatio.value.total > 0)
const trendChartable = computed(() => Boolean(trendOverview.value?.trend.some((point) => isCredit.value
  ? point.direct_credit || point.shared_credit || point.involved_credit
  : point.direct_tokens || point.shared_tokens || point.involved_tokens)))

const ratioData = computed<ChartData<'doughnut'>>(() => {
  const committed = activeRatio.value?.committed ?? 0
  const total = activeRatio.value?.total ?? 0
  return {
    labels: [t('activity.usedForCommittedCode'), isCredit.value ? t('activity.otherCredit') : t('activity.otherToken')],
    datasets: [{ data: [committed, Math.max(0, total - committed)], backgroundColor: ['#2563eb', '#cbd5e1'], borderWidth: 0 }],
  }
})
const ratioOptions: ChartOptions<'doughnut'> = { responsive: true, maintainAspectRatio: false, cutout: '68%', plugins: { legend: { position: 'bottom' } } }
const trendData = computed<ChartData<'line'>>(() => {
  const points = trendOverview.value?.trend ?? []
  const filtered = selectedRepoID.value || selectedPRID.value
  const primary = points.map((point) => selectedPRID.value
    ? (isCredit.value ? point.involved_credit : point.involved_tokens)
    : (isCredit.value ? point.direct_credit : point.direct_tokens))
  const datasets: ChartData<'line'>['datasets'] = [{
    label: selectedPRID.value ? t(isCredit.value ? 'activity.involvedCredit' : 'activity.involvedToken') : t(isCredit.value ? 'activity.committedCredit' : 'activity.committedToken'),
    data: primary,
    borderColor: '#2563eb',
    backgroundColor: 'rgba(37,99,235,.12)',
    fill: true,
    tension: 0.25,
  }]
  if (selectedRepoID.value) datasets.push({ label: t('activity.sharedParticipation'), data: points.map((point) => isCredit.value ? point.shared_credit : point.shared_tokens), borderColor: '#d97706', borderDash: [6, 4], tension: 0.25 })
  return { labels: points.map((point) => point.date), datasets: filtered ? datasets : datasets.slice(0, 1) }
})
const trendOptions: ChartOptions<'line'> = { responsive: true, maintainAspectRatio: false, interaction: { intersect: false, mode: 'index' }, plugins: { legend: { display: Boolean(selectedRepoID.value) } }, scales: { y: { beginAtZero: true } } }

function token(value: number) { return new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(value) }
// Credit amounts are small — a Kiro turn costs hundredths — so the compact
// Token format would render 0.0783 as "0.1" and 0.0083 as "0", losing the value
// and making a real amount indistinguishable from no data.
function credit(value: number) { return new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 }).format(value) }
function amount(value: number) { return isCredit.value ? credit(value) : token(value) }
function percent(value?: number) {
  if (value == null) return '—'
  if (value === 0) return '0%'
  if (value > 0 && value < 0.01) return '<0.01%'
  return `${value.toFixed(value < 1 ? 2 : 1)}%`
}
function amountChange(value?: number) { return value == null ? '' : `${value > 0 ? '+' : ''}${amount(value)}` }
function barWidth(value: number, max: number) { return `${Math.max(3, value / max * 100)}%` }

</script>

<template>
  <div class="min-w-0 space-y-5" data-testid="activity-v2-analytics">
    <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
      <div v-if="selectedRepoID || selectedPRID" class="min-w-0">
        <ElTag closable size="large" data-testid="activity-filter-chip" @close="clearFilter">
          {{ selectedPRID ? t('activity.filteredPullRequest') : t('activity.filteredRepository') }}
        </ElTag>
      </div>
      <div v-else />
      <div class="flex flex-wrap items-center gap-3">
        <ElRadioGroup v-model="unit" size="default" :aria-label="t('activity.unitSwitchLabel')" data-testid="activity-unit-switch">
          <ElRadioButton value="token">{{ t('activity.unitToken') }}</ElRadioButton>
          <ElRadioButton value="credit">{{ t('activity.unitCredit') }}</ElRadioButton>
        </ElRadioGroup>
        <ActivityDateRange :from="range.from" :to="range.to" :loading="refreshing" @change="selectRange" @refresh="refresh" />
      </div>
    </div>

    <div class="grid min-w-0 gap-4 xl:grid-cols-2">
      <section class="min-w-0 rounded-xl border border-slate-200 bg-white p-5" aria-labelledby="activity-ratio-heading">
        <div class="flex items-start justify-between gap-3">
          <div><h2 id="activity-ratio-heading" class="font-semibold text-slate-950">{{ t(isCredit ? 'activity.codeCreditRatio' : 'activity.codeTokenRatio') }}</h2><p class="mt-1 text-sm text-slate-500">{{ t(isCredit ? 'activity.codeCreditRatioHelp' : 'activity.codeTokenRatioHelp') }}</p></div>
          <ElButton :icon="Refresh" circle text :loading="loading.ratio" :aria-label="t('activity.retryRatio')" @click="loadRatio(false)" />
        </div>
        <ElAlert v-if="errors.ratio && ratioOverview" class="mt-4" type="error" :closable="false" :title="errors.ratio" show-icon />
        <ElAlert v-if="errors.ratio && !ratioOverview" class="mt-4" type="error" :closable="false" :title="errors.ratio" show-icon />
        <div v-else-if="loading.ratio && !ratioOverview" role="status" class="h-44 animate-pulse rounded-lg bg-slate-100" />
        <div v-else-if="ratioOverview" class="mt-4 grid items-center gap-4 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <div class="relative h-44" :data-testid="ratioChartable ? 'activity-ratio-chart' : undefined">
            <DoughnutChartCanvas v-if="ratioChartable" :data="ratioData" :options="ratioOptions" />
            <div v-else class="flex h-full items-center justify-center rounded-full border-slate-100 text-center text-sm text-slate-500" style="border-width: 18px">{{ t('activity.ratioUnavailableShort') }}</div>
          </div>
          <div>
            <!-- The headline reserves its tallest variant's height: a unit with
                 no usage this period renders one line where a percentage
                 renders two, and the card must not resize when the reader
                 switches units. -->
            <div class="flex min-h-[4.25rem] flex-col justify-center">
            <p v-if="activeRatio?.state === 'complete_zero_usage'" class="text-lg font-semibold text-slate-950">{{ t(isCredit ? 'activity.noCreditUsage' : 'activity.noAIUsage') }}</p>
            <template v-else-if="activeRatio?.percent != null">
              <p class="text-3xl font-semibold tracking-tight text-slate-950">{{ activeRatio.state === 'lower_bound' ? '≥' : '' }}{{ percent(activeRatio.percent) }}</p>
              <p v-if="activeRatio.change != null" class="mt-1 text-sm text-slate-600">{{ t('activity.percentagePointChange', { value: activeRatio.change > 0 ? `+${activeRatio.change.toFixed(1)}` : activeRatio.change.toFixed(1) }) }}</p>
            </template>
            <p v-else class="text-sm font-medium text-amber-700">{{ t('activity.ratioUnavailable') }}</p>
            </div>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div><dt class="text-slate-500">{{ t('activity.usedForCommittedCode') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ amount(activeRatio?.committed ?? 0) }}</dd></div>
              <div><dt class="text-slate-500">{{ t(isCredit ? 'activity.otherCredit' : 'activity.otherToken') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ activeRatio?.total == null ? '—' : amount(Math.max(0, activeRatio.total - activeRatio.committed)) }}</dd></div>
            </dl>
            <p class="mt-3 text-xs leading-5 text-slate-500">
              <span class="block font-medium text-slate-600" data-testid="activity-unit-source">{{ t('activity.unitSourceHelp') }}</span>
              {{ t(isCredit ? 'activity.otherCreditHelp' : 'activity.otherTokenHelp') }}
            </p>
          </div>
        </div>
      </section>

      <section class="min-w-0 rounded-xl border border-slate-200 bg-white p-5" aria-labelledby="activity-trend-heading">
        <div class="flex items-start justify-between gap-3"><div><h2 id="activity-trend-heading" class="font-semibold text-slate-950">{{ t(isCredit ? 'activity.creditTrend' : 'activity.committedTrend') }}</h2><p class="mt-1 text-sm text-slate-500">{{ t('activity.localDays', { timezone: overallQuery.timezone }) }}</p></div><ElButton :icon="Refresh" circle text :loading="loading.trend" :aria-label="t('activity.retryTrend')" @click="loadTrend(false)" /></div>
        <ElAlert v-if="errors.trend && trendOverview" class="mt-4" type="error" :closable="false" :title="errors.trend" show-icon />
        <ElAlert v-if="errors.trend && !trendOverview" class="mt-4" type="error" :closable="false" :title="errors.trend" show-icon />
        <div v-else-if="loading.trend && !trendOverview" role="status" class="mt-4 h-44 animate-pulse rounded-lg bg-slate-100" />
        <div v-else-if="trendChartable" class="mt-4 h-44" data-testid="activity-trend-chart"><LineChartCanvas :data="trendData" :options="trendOptions" /></div>
        <div v-else class="mt-4 flex h-44 items-center justify-center text-sm text-slate-500">{{ selectedRepoID || selectedPRID ? t('activity.noFilteredData') : t(isCredit ? 'activity.noCommittedCreditData' : 'activity.noCommittedData') }}</div>
      </section>
    </div>

    <div class="grid min-w-0 gap-4 xl:grid-cols-2">
      <section class="min-w-0 rounded-xl border border-slate-200 bg-white p-5" aria-labelledby="activity-repo-top-heading">
        <div class="flex items-center justify-between gap-3"><h2 id="activity-repo-top-heading" class="font-semibold text-slate-950">{{ t('activity.repositoryTop5') }}</h2><ElButton :icon="Refresh" circle text :loading="loading.repoTop" :aria-label="t('activity.retryRepositories')" @click="loadRepoTop(false)" /></div>
        <ElAlert v-if="errors.repoTop && repoTop.length" class="mt-4" type="error" :closable="false" :title="errors.repoTop" />
        <ElAlert v-if="errors.repoTop && !repoTop.length" class="mt-4" type="error" :closable="false" :title="errors.repoTop" />
        <div v-else-if="loading.repoTop && !repoTop.length" role="status" class="mt-4 h-44 animate-pulse rounded-lg bg-slate-100" />
        <div v-else-if="!topRepositories.length" class="flex h-44 items-center justify-center text-sm text-slate-500">{{ t(isCredit ? 'activity.noCreditRepositories' : 'activity.noRepositories') }}</div>
        <ol v-else class="mt-4 space-y-4">
          <li v-for="row in topRepositories" :key="row.repo_config_id">
            <button class="w-full rounded-md text-left" :class="selectedRepoID === row.repo_config_id ? 'bg-blue-50' : ''" :aria-pressed="selectedRepoID === row.repo_config_id" @click="selectRepository(row)">
              <div class="flex items-center justify-between gap-3 text-sm"><span class="truncate font-medium text-slate-900" :title="row.name">{{ row.name }}</span><span class="shrink-0 font-semibold text-slate-700">{{ amount(repoPrimary(row)) }}</span></div>
              <div class="mt-1.5 overflow-hidden rounded-full bg-slate-100" style="height: .5rem"><div class="h-full rounded-full" :style="{ background: '#3b82f6', width: barWidth(repoPrimary(row), maxRepoAmount) }" /></div>
              <div class="mt-1 flex flex-wrap gap-x-3 text-xs text-slate-500"><span>{{ percent(repoShare(row)) }} {{ t('activity.committedShare') }}</span><span v-if="repoChange(row) != null">{{ amountChange(repoChange(row)) }} {{ t('activity.previousPeriod') }}</span><span v-if="repoShared(row)">{{ amount(repoShared(row)) }} {{ t('activity.sharedParticipation') }}</span></div>
            </button>
          </li>
        </ol>
      </section>

      <section class="min-w-0 rounded-xl border border-slate-200 bg-white p-5" aria-labelledby="activity-pr-top-heading">
        <div class="flex items-center justify-between gap-3"><h2 id="activity-pr-top-heading" class="font-semibold text-slate-950">{{ t('activity.pullRequestTop5') }}</h2><ElButton :icon="Refresh" circle text :loading="loading.prTop" :aria-label="t('activity.retryPullRequests')" @click="loadPRTop(false)" /></div>
        <ElAlert v-if="errors.prTop && prTop.length" class="mt-4" type="error" :closable="false" :title="errors.prTop" />
        <ElAlert v-if="errors.prTop && !prTop.length" class="mt-4" type="error" :closable="false" :title="errors.prTop" />
        <div v-else-if="loading.prTop && !prTop.length" role="status" class="mt-4 h-44 animate-pulse rounded-lg bg-slate-100" />
        <div v-else-if="!topPullRequests.length" class="flex h-44 items-center justify-center text-sm text-slate-500">{{ t('activity.noPullRequests') }}</div>
        <ol v-else class="mt-4 space-y-4">
          <li v-for="row in topPullRequests" :key="row.pr_record_id">
            <button class="w-full rounded-md text-left" :class="selectedPRID === row.pr_record_id ? 'bg-cyan-50' : ''" :aria-pressed="selectedPRID === row.pr_record_id" @click="selectPullRequest(row)">
              <div class="flex items-center justify-between gap-3 text-sm"><span class="min-w-0 truncate font-medium text-slate-900" :title="row.title">{{ row.repository_name }} #{{ row.scm_pr_id }} · {{ row.title }}</span><span class="shrink-0 font-semibold text-slate-700">{{ amount(prPrimary(row)) }}</span></div>
              <div class="mt-1.5 overflow-hidden rounded-full bg-slate-100" style="height: .5rem"><div class="h-full rounded-full" :style="{ background: '#0891b2', width: barWidth(prPrimary(row), maxPRAmount) }" /></div>
              <p class="mt-1 flex flex-wrap gap-x-3 text-xs text-slate-500"><span>{{ row.overlap_state === 'shared' ? t('activity.sharedOverlap') : t('activity.directInvolvement') }}</span><span v-if="prChange(row) != null">{{ amountChange(prChange(row)) }} {{ t('activity.previousPeriod') }}</span></p>
            </button>
          </li>
        </ol>
      </section>
    </div>

    <section class="min-w-0 rounded-xl border border-slate-200 bg-white" aria-labelledby="activity-full-list-heading">
      <div class="border-b border-slate-200 px-4 pt-4">
        <h2 id="activity-full-list-heading" class="sr-only">{{ t('activity.fullLists') }}</h2>
        <ElTabs v-model="activeList">
          <ElTabPane :label="t('activity.repositories')" name="repositories" />
          <ElTabPane :label="t('activity.pullRequests')" name="pullRequests" />
        </ElTabs>
      </div>
      <div class="grid gap-3 border-b border-slate-100 p-4 sm:grid-cols-[minmax(0,1fr)_10rem_auto]">
        <ElInput v-model="search" :prefix-icon="Search" clearable :placeholder="t('activity.searchCurrentList')" data-testid="activity-list-search" @keyup.enter="applyListControls" />
        <ElSelect v-model="sort" :aria-label="t('activity.sort')" data-testid="activity-list-sort">
          <ElOption :label="t(isCredit ? 'activity.sortCredit' : 'activity.sortToken')" value="tokens" />
          <ElOption :label="t('activity.sortName')" value="name" />
        </ElSelect>
        <ElButton type="primary" class="!ml-0" @click="applyListControls">{{ t('activity.apply') }}</ElButton>
      </div>

      <template v-if="activeList === 'repositories'">
        <ElAlert v-if="errors.repositories && repositories" class="m-4" type="error" :closable="false"><template #title>{{ errors.repositories }} <ElButton type="primary" link @click="loadRepositories(false)">{{ t('activity.retry') }}</ElButton></template></ElAlert>
        <ElAlert v-if="errors.repositories && !repositories" class="m-4" type="error" :closable="false"><template #title>{{ errors.repositories }} <ElButton type="primary" link @click="loadRepositories(false)">{{ t('activity.retry') }}</ElButton></template></ElAlert>
        <div v-else-if="loading.repositories && !repositories" role="status" class="m-5 h-44 animate-pulse rounded-lg bg-slate-100" />
        <div v-else-if="!repositories?.items.length" class="px-5 py-12 text-center text-sm text-slate-500">{{ t(isCredit ? 'activity.noCreditRepositories' : 'activity.noRepositories') }}</div>
        <ElTable v-else-if="wide" :data="repositories.items" row-key="repo_config_id" class="w-full" data-testid="activity-repository-table" highlight-current-row :current-row-key="selectedRepoID" @row-click="selectRepository">
          <ElTableColumn prop="name" :label="t('activity.repository')" min-width="260" show-overflow-tooltip />
          <ElTableColumn :label="t(isCredit ? 'activity.committedCredit' : 'activity.committedToken')" width="150"><template #default="{ row }">{{ amount(repoPrimary(row as ActivityV2RepositoryRow)) }}</template></ElTableColumn>
          <ElTableColumn :label="t('activity.committedShare')" width="130"><template #default="{ row }">{{ percent(repoShare(row as ActivityV2RepositoryRow)) }}</template></ElTableColumn>
          <ElTableColumn :label="t('activity.periodChange')" width="130"><template #default="{ row }">{{ amountChange(repoChange(row as ActivityV2RepositoryRow)) || '—' }}</template></ElTableColumn>
          <ElTableColumn :label="t('activity.sharedParticipation')" width="170"><template #default="{ row }">{{ amount(repoShared(row as ActivityV2RepositoryRow)) }}</template></ElTableColumn>
        </ElTable>
        <ul v-else class="divide-y divide-slate-100" data-testid="activity-repository-cards">
          <li v-for="row in repositories.items" :key="row.repo_config_id"><button class="min-h-11 w-full px-4 py-4 text-left" :class="selectedRepoID === row.repo_config_id ? 'bg-blue-50' : ''" :aria-pressed="selectedRepoID === row.repo_config_id" @click="selectRepository(row)"><p class="break-words font-medium text-slate-900">{{ row.name }}</p><dl class="mt-3 grid grid-cols-2 gap-3 text-sm"><div><dt class="text-slate-500">{{ t(isCredit ? 'activity.committedCredit' : 'activity.committedToken') }}</dt><dd class="font-semibold">{{ amount(repoPrimary(row)) }}</dd></div><div><dt class="text-slate-500">{{ t('activity.committedShare') }}</dt><dd>{{ percent(repoShare(row)) }}</dd></div><div><dt class="text-slate-500">{{ t('activity.periodChange') }}</dt><dd>{{ amountChange(repoChange(row)) || '—' }}</dd></div><div><dt class="text-slate-500">{{ t('activity.sharedParticipation') }}</dt><dd>{{ amount(repoShared(row)) }}</dd></div></dl></button></li>
        </ul>
        <CursorPager :has-previous="repoPage > 0" :has-next="Boolean(repositories?.next_cursor)" :loading="loading.repositories" :loading-label="t('settings.loading')" :previous-label="t('activity.previousPage')" :next-label="t('activity.nextPage')" test-i-d-prefix="activity-repositories" @previous="previousPage('repositories')" @next="nextPage('repositories')" />
      </template>

      <template v-else>
        <ElAlert v-if="errors.pullRequests && pullRequests" class="m-4" type="error" :closable="false"><template #title>{{ errors.pullRequests }} <ElButton type="primary" link @click="loadPullRequests(false)">{{ t('activity.retry') }}</ElButton></template></ElAlert>
        <ElAlert v-if="errors.pullRequests && !pullRequests" class="m-4" type="error" :closable="false"><template #title>{{ errors.pullRequests }} <ElButton type="primary" link @click="loadPullRequests(false)">{{ t('activity.retry') }}</ElButton></template></ElAlert>
        <div v-else-if="loading.pullRequests && !pullRequests" role="status" class="m-5 h-44 animate-pulse rounded-lg bg-slate-100" />
        <div v-else-if="!pullRequests?.items.length" class="px-5 py-12 text-center text-sm text-slate-500">{{ selectedRepoID || selectedPRID ? t('activity.noFilteredData') : t('activity.noPullRequests') }}</div>
        <ElTable v-else-if="wide" :data="pullRequests.items" row-key="pr_record_id" class="w-full" data-testid="activity-pull-request-table" highlight-current-row :current-row-key="selectedPRID" @row-click="selectPullRequest">
          <ElTableColumn :label="t('activity.pullRequest')" min-width="320"><template #default="{ row }"><p class="font-medium text-slate-900">{{ row.repository_name }} #{{ row.scm_pr_id }}</p><p class="truncate text-sm text-slate-500">{{ row.title }}</p></template></ElTableColumn>
          <ElTableColumn :label="t(isCredit ? 'activity.involvedCredit' : 'activity.involvedToken')" width="160"><template #default="{ row }">{{ amount(prPrimary(row as ActivityV2PullRequestRow)) }}</template></ElTableColumn>
          <ElTableColumn :label="t('activity.periodChange')" width="130"><template #default="{ row }">{{ amountChange(prChange(row as ActivityV2PullRequestRow)) || '—' }}</template></ElTableColumn>
          <ElTableColumn :label="t('activity.overlap')" width="130"><template #default="{ row }">{{ row.overlap_state === 'shared' ? t('activity.sharedOverlap') : t('activity.directInvolvement') }}</template></ElTableColumn>
          <ElTableColumn :label="t('activity.status')" width="120" prop="status" />
        </ElTable>
        <ul v-else class="divide-y divide-slate-100" data-testid="activity-pull-request-cards">
          <li v-for="row in pullRequests.items" :key="row.pr_record_id"><button class="min-h-11 w-full px-4 py-4 text-left" :class="selectedPRID === row.pr_record_id ? 'bg-cyan-50' : ''" :aria-pressed="selectedPRID === row.pr_record_id" @click="selectPullRequest(row)"><p class="font-medium text-slate-900">{{ row.repository_name }} #{{ row.scm_pr_id }}</p><p class="mt-1 break-words text-sm text-slate-600">{{ row.title }}</p><div class="mt-3 flex flex-wrap gap-4 text-sm text-slate-500"><span>{{ amount(prPrimary(row)) }} {{ t(isCredit ? 'activity.involvedCredit' : 'activity.involvedToken') }}</span><span v-if="prChange(row) != null">{{ amountChange(prChange(row)) }} {{ t('activity.previousPeriod') }}</span><span>{{ row.overlap_state === 'shared' ? t('activity.sharedOverlap') : t('activity.directInvolvement') }}</span><span>{{ row.status }}</span></div></button><div v-if="expandedPR === row.pr_record_id && row.commits?.length" class="bg-slate-50 px-4 py-3 text-xs text-slate-600"><p class="mb-2 font-medium">{{ t('activity.relatedCommits') }}</p><ul class="space-y-1 font-mono"><li v-for="commit in row.commits" :key="`${commit.repo_config_id}:${commit.commit_sha}`">{{ commit.commit_sha.slice(0, 12) }}</li></ul></div></li>
        </ul>
        <div v-if="wide && expandedPR && selectedPRRow?.commits?.length" class="border-t border-slate-100 bg-slate-50 px-5 py-4 text-xs text-slate-600"><p class="mb-2 font-medium">{{ t('activity.relatedCommits') }}</p><ul class="space-y-1 font-mono"><li v-for="commit in selectedPRRow.commits" :key="`${commit.repo_config_id}:${commit.commit_sha}`">{{ commit.commit_sha.slice(0, 12) }}</li></ul></div>
        <CursorPager :has-previous="prPage > 0" :has-next="Boolean(pullRequests?.next_cursor)" :loading="loading.pullRequests" :loading-label="t('settings.loading')" :previous-label="t('activity.previousPage')" :next-label="t('activity.nextPage')" test-i-d-prefix="activity-pull-requests" @previous="previousPage('pullRequests')" @next="nextPage('pullRequests')" />
      </template>
    </section>
  </div>
</template>
