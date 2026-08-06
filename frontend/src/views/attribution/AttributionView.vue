<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getAttributionReport, normalizeAttributionReport } from '@/api/attribution'
import { useI18n } from '@/i18n'
import type { AttributionRepoReport, AttributionReport } from '@/types'
import AppLayout from '@/components/AppLayout.vue'

type RangePreset = '7' | '30' | '90' | 'custom'

const { t } = useI18n()
const report = ref<AttributionReport | null>(null)
const loading = ref(false)
const error = ref('')
const rangePreset = ref<RangePreset>('7')
const customFrom = ref(toDateInput(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000)))
const customTo = ref(toDateInput(new Date()))
const expandedRepos = ref<Set<number>>(new Set())

const conserved = computed(() => {
  if (!report.value) return true
  return report.value.measured_tokens === report.value.bound_tokens + report.value.unbound_tokens
})

function rangeParams() {
  if (rangePreset.value === 'custom') {
    const from = new Date(`${customFrom.value}T00:00:00`)
    const to = new Date(`${customTo.value}T00:00:00`)
    to.setDate(to.getDate() + 1)
    return { from: from.toISOString(), to: to.toISOString() }
  }
  const days = Number(rangePreset.value)
  const to = new Date()
  const from = new Date(to.getTime() - days * 24 * 60 * 60 * 1000)
  return { from: from.toISOString(), to: to.toISOString() }
}

async function loadReport() {
  loading.value = true
  error.value = ''
  try {
    const response = await getAttributionReport(rangeParams())
    if (!response.data.data) throw new Error('attribution report response is empty')
    report.value = normalizeAttributionReport(response.data.data)
  } catch (cause) {
    console.error(cause)
    error.value = t('attribution.loadFailed')
  } finally {
    loading.value = false
  }
}

function selectPreset(value: RangePreset) {
  rangePreset.value = value
  if (value !== 'custom') void loadReport()
}

function toggleRepo(repoID: number) {
  const next = new Set(expandedRepos.value)
  if (next.has(repoID)) next.delete(repoID)
  else next.add(repoID)
  expandedRepos.value = next
}

function repoPRCount(repo: AttributionRepoReport) {
  return new Set(repo.commits.flatMap((commit) => commit.prs.map((pr) => pr.id))).size
}

function repoDisplayName(repo: AttributionRepoReport) {
  return repo.name || repo.repo_key || t('attribution.unbound')
}

function shortSHA(value: string) {
  return value.slice(0, 10)
}

function formatTokens(value?: number) {
  return new Intl.NumberFormat().format(value ?? 0)
}

function formatPercent(value?: number) {
  return `${Math.round((value ?? 0) * 1000) / 10}%`
}

function presetLabel(preset: RangePreset) {
  if (preset === '30') return t('attribution.last30Days')
  if (preset === '90') return t('attribution.last90Days')
  return t('attribution.last7Days')
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function toDateInput(value: Date) {
  const local = new Date(value.getTime() - value.getTimezoneOffset() * 60 * 1000)
  return local.toISOString().slice(0, 10)
}

onMounted(() => {
  void loadReport()
})
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-6">
      <header class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div class="min-w-0">
          <h1 class="text-2xl font-bold text-slate-900">{{ t('attribution.title') }}</h1>
          <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('attribution.subtitle') }}</p>
        </div>
        <div class="flex w-full flex-wrap items-center gap-2 lg:w-auto lg:justify-end">
        <button
          v-for="preset in (['7', '30', '90'] as RangePreset[])"
          :key="preset"
          type="button"
          class="min-h-11 rounded-md border px-3 py-2 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2"
          :class="rangePreset === preset ? 'border-cyan-700 bg-cyan-50 text-cyan-800' : 'border-slate-300 bg-white text-slate-700 hover:bg-slate-50'"
          @click="selectPreset(preset)"
        >
          {{ presetLabel(preset) }}
        </button>
        <button
          type="button"
          class="min-h-11 rounded-md border px-3 py-2 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2"
          :class="rangePreset === 'custom' ? 'border-cyan-700 bg-cyan-50 text-cyan-800' : 'border-slate-300 bg-white text-slate-700 hover:bg-slate-50'"
          @click="selectPreset('custom')"
        >
          {{ t('attribution.custom') }}
        </button>
        <button
          type="button"
          data-testid="attribution-refresh"
          class="min-h-11 rounded-md bg-cyan-700 px-3 py-2 text-sm font-medium text-white hover:bg-cyan-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
          :disabled="loading"
          @click="loadReport"
        >
          {{ loading ? t('attribution.loading') : t('attribution.refresh') }}
        </button>
        </div>
      </header>

    <div v-if="rangePreset === 'custom'" class="flex min-w-0 flex-wrap items-end gap-3 rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
      <label class="w-full text-sm text-slate-700 sm:w-auto">
        <span class="mb-1 block text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('attribution.from') }}</span>
        <input v-model="customFrom" type="date" class="min-h-11 w-full rounded-md border border-slate-300 px-3 py-2 text-slate-900 focus:border-cyan-600 focus:outline-none focus:ring-1 focus:ring-cyan-600 sm:w-auto" />
      </label>
      <label class="w-full text-sm text-slate-700 sm:w-auto">
        <span class="mb-1 block text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('attribution.to') }}</span>
        <input v-model="customTo" type="date" class="min-h-11 w-full rounded-md border border-slate-300 px-3 py-2 text-slate-900 focus:border-cyan-600 focus:outline-none focus:ring-1 focus:ring-cyan-600 sm:w-auto" />
      </label>
      <button type="button" class="min-h-11 w-full rounded-md bg-cyan-700 px-4 py-2 text-sm font-medium text-white hover:bg-cyan-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2 sm:w-auto" @click="loadReport">
        {{ t('attribution.apply') }}
      </button>
    </div>

    <div v-if="error" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">{{ error }}</div>

    <template v-if="report">
      <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('attribution.measured') }}</p>
          <p data-testid="attribution-measured" class="mt-2 text-2xl font-semibold text-slate-900">{{ formatTokens(report.measured_tokens) }}</p>
        </article>
        <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('attribution.bound') }}</p>
          <p class="mt-2 text-2xl font-semibold text-emerald-700">{{ formatTokens(report.bound_tokens) }}</p>
          <p class="mt-1 text-xs text-slate-500">{{ formatPercent(report.allocation_rate) }} {{ t('attribution.allocationRate') }}</p>
        </article>
        <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('attribution.unbound') }}</p>
          <p class="mt-2 text-2xl font-semibold text-amber-700">{{ formatTokens(report.unbound_tokens) }}</p>
          <p class="mt-1 text-xs text-slate-500">{{ t('attribution.shared', { count: formatTokens(report.shared_tokens) }) }}</p>
        </article>
        <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('attribution.coverageGaps') }}</p>
          <p class="mt-2 text-2xl font-semibold text-slate-900">{{ formatTokens(report.coverage_gap_count) }}</p>
          <p class="mt-1 text-xs text-slate-500">{{ t('attribution.requestIDs', { count: formatTokens(report.request_id_coverage_count) }) }}</p>
        </article>
      </section>

      <div
        data-testid="attribution-conservation"
        class="rounded-lg border px-4 py-3 text-sm font-medium"
        :class="conserved ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-red-200 bg-red-50 text-red-800'"
      >
        {{ conserved ? t('attribution.conservationOK') : t('attribution.conservationFailed') }}
        <span v-if="report.historical_advisory_tokens" class="ml-2 font-normal">{{ t('attribution.historicalAdvisory', { count: formatTokens(report.historical_advisory_tokens) }) }}</span>
      </div>

      <section class="min-w-0 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="border-b border-slate-200 px-5 py-4">
          <h2 class="font-semibold text-slate-900">{{ t('attribution.repositories') }}</h2>
        </div>
        <div v-if="report.repositories.length === 0" class="px-5 py-12 text-center text-sm text-slate-500">{{ t('attribution.noData') }}</div>
        <div v-else class="overflow-x-auto" role="region" tabindex="0" :aria-label="t('attribution.repositories')">
          <table class="min-w-[760px] divide-y divide-slate-200 text-sm">
            <thead class="bg-slate-50 text-left text-xs font-semibold uppercase tracking-wide text-slate-500">
              <tr>
                <th scope="col" class="px-5 py-3">{{ t('attribution.repository') }}</th>
                <th scope="col" class="px-5 py-3 text-right">{{ t('attribution.processed') }}</th>
                <th scope="col" class="px-5 py-3 text-right">{{ t('attribution.unbound') }}</th>
                <th scope="col" class="px-5 py-3 text-right">{{ t('attribution.commits') }}</th>
                <th scope="col" class="px-5 py-3 text-right">{{ t('attribution.prs') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100">
              <template v-for="repo in report.repositories" :key="repo.repo_config_id">
                <tr class="hover:bg-slate-50">
                  <td class="px-5 py-4">
                    <button
                      type="button"
                      class="flex max-w-xl items-start gap-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2"
                      :data-testid="`attribution-repo-${repo.repo_config_id}`"
                      :aria-expanded="expandedRepos.has(repo.repo_config_id)"
                      :aria-controls="`attribution-repo-detail-${repo.repo_config_id}`"
                      @click="toggleRepo(repo.repo_config_id)"
                    >
                      <span class="mt-0.5 text-slate-400">{{ expandedRepos.has(repo.repo_config_id) ? '▾' : '▸' }}</span>
                      <span class="min-w-0">
                        <span class="block break-words font-medium text-slate-900">{{ repoDisplayName(repo) }}</span>
                        <span class="mt-1 block text-xs text-slate-500">
                          {{ t('attribution.worktrees') }}: {{ repo.worktrees.length }} · {{ t('attribution.branches') }}: {{ repo.branches.join(', ') || '—' }}
                          <template v-if="repo.shared_tokens"> · {{ t('attribution.shared', { count: formatTokens(repo.shared_tokens) }) }}</template>
                          <template v-if="repo.inherited_tokens"> · {{ t('attribution.inherited', { count: formatTokens(repo.inherited_tokens) }) }}</template>
                          <template v-if="repo.commits.length === 0"> · {{ t('attribution.noCommits') }}</template>
                        </span>
                      </span>
                    </button>
                  </td>
                  <td class="px-5 py-4 text-right font-medium tabular-nums text-slate-900">{{ formatTokens(repo.processed_tokens) }}</td>
                  <td class="px-5 py-4 text-right text-amber-700">{{ formatTokens(repo.unbound_tokens) }}</td>
                  <td class="px-5 py-4 text-right tabular-nums text-slate-700">{{ repo.commits.length }}</td>
                  <td class="px-5 py-4 text-right tabular-nums text-slate-700">{{ repoPRCount(repo) }}</td>
                </tr>
                <tr v-if="expandedRepos.has(repo.repo_config_id)" :id="`attribution-repo-detail-${repo.repo_config_id}`" class="bg-slate-50">
                  <td colspan="5" class="px-5 py-4">
                    <div v-if="repo.commits.length === 0" class="text-sm text-slate-500">{{ t('attribution.noCommits') }}</div>
                    <div v-else class="space-y-3">
                      <article v-for="commit in repo.commits" :key="commit.commit_sha" class="rounded-lg border border-slate-200 bg-white p-4">
                        <div class="flex flex-wrap items-center justify-between gap-2">
                          <div>
                            <span class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('attribution.commit') }}</span>
                            <code class="ml-2 text-sm font-semibold text-slate-900">{{ shortSHA(commit.commit_sha) }}</code>
                            <span v-if="commit.lineage" class="ml-2 text-xs text-cyan-700">{{ t('attribution.lineage', { lineage: commit.lineage }) }}</span>
                          </div>
                          <div class="text-right text-sm">
                            <strong class="text-slate-900">{{ formatTokens(commit.tokens) }}</strong>
                            <span v-if="commit.inherited_tokens" class="ml-2 text-cyan-700">{{ t('attribution.inherited', { count: formatTokens(commit.inherited_tokens) }) }}</span>
                          </div>
                        </div>
                        <p v-if="commit.inherited_from_commit_shas.length" class="mt-2 break-words text-xs text-slate-500">
                          {{ t('attribution.inheritedFrom', { commits: commit.inherited_from_commit_shas.map(shortSHA).join(', ') }) }}
                        </p>
                        <div class="mt-3 flex flex-wrap gap-2">
                          <a v-for="pr in commit.prs" :key="pr.id" :href="pr.url" target="_blank" rel="noreferrer" class="max-w-full break-words rounded-full bg-cyan-50 px-3 py-1 text-xs font-medium text-cyan-800 hover:bg-cyan-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2">
                            #{{ pr.scm_pr_id }} {{ pr.title }} · {{ pr.status }}
                          </a>
                          <span v-if="commit.prs.length === 0" class="text-xs text-slate-500">{{ t('attribution.noPR') }}</span>
                        </div>
                      </article>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </section>

      <details class="min-w-0 rounded-lg border border-slate-200 bg-white shadow-sm">
        <summary class="cursor-pointer rounded-lg px-5 py-4 font-semibold text-slate-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-cyan-600">{{ t('attribution.evidence') }}</summary>
        <div class="border-t border-slate-200 px-5 py-4">
          <p class="mb-4 text-sm text-slate-500">{{ t('attribution.evidenceHelp') }}</p>
          <div class="overflow-x-auto" role="region" tabindex="0" :aria-label="t('attribution.evidence')">
            <table class="min-w-[900px] divide-y divide-slate-200 text-xs">
              <thead class="bg-slate-50 text-left font-semibold uppercase tracking-wide text-slate-500">
                <tr>
                  <th scope="col" class="px-3 py-2">{{ t('attribution.bucket') }}</th>
                  <th scope="col" class="px-3 py-2">{{ t('attribution.window') }}</th>
                  <th scope="col" class="px-3 py-2">{{ t('attribution.quality') }}</th>
                  <th scope="col" class="px-3 py-2">{{ t('attribution.correlation') }}</th>
                  <th scope="col" class="px-3 py-2">{{ t('attribution.allocation') }}</th>
                  <th scope="col" class="px-3 py-2">{{ t('attribution.breakdown') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100">
                <tr v-for="bucket in report.buckets" :key="bucket.bucket_id">
                  <td class="px-3 py-3 font-mono text-slate-700">{{ bucket.bucket_id.slice(0, 12) }}</td>
                  <td class="px-3 py-3 text-slate-600">{{ formatDateTime(bucket.observed_start_at) }}<br />{{ formatDateTime(bucket.observed_end_at) }}</td>
                  <td class="px-3 py-3 text-slate-700">{{ bucket.token_quality }}<br />{{ bucket.model || bucket.tool }}</td>
                  <td class="px-3 py-3 text-slate-700">{{ bucket.request_correlation_quality }}<br />{{ t('attribution.requestIDs', { count: bucket.request_id_coverage_count }) }}</td>
                  <td class="px-3 py-3 text-slate-700">{{ bucket.allocation_status }}<br />{{ t('attribution.revision', { count: bucket.allocation_revision }) }}</td>
                  <td class="px-3 py-3 text-slate-600">
                    {{ t('attribution.inputBreakdown', { fresh: formatTokens(bucket.tokens.fresh_input_tokens), read: formatTokens(bucket.tokens.cache_read_tokens), write: formatTokens(bucket.tokens.cache_write_tokens) }) }}<br />
                    {{ t('attribution.outputBreakdown', { output: formatTokens(bucket.tokens.output_tokens), reasoning: formatTokens(bucket.tokens.reasoning_tokens) }) }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </details>
    </template>
    </div>
  </AppLayout>
</template>
