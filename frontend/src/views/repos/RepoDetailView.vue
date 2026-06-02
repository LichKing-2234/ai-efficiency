<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getRepo, updateRepo } from '@/api/repo'
import { getPR, getPRSyncJob, listPRs, refreshPRUsage, syncPRs } from '@/api/pr'
import { listProviders } from '@/api/scmProvider'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import type { CommitFreshness, PRCommitUsageSnapshot, PRListSummary, PRRecord, PRSyncJob, RepoConfig, SCMProvider, UsageStatus } from '@/types'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { locale, t } = useI18n()

const repo = ref<RepoConfig | null>(null)
const prs = ref<PRRecord[]>([])
const prsTotal = ref(0)
const prsSummary = ref<PRListSummary | null>(null)
const prsPage = ref(0)
const prsPageSize = 10
const prsMonths = ref(3)
const loading = ref(true)
const syncing = ref(false)
const syncJob = ref<PRSyncJob | null>(null)
const syncPollTimer = ref<number | null>(null)
const syncMessage = ref('')
const syncMessageTone = ref<'success' | 'error'>('success')
const detailsLoadingIds = ref<Record<number, boolean>>({})
const expandedPRId = ref<number | null>(null)
const prDetails = ref<Record<number, PRRecord>>({})
const prDetailRequests = new Map<number, Promise<boolean>>()
const providers = ref<SCMProvider[]>([])
const selectedProviderId = ref<number | null>(null)
const bindingSaving = ref(false)
const bindingMessage = ref('')

const repoId = Number(route.params.id)
const isRepoUnbound = computed(() => repo.value?.binding_state === 'unbound')
const syncDisabledReason = computed(() => (isRepoUnbound.value ? t('repoDetail.syncDisabledUnbound') : ''))
const prUsageSummary = computed(() => {
  if (prsSummary.value) {
    return {
      total: prsSummary.value.total,
      withUsage: prsSummary.value.with_usage,
      pendingUpload: prsSummary.value.pending_upload,
      noCheckpoint: prsSummary.value.no_checkpoint,
      refreshFailed: prsSummary.value.refresh_failed,
    }
  }
  const total = prsTotal.value || prs.value.length
  const withUsage = prs.value.filter((pr) => totalPRTokens(pr) > 0 || (pr.usage_credit_usage ?? 0) > 0 || (pr.usage_request_count ?? 0) > 0).length
  const pendingUpload = prs.value.filter((pr) => pr.usage_status === 'pending_upload').length
  const noCheckpoint = prs.value.filter((pr) => pr.usage_status === 'no_checkpoint').length
  const refreshFailed = prs.value.filter((pr) => pr.usage_status === 'refresh_failed').length
  return { total, withUsage, pendingUpload, noCheckpoint, refreshFailed }
})

onMounted(async () => {
  try {
    const [repoRes, prsRes] = await Promise.all([
      getRepo(repoId),
      listPRs(repoId, { limit: prsPageSize, months: prsMonths.value }).catch(() => ({ data: { data: { items: [] } } })),
    ])
    repo.value = repoRes.data.data ?? null
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0
    prsSummary.value = prData && 'summary' in prData && prData.summary ? prData.summary : null

    selectedProviderId.value = repo.value?.edges?.scm_provider?.id ?? repo.value?.scm_provider_id ?? null
    if (auth.isAdmin) {
      const providersRes = await listProviders().catch(() => ({ data: { data: [] } }))
      const providerData = providersRes.data.data
      providers.value = Array.isArray(providerData) ? providerData : (providerData as any)?.items ?? []
    }
  } catch {
    router.push('/repos')
  } finally {
    loading.value = false
  }
})

async function refreshRepo() {
  const repoRes = await getRepo(repoId)
  repo.value = repoRes.data.data ?? null
  selectedProviderId.value = repo.value?.edges?.scm_provider?.id ?? repo.value?.scm_provider_id ?? null
}

async function loadPRs() {
  try {
    const prsRes = await listPRs(repoId, { limit: prsPageSize, offset: prsPage.value * prsPageSize, months: prsMonths.value })
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0
    prsSummary.value = prData && 'summary' in prData && prData.summary ? prData.summary : null
  } catch { /* load failed */ }
}

async function handleSyncPRs() {
  syncing.value = true
  syncMessage.value = ''
  syncJob.value = null
  if (syncPollTimer.value != null) {
    window.clearTimeout(syncPollTimer.value)
    syncPollTimer.value = null
  }
  try {
    const res = await syncPRs(repoId)
    const result = res.data.data
    if (!result?.job_id) {
      throw new Error(t('repoDetail.prSyncJobMissing'))
    }
    syncJob.value = {
      id: result.job_id,
      repo_config_id: repoId,
      status: result.status as PRSyncJob['status'],
      phase: result.phase as PRSyncJob['phase'],
      current_page: 0,
      page_size: 100,
      fetched_prs: 0,
      total_prs: 0,
      processed_prs: 0,
      created_prs: 0,
      changed_prs: 0,
      unchanged_prs: 0,
      usage_total_prs: 0,
      usage_refreshed_prs: 0,
      usage_skipped_prs: 0,
      usage_failed_prs: 0,
    }
    await pollSyncJob(result.job_id)
  } catch (error: any) {
    syncMessageTone.value = 'error'
    syncMessage.value = error?.response?.data?.message || error?.message || t('repoDetail.syncFailed')
    syncing.value = false
  }
}

async function pollSyncJob(jobId: number) {
  try {
    const res = await getPRSyncJob(jobId)
    const job = res.data.data ?? null
    syncJob.value = job
    if (!job) return
    if (isTerminalJob(job)) {
      if (syncPollTimer.value != null) {
        window.clearTimeout(syncPollTimer.value)
        syncPollTimer.value = null
      }
      syncing.value = false
      if (job.status === 'completed') {
        prsPage.value = 0
        await loadPRs()
        syncMessageTone.value = 'success'
        syncMessage.value = t('repoDetail.syncCompleted', {
          created: formatCount(job.created_prs),
          changed: formatCount(job.changed_prs),
          unchanged: formatCount(job.unchanged_prs),
        })
      } else {
        syncMessageTone.value = 'error'
        syncMessage.value = job.last_error || t('repoDetail.syncStatusSummary', { status: job.status })
      }
      return
    }
    syncPollTimer.value = window.setTimeout(() => pollSyncJob(job.id), 1500)
  } catch (error: any) {
    syncing.value = false
    syncMessageTone.value = 'error'
    syncMessage.value = error?.response?.data?.message || t('repoDetail.syncProgressFailed')
  }
}

async function saveBinding() {
  bindingSaving.value = true
  bindingMessage.value = ''
  try {
    await updateRepo(repoId, { scm_provider_id: selectedProviderId.value ?? undefined, clear_scm_provider: selectedProviderId.value == null } as any)
    await refreshRepo()
    bindingMessage.value = t('repoDetail.bindingSaved')
  } catch (error: any) {
    bindingMessage.value = error?.response?.data?.message || t('repoDetail.bindingSaveFailed')
  } finally {
    bindingSaving.value = false
  }
}

async function clearBinding() {
  selectedProviderId.value = null
  await saveBinding()
}

function handleMonthsChange(e: Event) {
  prsMonths.value = Number((e.target as HTMLSelectElement).value)
  prsPage.value = 0
  loadPRs()
}

function prsPrevPage() {
  if (prsPage.value > 0) {
    prsPage.value--
    loadPRs()
  }
}

function prsNextPage() {
  if ((prsPage.value + 1) * prsPageSize < prsTotal.value) {
    prsPage.value++
    loadPRs()
  }
}

function formatDate(date: string | null | undefined) {
  if (!date) return '—'
  return new Date(date).toLocaleString(locale.value)
}

function formatCount(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toLocaleString(locale.value)
}

function formatDecimal(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toFixed(2)
}

function totalPRTokens(pr: PRRecord) {
  return (pr.usage_input_tokens ?? 0) + (pr.usage_output_tokens ?? 0) + (pr.usage_cached_input_tokens ?? 0)
}

function formatPRTokenUsage(pr: PRRecord) {
  const total = totalPRTokens(pr)
  return total > 0 ? formatCount(total) : '—'
}

function isTerminalJob(job: PRSyncJob) {
  return ['completed', 'failed', 'cancelled', 'abandoned'].includes(job.status)
}

function phaseLabel(phase?: string) {
  const labels: Record<string, string> = {
    queued: t('repoDetail.phaseQueued'),
    fetching_prs: t('repoDetail.phaseFetchingPrs'),
    upserting_prs: t('repoDetail.phaseUpsertingPrs'),
    labeling: t('repoDetail.phaseLabeling'),
    refreshing_usage: t('repoDetail.phaseRefreshingUsage'),
    completed: t('repoDetail.phaseCompleted'),
    failed: t('repoDetail.phaseFailed'),
  }
  return phase ? labels[phase] ?? phase : t('repoDetail.phaseQueued')
}

function usageStatusLabel(status?: UsageStatus) {
  const labels: Record<UsageStatus, string> = {
    fresh: t('repoDetail.usageFresh'),
    pending_upload: t('repoDetail.usagePending'),
    no_checkpoint: t('repoDetail.noCheckpoint'),
    no_usage_events: t('repoDetail.usageNoUsage'),
    unbound: t('repoDetail.unbound'),
    stale_snapshot: t('repoDetail.usageStale'),
    refresh_failed: t('repoDetail.usageFailed'),
    unknown: t('repoDetail.usageUnknown'),
  }
  return labels[status ?? 'unknown']
}

function bindingLabel() {
  return isRepoUnbound.value ? t('repoDetail.needsBinding') : t('repoDetail.bound')
}

function resolvedPR(pr: PRRecord) {
  return prDetails.value[pr.id] ?? pr
}

function commitSnapshots(pr: PRRecord): PRCommitUsageSnapshot[] {
  const detail = resolvedPR(pr)
  const snapshots = detail.edges?.pr_commit_usage_snapshots
  return Array.isArray(snapshots) ? [...snapshots].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0)) : []
}

function commitFreshnessFor(pr: PRRecord, commitSha: string): CommitFreshness | undefined {
  const detail = resolvedPR(pr)
  return detail.commit_freshness?.find((item) => item.commit_sha === commitSha)
}

function hasUsageSnapshot(pr: PRRecord) {
  return commitSnapshots(pr).length > 0
}

function usageSummaryNeedsRefresh(pr: PRRecord) {
  const detail = resolvedPR(pr)
  return !hasUsageSnapshot(detail) && !detail.usage_refreshed_at
}

function isPRDetailLoading(prId: number) {
  return Boolean(detailsLoadingIds.value[prId])
}

function setPRDetailLoading(prId: number, loading: boolean) {
  if (loading) {
    detailsLoadingIds.value = { ...detailsLoadingIds.value, [prId]: true }
    return
  }
  const { [prId]: _removed, ...rest } = detailsLoadingIds.value
  detailsLoadingIds.value = rest
}

async function ensurePRDetail(prId: number, options?: { force?: boolean }) {
  const forceRefresh = options?.force === true
  if (!forceRefresh && prDetails.value[prId]) return true

  const existingRequest = prDetailRequests.get(prId)
  if (existingRequest) {
    if (!forceRefresh) return existingRequest
    await existingRequest
  }

  setPRDetailLoading(prId, true)
  const request = (async () => {
    try {
      const res = await getPR(prId)
      const detail = res.data.data
      if (detail) {
        prDetails.value = { ...prDetails.value, [prId]: detail }
        return true
      }
      return false
    } catch {
      return false
    } finally {
      prDetailRequests.delete(prId)
      setPRDetailLoading(prId, false)
    }
  })()

  prDetailRequests.set(prId, request)
  return request
}

async function refreshUsageDetail(prId: number) {
  setPRDetailLoading(prId, true)
  try {
    const res = await refreshPRUsage(prId)
    const detail = res.data.data
    if (detail) {
      prDetails.value = { ...prDetails.value, [prId]: detail }
      prs.value = prs.value.map((pr) => (pr.id === prId ? { ...pr, ...detail } : pr))
      return true
    }
    return false
  } catch {
    return false
  } finally {
    setPRDetailLoading(prId, false)
  }
}

async function togglePRDetails(prId: number) {
  if (expandedPRId.value === prId) {
    expandedPRId.value = null
    return
  }

  expandedPRId.value = prId
  const loaded = await ensurePRDetail(prId)
  if (!loaded && expandedPRId.value === prId && !prDetails.value[prId]) {
    expandedPRId.value = null
    return
  }

  const row = prDetails.value[prId] ?? prs.value.find((item) => item.id === prId)
  if (row && usageSummaryNeedsRefresh(row)) {
    const refreshed = await refreshUsageDetail(prId)
    if (!refreshed && expandedPRId.value === prId && !prDetails.value[prId]) {
      expandedPRId.value = null
    }
  }
}

onUnmounted(() => {
  if (syncPollTimer.value != null) {
    window.clearTimeout(syncPollTimer.value)
  }
})
</script>

<template>
  <AppLayout>
    <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('repoDetail.loading') }}</div>

    <div v-else-if="repo" class="space-y-5">
      <div>
        <button class="text-sm text-indigo-600 hover:text-indigo-800" @click="router.push('/repos')">
          &larr; {{ t('repoDetail.backToRepos') }}
        </button>
        <div class="mt-2 flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p class="text-xs font-semibold uppercase tracking-wide text-blue-700">{{ t('nav.codeSection') }}</p>
            <h1 class="text-2xl font-bold text-gray-900">{{ repo.name }}</h1>
            <p class="text-sm text-gray-500">{{ repo.full_name }}</p>
            <p v-if="repo.clone_url" class="mt-0.5 break-all font-mono text-xs text-gray-400">{{ repo.clone_url }}</p>
          </div>
          <div class="flex flex-col items-start gap-1 sm:items-end">
            <button
              class="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              :disabled="syncing || isRepoUnbound"
              :title="syncDisabledReason"
              @click="handleSyncPRs"
            >{{ syncing ? t('repoDetail.syncing') : t('repoDetail.syncPrs') }}</button>
            <p v-if="syncDisabledReason" class="max-w-xs text-xs text-amber-700">{{ syncDisabledReason }}</p>
          </div>
        </div>
      </div>

      <div
        v-if="syncMessage"
        class="rounded-md p-3 text-sm"
        :class="syncMessageTone === 'success' ? 'bg-emerald-50 text-emerald-800' : 'bg-red-50 text-red-700'"
      >
        {{ syncMessage }}
      </div>

      <div v-if="syncJob" class="rounded-md bg-blue-50 p-3 text-sm text-blue-900">
        <div class="font-medium">{{ phaseLabel(syncJob.phase) }}</div>
        <div class="mt-1 text-xs text-blue-800">
          {{ formatCount(syncJob.fetched_prs) }} fetched ·
          {{ formatCount(syncJob.processed_prs) }} processed ·
          {{ formatCount(syncJob.usage_refreshed_prs) }} usage refreshed ·
          {{ formatCount(syncJob.usage_skipped_prs) }} skipped ·
          {{ formatCount(syncJob.usage_failed_prs) }} failed
        </div>
      </div>

      <div v-if="auth.isAdmin" class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('repoDetail.scmBinding') }}</h2>
          <span
            class="rounded px-2 py-0.5 text-xs font-medium"
            :class="isRepoUnbound ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800'"
          >
            {{ isRepoUnbound ? t('repoDetail.unbound') : t('repoDetail.bound') }}
          </span>
        </div>
        <p class="mt-3 text-sm text-gray-500">
          {{ isRepoUnbound ? t('repoDetail.bindingUnboundHelp') : t('repoDetail.bindingBoundHelp') }}
        </p>
        <div class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
          <select
            v-model="selectedProviderId"
            class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 sm:max-w-sm"
          >
            <option :value="null">{{ t('repoDetail.unbound') }}</option>
            <option v-for="provider in providers" :key="provider.id" :value="provider.id">
              {{ provider.name }}
            </option>
          </select>
          <div class="flex gap-2">
            <button
              class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
              :disabled="bindingSaving"
              @click="saveBinding"
            >
              {{ bindingSaving ? t('repoDetail.saving') : t('repoDetail.saveBinding') }}
            </button>
            <button
              class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 disabled:opacity-50"
              :disabled="bindingSaving || selectedProviderId == null"
              @click="clearBinding"
            >
              {{ t('repoDetail.clearBinding') }}
            </button>
          </div>
        </div>
        <div v-if="bindingMessage" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-700">{{ bindingMessage }}</div>
      </div>

      <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.health') }}</h2>
            <p class="mt-1 text-sm text-slate-600">
              {{ t('repoDetail.healthHelp') }}
            </p>
          </div>
          <span
            class="inline-flex rounded-full px-2.5 py-1 text-xs font-semibold"
            :class="isRepoUnbound ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800'"
          >
            {{ bindingLabel() }}
          </span>
        </div>
        <div class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-md bg-slate-50 p-3">
            <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repos.defaultBranch') }}</div>
            <div class="mt-1 font-medium text-slate-900">{{ repo.default_branch }}</div>
          </div>
          <div class="rounded-md bg-slate-50 p-3">
            <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.repositoryStatus') }}</div>
            <div class="mt-1 font-medium text-slate-900">{{ repo.status }}</div>
          </div>
          <div class="rounded-md bg-slate-50 p-3">
            <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.created') }}</div>
            <div class="mt-1 font-medium text-slate-900">{{ formatDate(repo.created_at) }}</div>
          </div>
          <div class="rounded-md bg-slate-50 p-3">
            <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.scmProvider') }}</div>
            <div class="mt-1 font-medium text-slate-900">{{ repo.edges?.scm_provider?.name || t('repoDetail.unbound') }}</div>
          </div>
        </div>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('repoDetail.prUsageSummary') }}</h2>
            <p class="mt-1 text-sm text-gray-500">{{ t('repoDetail.prUsageHelp') }}</p>
          </div>
          <div class="flex items-center space-x-3">
            <select :value="prsMonths" @change="handleMonthsChange" class="rounded-md border border-gray-300 px-2 py-1 text-xs text-gray-600">
              <option :value="1">{{ t('repoDetail.month1') }}</option>
              <option :value="3">{{ t('repoDetail.month3') }}</option>
              <option :value="6">{{ t('repoDetail.month6') }}</option>
              <option :value="12">{{ t('repoDetail.month12') }}</option>
              <option :value="0">{{ t('repoDetail.allTime') }}</option>
            </select>
            <span v-if="prsTotal > 0" class="text-xs text-gray-400">{{ prsTotal }} {{ t('repoDetail.totalSuffix') }}</span>
          </div>
        </div>

        <div class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <div class="rounded-md border border-slate-200 p-3">
            <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.totalPrs') }}</div>
            <div class="mt-1 text-xl font-semibold text-slate-900">{{ prUsageSummary.total }}</div>
          </div>
          <div class="rounded-md border border-emerald-200 bg-emerald-50 p-3">
            <div class="text-xs uppercase tracking-wide text-emerald-700">{{ t('repoDetail.withAiUsage') }}</div>
            <div class="mt-1 text-xl font-semibold text-emerald-900">{{ prUsageSummary.withUsage }}</div>
          </div>
          <div class="rounded-md border border-blue-200 bg-blue-50 p-3">
            <div class="text-xs uppercase tracking-wide text-blue-700">{{ t('repoDetail.pendingUpload') }}</div>
            <div class="mt-1 text-xl font-semibold text-blue-900">{{ prUsageSummary.pendingUpload }}</div>
          </div>
          <div class="rounded-md border border-amber-200 bg-amber-50 p-3">
            <div class="text-xs uppercase tracking-wide text-amber-700">{{ t('repoDetail.noCheckpoint') }}</div>
            <div class="mt-1 text-xl font-semibold text-amber-900">{{ prUsageSummary.noCheckpoint }}</div>
          </div>
          <div class="rounded-md border border-red-200 bg-red-50 p-3">
            <div class="text-xs uppercase tracking-wide text-red-700">{{ t('repoDetail.refreshFailed') }}</div>
            <div class="mt-1 text-xl font-semibold text-red-900">{{ prUsageSummary.refreshFailed }}</div>
          </div>
        </div>

        <div v-if="prs.length > 0" class="mt-3 space-y-3 md:hidden">
          <article v-for="pr in prs" :key="pr.id" class="rounded-lg border border-gray-100 bg-white p-4 shadow-sm">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <a v-if="pr.scm_pr_url" :href="pr.scm_pr_url" target="_blank" rel="noopener noreferrer" class="block truncate text-sm font-semibold text-indigo-700 hover:text-indigo-900">
                  {{ pr.title }}
                </a>
                <div v-else class="truncate text-sm font-semibold text-gray-900">{{ pr.title }}</div>
                <div class="mt-1 truncate text-xs text-gray-500">{{ pr.author || '—' }}</div>
              </div>
              <span
                class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium"
                :class="pr.status === 'merged' ? 'bg-purple-50 text-purple-700' : pr.status === 'open' ? 'bg-green-50 text-green-700' : 'bg-gray-50 text-gray-500'"
              >
                {{ pr.status }}
              </span>
            </div>
            <dl class="mt-3 grid grid-cols-2 gap-3 text-xs">
              <div>
                <dt class="text-gray-400">{{ t('repoDetail.usageStatus') }}</dt>
                <dd class="mt-1 text-gray-800">{{ usageStatusLabel(pr.usage_status) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('repoDetail.tokenUsage') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatPRTokenUsage(pr) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('repoDetail.refreshed') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatDate(pr.usage_refreshed_at || null) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('repoDetail.credits') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatDecimal(pr.usage_credit_usage) }}</dd>
              </div>
            </dl>
            <button
              class="mt-3 rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-40"
              :disabled="isPRDetailLoading(pr.id)"
              type="button"
              @click="togglePRDetails(pr.id)"
            >
              {{ isPRDetailLoading(pr.id) ? t('repoDetail.loading') : expandedPRId === pr.id ? t('repoDetail.hide') : t('repoDetail.details') }}
            </button>
            <div v-if="expandedPRId === pr.id" class="mt-4 space-y-4 border-t border-gray-100 pt-4 text-xs text-gray-700">
              <div v-if="isPRDetailLoading(pr.id) && !prDetails[pr.id]" class="py-4 text-center text-gray-500">
                {{ t('repoDetail.loadingDetails') }}
              </div>
              <template v-else>
                <div class="grid grid-cols-2 gap-3">
                  <div>
                    <div class="text-gray-400">{{ t('repoDetail.input') }}</div>
                    <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_input_tokens) }}</div>
                  </div>
                  <div>
                    <div class="text-gray-400">{{ t('repoDetail.output') }}</div>
                    <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_output_tokens) }}</div>
                  </div>
                  <div>
                    <div class="text-gray-400">{{ t('repoDetail.requests') }}</div>
                    <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_request_count) }}</div>
                  </div>
                  <div>
                    <div class="text-gray-400">{{ t('repoDetail.lastRefreshed') }}</div>
                    <div class="mt-1 font-medium text-gray-900">{{ formatDate(resolvedPR(pr).usage_refreshed_at || null) }}</div>
                  </div>
                </div>
                <details>
                  <summary class="cursor-pointer font-medium text-gray-700">{{ t('repoDetail.commits') }}</summary>
                  <div v-if="commitSnapshots(pr).length > 0" class="mt-2 space-y-2">
                    <div v-for="snapshot in commitSnapshots(pr)" :key="snapshot.commit_sha" class="rounded border border-gray-100 p-3">
                      <div class="break-all font-mono text-gray-900">{{ snapshot.commit_sha }}</div>
                      <dl class="mt-2 grid grid-cols-2 gap-2">
                        <div><dt class="text-gray-400">{{ t('repoDetail.capturedAt') }}</dt><dd>{{ formatDate(snapshot.captured_at || null) }}</dd></div>
                        <div><dt class="text-gray-400">{{ t('repoDetail.tokenUsage') }}</dt><dd>{{ formatCount((snapshot.input_tokens ?? 0) + (snapshot.output_tokens ?? 0) + (snapshot.cached_input_tokens ?? 0)) }}</dd></div>
                        <div><dt class="text-gray-400">{{ t('repoDetail.credits') }}</dt><dd>{{ formatDecimal(snapshot.credit_usage) }}</dd></div>
                        <div><dt class="text-gray-400">{{ t('repoDetail.usageStatus') }}</dt><dd>{{ commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status_reason || usageStatusLabel(commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status) }}</dd></div>
                      </dl>
                    </div>
                  </div>
                  <div v-else class="mt-2 text-gray-500">{{ t('repoDetail.noSnapshot') }}</div>
                </details>
              </template>
            </div>
          </article>
          <div v-if="prsTotal > prsPageSize" class="flex items-center justify-between border-t border-gray-100 pt-3">
            <span class="text-xs text-gray-400">
              {{ prsPage * prsPageSize + 1 }}-{{ Math.min((prsPage + 1) * prsPageSize, prsTotal) }} {{ t('repoDetail.of') }} {{ prsTotal }}
            </span>
            <div class="flex space-x-2">
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40" :disabled="prsPage === 0" @click="prsPrevPage">{{ t('events.prev') }}</button>
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40" :disabled="(prsPage + 1) * prsPageSize >= prsTotal" @click="prsNextPage">{{ t('events.next') }}</button>
            </div>
          </div>
        </div>

        <div v-if="prs.length > 0" class="mt-3 hidden overflow-x-auto md:block">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">{{ t('repoDetail.title') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('repoDetail.author') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('repoDetail.prStatus') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('repoDetail.usageStatus') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('repoDetail.tokenUsage') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('repoDetail.refreshed') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('repos.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <template v-for="pr in prs" :key="pr.id">
                <tr class="hover:bg-gray-50">
                  <td class="max-w-xs truncate px-3 py-2">
                    <a v-if="pr.scm_pr_url" :href="pr.scm_pr_url" target="_blank" rel="noopener noreferrer" class="text-indigo-600 hover:text-indigo-800">
                      {{ pr.title }}
                    </a>
                    <span v-else>{{ pr.title }}</span>
                  </td>
                  <td class="px-3 py-2 text-gray-500">{{ pr.author }}</td>
                  <td class="px-3 py-2">
                    <span
                      class="inline-flex rounded-full px-2 text-xs font-medium leading-5"
                      :class="pr.status === 'merged' ? 'bg-purple-50 text-purple-700' : pr.status === 'open' ? 'bg-green-50 text-green-700' : 'bg-gray-50 text-gray-500'"
                    >{{ pr.status }}</span>
                  </td>
                  <td class="px-3 py-2">
                    <span class="inline-flex rounded-full bg-gray-50 px-2 text-xs font-medium leading-5 text-gray-600" :title="pr.usage_status_reason || ''">
                      {{ usageStatusLabel(pr.usage_status) }}
                    </span>
                  </td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatPRTokenUsage(pr) }}</td>
                  <td class="whitespace-nowrap px-3 py-2 text-xs text-gray-400">{{ formatDate(pr.usage_refreshed_at || null) }}</td>
                  <td class="px-3 py-2">
                    <button
                      class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="isPRDetailLoading(pr.id)"
                      @click="togglePRDetails(pr.id)"
                    >{{ isPRDetailLoading(pr.id) ? t('repoDetail.loading') : expandedPRId === pr.id ? t('repoDetail.hide') : t('repoDetail.details') }}</button>
                  </td>
                </tr>
                <tr v-if="expandedPRId === pr.id" class="bg-gray-50/70">
                  <td colspan="7" class="px-4 py-4">
                    <div v-if="isPRDetailLoading(pr.id) && !prDetails[pr.id]" class="py-6 text-center text-xs text-gray-500">
                      {{ t('repoDetail.loadingDetails') }}
                    </div>
                    <div v-else class="space-y-4 text-xs text-gray-700">
                      <div class="grid gap-3 sm:grid-cols-4">
                        <div>
                          <div class="text-gray-400">{{ t('repoDetail.input') }}</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_input_tokens) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">{{ t('repoDetail.output') }}</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_output_tokens) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">{{ t('repoDetail.credits') }}</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatDecimal(resolvedPR(pr).usage_credit_usage) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">{{ t('repoDetail.lastRefreshed') }}</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatDate(resolvedPR(pr).usage_refreshed_at || null) }}</div>
                        </div>
                      </div>

                      <div>
                        <div class="text-gray-400">{{ t('repoDetail.commits') }}</div>
                        <div v-if="commitSnapshots(pr).length > 0" class="mt-2 overflow-x-auto">
                          <table class="min-w-full divide-y divide-gray-200 text-[11px]">
                            <thead>
                              <tr class="uppercase text-gray-400">
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.commitSha') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.capturedAt') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.input') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.output') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.cache') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.reasoning') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.credits') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.requests') }}</th>
                                <th class="px-2 py-1 text-left font-medium">{{ t('repoDetail.usageStatus') }}</th>
                              </tr>
                            </thead>
                            <tbody class="divide-y divide-gray-100">
                              <tr v-for="snapshot in commitSnapshots(pr)" :key="snapshot.commit_sha">
                                <td class="px-2 py-1 font-mono text-gray-800">{{ snapshot.commit_sha }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatDate(snapshot.captured_at || null) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.input_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.output_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.cached_input_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.reasoning_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatDecimal(snapshot.credit_usage) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.request_count) }}</td>
                                <td class="px-2 py-1 text-gray-700">
                                  {{ commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status_reason || usageStatusLabel(commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status) }}
                                </td>
                              </tr>
                            </tbody>
                          </table>
                        </div>
                        <div v-else class="mt-1 text-gray-500">{{ t('repoDetail.noSnapshot') }}</div>
                      </div>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
          <div v-if="prsTotal > prsPageSize" class="mt-3 flex items-center justify-between border-t border-gray-100 pt-3">
            <span class="text-xs text-gray-400">
              {{ prsPage * prsPageSize + 1 }}–{{ Math.min((prsPage + 1) * prsPageSize, prsTotal) }} {{ t('repoDetail.of') }} {{ prsTotal }}
            </span>
            <div class="flex space-x-2">
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40" :disabled="prsPage === 0" @click="prsPrevPage">{{ t('events.prev') }}</button>
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40" :disabled="(prsPage + 1) * prsPageSize >= prsTotal" @click="prsNextPage">{{ t('events.next') }}</button>
            </div>
          </div>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">{{ t('repoDetail.noPullRequests') }}</p>
      </div>
    </div>
  </AppLayout>
</template>
