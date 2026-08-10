<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { getRepo, repairWebhook, updateRepo } from '@/api/repo'
import { getLatestPRSyncJob, getPR, getPRSyncJob, listPRs, refreshPRUsage, syncPRs } from '@/api/pr'
import { listProviders } from '@/api/scmProvider'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import type { CommitFreshness, PRCommitUsageSnapshot, PRListSummary, PRRecord, PRSyncJob, RepoConfig, SCMProvider, UsageStatus } from '@/types'
import { pullRequestStatusLabel, repositoryStatusLabel } from '@/utils/displayLabels'

const auth = useAuthStore()
const { locale, t } = useI18n()

const props = defineProps<{ repoId: number; repo: RepoConfig }>()
const emit = defineEmits<{ repoUpdated: [RepoConfig] }>()
const repo = ref<RepoConfig>(props.repo)
const prs = ref<PRRecord[]>([])
const prsTotal = ref(0)
const prsSummary = ref<PRListSummary | null>(null)
const prsPage = ref(0)
const prsPageSize = 10
const prsMonths = ref(3)
const prsLoading = ref(false)
const prsLoadError = ref('')
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
const providerOptionsLoading = ref(false)
const providerOptionsLoaded = ref(false)
const providerOptionsError = ref('')
const selectedProviderId = ref<number | null>(props.repo.edges?.scm_provider?.id ?? props.repo.scm_provider_id ?? null)
const selectedProviderValue = computed({
  get: () => selectedProviderId.value ?? 0,
  set: (value: number) => {
    selectedProviderId.value = value === 0 ? null : value
  },
})
const providerOptions = computed(() => {
  const currentProvider = repo.value.edges?.scm_provider
  if (!currentProvider || providers.value.some((provider) => provider.id === currentProvider.id)) {
    return providers.value
  }
  return [currentProvider as SCMProvider, ...providers.value]
})
const bindingSaving = ref(false)
const bindingMessage = ref('')
const bindingMessageTone = ref<'success' | 'error'>('success')
const webhookRepairing = ref(false)
const webhookRepairForce = ref(false)
const webhookRepairMessage = ref('')
const webhookRepairError = ref('')

const repoId = props.repoId
const isRepoUnbound = computed(() => repo.value?.binding_state === 'unbound')
const isWebhookMissing = computed(() => !repo.value?.webhook_id)
const canRepairWebhook = computed(() => (
  auth.isAdmin
  && repo.value?.binding_state === 'bound'
  && repo.value?.status !== 'inactive'
  && (repo.value?.status === 'webhook_failed' || isWebhookMissing.value)
))
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

onMounted(() => {
  if (auth.isAdmin) void loadProviderOptions()
  void Promise.all([loadPRs(), recoverLatestSyncJob()])
})

async function loadProviderOptions() {
  providerOptionsLoading.value = true
  providerOptionsError.value = ''
  try {
    const providersRes = await listProviders()
    const providerData = providersRes.data.data
    providers.value = Array.isArray(providerData) ? providerData : (providerData as any)?.items ?? []
    providerOptionsLoaded.value = true
  } catch (error: any) {
    providerOptionsLoaded.value = false
    providerOptionsError.value = error?.response?.data?.message || error?.message || 'Failed to load code platforms'
  } finally {
    providerOptionsLoading.value = false
  }
}

async function refreshRepo() {
  const repoRes = await getRepo(repoId)
  const refreshed = repoRes.data.data
  if (!refreshed) return
  repo.value = refreshed
  selectedProviderId.value = refreshed.edges?.scm_provider?.id ?? refreshed.scm_provider_id ?? null
  emit('repoUpdated', refreshed)
}

async function loadPRs() {
  prsLoading.value = true
  prsLoadError.value = ''
  try {
    const prsRes = await listPRs(repoId, { limit: prsPageSize, offset: prsPage.value * prsPageSize, months: prsMonths.value })
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0
    prsSummary.value = prData && 'summary' in prData && prData.summary ? prData.summary : null
  } catch (error: any) {
    prsLoadError.value = error?.response?.data?.message || error?.message || t('repoDetail.prListLoadFailed')
  } finally {
    prsLoading.value = false
  }
}

async function recoverLatestSyncJob() {
  try {
    const res = await getLatestPRSyncJob(repoId)
    const job = res.data.data ?? null
    if (!job) return
    syncJob.value = job
    if (!isTerminalJob(job)) {
      syncing.value = true
      syncPollTimer.value = window.setTimeout(() => pollSyncJob(job.id), 1500)
    }
  } catch (error: any) {
    syncMessageTone.value = 'error'
    syncMessage.value = error?.response?.data?.message || t('repoDetail.syncProgressFailed')
  }
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
    bindingMessageTone.value = 'success'
    bindingMessage.value = t('repoDetail.bindingSaved')
  } catch (error: any) {
    bindingMessageTone.value = 'error'
    bindingMessage.value = error?.response?.data?.message || t('repoDetail.bindingSaveFailed')
  } finally {
    bindingSaving.value = false
  }
}

async function clearBinding() {
  selectedProviderId.value = null
  await saveBinding()
}

async function handleRepairWebhook() {
  if (!repo.value) return
  webhookRepairing.value = true
  webhookRepairMessage.value = ''
  webhookRepairError.value = ''
  try {
    const res = await repairWebhook(repoId, { force: webhookRepairForce.value })
    const item = res.data.data
    const failed = item?.webhook_status === 'failed' || item?.status === 'webhook_failed' || Boolean(item?.error)
    if (failed) {
      webhookRepairError.value = item?.error
        ? `${t('repoDetail.webhookRepairFailed')}: ${item.error}`
        : t('repoDetail.webhookRepairFailed')
      await refreshRepo()
      return
    }
    webhookRepairMessage.value = item?.webhook_status === 'registered'
      ? t('repoDetail.webhookRepaired')
      : t('repoDetail.webhookRepairComplete')
    await refreshRepo()
  } catch (error: any) {
    webhookRepairError.value = error?.response?.data?.message || t('repoDetail.webhookRepairFailed')
  } finally {
    webhookRepairing.value = false
  }
}

function handleMonthsChange(value: string | number) {
  prsMonths.value = Number(value)
  prsPage.value = 0
  void loadPRs()
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
  return (pr.usage_input_tokens ?? 0) + (pr.usage_output_tokens ?? 0)
}

function formatPRTokenUsage(pr: PRRecord) {
  const total = totalPRTokens(pr)
  return total > 0 ? formatCount(total) : '—'
}

function totalSnapshotTokens(snapshot: PRCommitUsageSnapshot) {
  return (snapshot.input_tokens ?? 0) + (snapshot.output_tokens ?? 0)
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
    unbound: t('repoDetail.usageUnbound'),
    stale_snapshot: t('repoDetail.usageStale'),
    refresh_failed: t('repoDetail.usageFailed'),
    unknown: t('repoDetail.usageUnknown'),
  }
  return labels[status ?? 'unknown']
}

function usageStatusHelp(status?: UsageStatus, reason?: string | null) {
  if (reason) return reason
  const labels: Record<UsageStatus, string> = {
    fresh: t('repoDetail.usageFreshHelp'),
    pending_upload: t('repoDetail.usagePendingHelp'),
    no_checkpoint: t('repoDetail.noCheckpointHelp'),
    no_usage_events: t('repoDetail.usageNoUsageHelp'),
    unbound: t('repoDetail.usageUnboundHelp'),
    stale_snapshot: t('repoDetail.usageStaleHelp'),
    refresh_failed: t('repoDetail.usageFailedHelp'),
    unknown: t('repoDetail.usageUnknownHelp'),
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
  <section class="space-y-5" data-testid="repo-operations">
    <div class="flex flex-col items-start gap-1 sm:items-end">
      <ElButton
        data-testid="repo-sync-prs"
        :loading="syncing"
        :disabled="syncing || isRepoUnbound"
        :title="syncDisabledReason"
        @click="handleSyncPRs"
      >{{ syncing ? t('repoDetail.syncing') : t('repoDetail.syncPrs') }}</ElButton>
      <p v-if="syncDisabledReason" class="max-w-xs text-xs text-amber-700">{{ syncDisabledReason }}</p>
    </div>

<ElAlert
  v-if="syncMessage"
  :type="syncMessageTone"
  :closable="false"
  show-icon
  :title="syncMessage"
/>

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
  <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
    <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('repoDetail.scmBinding') }}</h2>
    <ElTag :type="isRepoUnbound ? 'warning' : 'success'" effect="light" size="small">
      {{ isRepoUnbound ? t('repoDetail.unbound') : t('repoDetail.bound') }}
    </ElTag>
  </div>
  <p class="mt-3 text-sm text-gray-500">
    {{ isRepoUnbound ? t('repoDetail.bindingUnboundHelp') : t('repoDetail.bindingBoundHelp') }}
  </p>
  <div v-if="providerOptionsLoading" class="mt-4 text-sm text-gray-500">
{{ t('repoDetail.loading') }}
    </div>
    <div v-else-if="providerOptionsLoaded" data-testid="repo-binding-controls" class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
    <ElSelect
      v-model="selectedProviderValue"
      data-testid="repo-provider-select"
      class="w-full sm:max-w-sm"
      :teleported="false"
    >
      <ElOption :value="0" :label="t('repoDetail.unbound')" />
      <ElOption v-for="provider in providerOptions" :key="provider.id" :value="provider.id" :label="provider.name" />
    </ElSelect>
    <div class="flex gap-2">
      <ElButton
        data-testid="repo-save-binding"
        type="primary"
        :loading="bindingSaving"
        :disabled="bindingSaving"
        @click="saveBinding"
      >
        {{ bindingSaving ? t('repoDetail.saving') : t('repoDetail.saveBinding') }}
      </ElButton>
      <ElButton
        :disabled="bindingSaving || selectedProviderId == null"
        @click="clearBinding"
      >
        {{ t('repoDetail.clearBinding') }}
      </ElButton>
    </div>
  </div>
    <ElAlert v-else-if="providerOptionsError" class="mt-4" type="error" :closable="false" show-icon :title="providerOptionsError">
<ElButton class="mt-2" type="danger" link size="small" @click="loadProviderOptions">
  {{ t('repoDetail.retry') }}
</ElButton>
    </ElAlert>
  <ElAlert v-if="bindingMessage" class="mt-3" :type="bindingMessageTone" :closable="false" show-icon :title="bindingMessage" />
</div>

<div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
  <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
    <div>
      <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.health') }}</h2>
      <p class="mt-1 text-sm text-slate-600">
        {{ t('repoDetail.healthHelp') }}
      </p>
    </div>
    <ElTag :type="isRepoUnbound ? 'warning' : 'success'" effect="light">
      {{ bindingLabel() }}
    </ElTag>
  </div>
  <div data-testid="repo-detail-health-metrics" class="mt-4 grid grid-cols-2 gap-3 xl:grid-cols-4">
    <div class="rounded-md bg-slate-50 p-3">
      <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repos.defaultBranch') }}</div>
      <div class="mt-1 font-medium text-slate-900">{{ repo.default_branch }}</div>
    </div>
    <div class="rounded-md bg-slate-50 p-3">
      <div class="text-xs uppercase tracking-wide text-slate-500">{{ t('repoDetail.repositoryStatus') }}</div>
      <div class="mt-1">
        <ElTag :type="repo.status === 'active' ? 'success' : repo.status === 'webhook_failed' ? 'danger' : 'info'" size="small">
          {{ repositoryStatusLabel(repo.status, t) }}
        </ElTag>
      </div>
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
  <div
    v-if="canRepairWebhook"
    class="mt-4 flex flex-col gap-3 rounded-md border border-amber-200 bg-amber-50 p-3 sm:flex-row sm:items-center sm:justify-between"
  >
    <div class="text-sm text-amber-900">
      <div class="font-medium">{{ t('repoDetail.webhookRepairNeeded') }}</div>
      <ElCheckbox v-if="repo.webhook_id" v-model="webhookRepairForce" class="mt-2">
        {{ t('repoDetail.forceReplaceWebhook') }}
      </ElCheckbox>
    </div>
    <ElButton
      data-testid="repo-repair-webhook-button"
      type="warning"
      :loading="webhookRepairing"
      :disabled="webhookRepairing"
      @click="handleRepairWebhook"
    >
      {{ webhookRepairing ? t('repoDetail.webhookRepairing') : t('repoDetail.repairWebhook') }}
    </ElButton>
  </div>
  <ElAlert v-if="webhookRepairMessage" class="mt-3" type="success" :closable="false" show-icon :title="webhookRepairMessage" />
  <ElAlert v-if="webhookRepairError" class="mt-3" type="error" :closable="false" show-icon :title="webhookRepairError" />
</div>

<div class="rounded-lg bg-white p-5 shadow">
  <div data-testid="repo-pr-summary-header" class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
    <div>
      <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('repoDetail.prUsageSummary') }}</h2>
      <p class="mt-1 text-sm text-gray-500">{{ t('repoDetail.prUsageHelp') }}</p>
    </div>
    <div data-testid="repo-pr-summary-controls" class="flex w-full flex-wrap items-center gap-3 lg:w-auto lg:shrink-0 lg:flex-nowrap">
      <ElSelect
        data-testid="repo-pr-range"
        :model-value="prsMonths"
        class="w-full min-w-0 sm:!w-40 sm:shrink-0"
        @change="handleMonthsChange"
      >
        <ElOption :value="1" :label="t('repoDetail.month1')" />
        <ElOption :value="3" :label="t('repoDetail.month3')" />
        <ElOption :value="6" :label="t('repoDetail.month6')" />
        <ElOption :value="12" :label="t('repoDetail.month12')" />
        <ElOption :value="0" :label="t('repoDetail.allTime')" />
      </ElSelect>
      <span v-if="prsTotal > 0" class="text-xs text-gray-400">{{ prsTotal }} {{ t('repoDetail.totalSuffix') }}</span>
    </div>
  </div>

  <div data-testid="repo-detail-pr-metrics" class="mt-4 grid grid-cols-2 gap-3 xl:grid-cols-5">
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

  <ElAlert v-if="prsLoadError" class="mt-4" type="error" :closable="false" show-icon :title="t('repoDetail.prListLoadFailed')">
    <ElButton
      class="mt-2"
      type="danger"
      link
      size="small"
      @click="loadPRs"
    >
      {{ t('repoDetail.retry') }}
    </ElButton>
  </ElAlert>

  <div v-if="prs.length > 0" class="mt-3 divide-y divide-gray-100 border-y border-gray-100">
    <article v-for="pr in prs" :key="pr.id" data-testid="repo-pr-row" class="py-4">
      <div data-testid="repo-pr-summary-grid" class="lg:grid lg:grid-cols-[minmax(0,1.3fr)_minmax(320px,1fr)_auto] lg:items-center lg:gap-5">
        <div data-testid="repo-pr-identity" class="flex min-w-0 items-start justify-between gap-3 overflow-hidden">
          <div class="min-w-0 flex-1 overflow-hidden">
            <ElLink v-if="pr.scm_pr_url" data-testid="repo-pr-title" :href="pr.scm_pr_url" target="_blank" rel="noopener noreferrer" underline="never" class="repo-pr-title min-w-0 max-w-full text-sm font-semibold text-indigo-700 hover:text-indigo-900" :title="pr.title">
              <span class="block min-w-0 truncate">{{ pr.title }}</span>
            </ElLink>
            <div v-else data-testid="repo-pr-title" class="min-w-0 max-w-full truncate text-sm font-semibold text-gray-900" :title="pr.title"><span class="truncate">{{ pr.title }}</span></div>
            <div class="mt-1 truncate text-xs text-gray-500">{{ pr.author || '—' }}</div>
          </div>
          <ElTag
            class="shrink-0"
            :type="pr.status === 'open' ? 'success' : pr.status === 'merged' ? 'primary' : 'info'"
            size="small"
          >
            {{ pullRequestStatusLabel(pr.status, t) }}
          </ElTag>
        </div>
        <dl data-testid="repo-pr-summary-metrics" class="mt-3 grid grid-cols-2 gap-3 text-xs lg:mt-0 lg:grid-cols-4">
          <div>
            <dt class="text-gray-400">{{ t('repoDetail.usageStatus') }}</dt>
            <dd class="mt-1 text-gray-800" :title="usageStatusHelp(pr.usage_status, pr.usage_status_reason)">{{ usageStatusLabel(pr.usage_status) }}</dd>
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
        <ElButton
          data-testid="repo-pr-details-button"
          class="mt-3 lg:mt-0"
          size="small"
          :loading="isPRDetailLoading(pr.id)"
          :disabled="isPRDetailLoading(pr.id)"
          @click="togglePRDetails(pr.id)"
        >
          {{ isPRDetailLoading(pr.id) ? t('repoDetail.loading') : expandedPRId === pr.id ? t('repoDetail.hide') : t('repoDetail.details') }}
        </ElButton>
      </div>
      <div v-if="expandedPRId === pr.id" data-testid="repo-pr-detail" class="mt-4 space-y-4 border-t border-gray-100 pt-4 text-xs text-gray-700">
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
  <div>
    <div class="font-medium text-gray-700">{{ t('repoDetail.commits') }}</div>
            <div v-if="commitSnapshots(pr).length > 0" class="mt-2 space-y-2">
    <div v-for="snapshot in commitSnapshots(pr)" :key="snapshot.commit_sha" class="border-t border-gray-100 py-3 first:border-t-0">
      <div class="text-gray-400">{{ t('repoDetail.commitSha') }}</div>
      <div class="mt-1 break-all font-mono text-gray-900">{{ snapshot.commit_sha }}</div>
      <dl class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
                  <div><dt class="text-gray-400">{{ t('repoDetail.capturedAt') }}</dt><dd>{{ formatDate(snapshot.captured_at || null) }}</dd></div>
      <div><dt class="text-gray-400">{{ t('repoDetail.input') }}</dt><dd>{{ formatCount(snapshot.input_tokens) }}</dd></div>
      <div><dt class="text-gray-400">{{ t('repoDetail.output') }}</dt><dd>{{ formatCount(snapshot.output_tokens) }}</dd></div>
      <div><dt class="text-gray-400">{{ t('repoDetail.cache') }}</dt><dd>{{ formatCount(snapshot.cached_input_tokens) }}</dd></div>
      <div><dt class="text-gray-400">{{ t('repoDetail.reasoning') }}</dt><dd>{{ formatCount(snapshot.reasoning_tokens) }}</dd></div>
                  <div><dt class="text-gray-400">{{ t('repoDetail.credits') }}</dt><dd>{{ formatDecimal(snapshot.credit_usage) }}</dd></div>
      <div><dt class="text-gray-400">{{ t('repoDetail.requests') }}</dt><dd>{{ formatCount(snapshot.request_count) }}</dd></div>
                  <div><dt class="text-gray-400">{{ t('repoDetail.usageStatus') }}</dt><dd>{{ commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status_reason || usageStatusLabel(commitFreshnessFor(pr, snapshot.commit_sha)?.usage_status) }}</dd></div>
                </dl>
              </div>
            </div>
            <div v-else class="mt-2 text-gray-500">{{ t('repoDetail.noSnapshot') }}</div>
  </div>
        </template>
      </div>
    </article>
    <div v-if="prsTotal > prsPageSize" class="flex items-center justify-between border-t border-gray-100 pt-3">
      <span class="text-xs text-gray-400">
        {{ prsPage * prsPageSize + 1 }}-{{ Math.min((prsPage + 1) * prsPageSize, prsTotal) }} {{ t('repoDetail.of') }} {{ prsTotal }}
      </span>
      <div class="flex space-x-2">
        <ElButton size="small" :disabled="prsPage === 0" @click="prsPrevPage">{{ t('repos.previousPage') }}</ElButton>
        <ElButton size="small" :disabled="(prsPage + 1) * prsPageSize >= prsTotal" @click="prsNextPage">{{ t('repos.nextPage') }}</ElButton>
      </div>
    </div>
  </div>

  <ElSkeleton v-else-if="prsLoading" class="mt-3" :rows="3" animated />
  <ElEmpty v-else-if="!prsLoadError" class="mt-3" :description="t('repoDetail.noPullRequests')" />
</div>

  </section>
</template>

<style>
.repo-pr-title .el-link__inner {
  min-width: 0;
  max-width: 100%;
}
</style>
