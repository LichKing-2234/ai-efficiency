<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from '@/i18n'
import { accountQuotaMessages } from '@/locales/accountQuota'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { formatBeijingDateTime } from '@/utils/opsMonitoring'
import {
  getLatestAccountQuotaSweep,
  listAccountQuotas,
  startAccountQuotaSweep,
  type AccountQuotaAccount,
  type AccountQuotaCredits,
  type AccountQuotaItem,
  type AccountQuotaObservation,
  type AccountQuotaResetCredits,
  type AccountQuotaSweep,
  type AccountQuotaSweepResult,
  type AccountQuotaSweepStatus,
  type AccountQuotaUsageWindow,
  type StartAccountQuotaSweepInput,
} from '@/api/accountQuota'

const props = defineProps<{
  providerId: number
  platform: string
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'accountQuota.', accountQuotaMessages)

const POLL_INTERVAL_MS = 2000
const WINDOW_5H_SECONDS = 18000
const WINDOW_7D_SECONDS = 604800

interface AccountQuotaRow {
  item: AccountQuotaItem
  account: AccountQuotaAccount
  window5h: AccountQuotaUsageWindow | null
  window7d: AccountQuotaUsageWindow | null
  creditsLabel: string
  resetCreditsLabel: string
  nearestExpiry: string
  observedAt: string
  hasObservation: boolean
  planType: string
  groupNames: string
  result: AccountQuotaSweepResult | null
  resultCodeText: string
  selected: boolean
}

const items = ref<AccountQuotaItem[]>([])
const latestSweep = ref<AccountQuotaSweep | null>(null)
const loading = ref(false)
const starting = ref(false)
const error = ref('')
const selectedIDs = ref<Set<number>>(new Set())
const search = ref('')
const statusFilter = ref('')
const groupFilter = ref('')
const resetCreditsFilter = ref('')
const onlyFailed = ref(false)
const onlyUnobserved = ref(false)
const confirmOpen = ref(false)
const confirmMode = ref<'all' | 'selected'>('all')
const includeAutoReset = ref(false)

// A later provider/platform context supersedes every read still in flight.
let listRequest = 0
let sweepContext = 0
let pollTimer: ReturnType<typeof setInterval> | null = null
let pollInFlight = false
let disposed = false

const isOpenAI = computed(() => props.platform === 'openai')
const isSweepActive = computed(() => {
  const status = latestSweep.value?.status
  return status === 'queued' || status === 'running'
})
const busy = computed(() => starting.value || isSweepActive.value)
const eligibleCount = computed(() => items.value.filter((item) => !item.account.auto_reset_credit_enabled).length)
const selectedCount = computed(() => selectedIDs.value.size)
const resultsByAccount = computed(() => {
  const buckets = new Map<number, AccountQuotaSweepResult>()
  for (const result of latestSweep.value?.results ?? []) buckets.set(result.account_id, result)
  return buckets
})
const statusOptions = computed(() => Array.from(new Set(items.value.map((item) => item.account.status).filter(Boolean))).sort())
const groupOptions = computed(() => {
  const buckets = new Map<number, string>()
  for (const item of items.value) {
    for (const group of item.account.groups ?? []) buckets.set(group.id, group.name)
  }
  return Array.from(buckets, ([id, name]) => ({ id, name })).sort((left, right) => left.name.localeCompare(right.name))
})
const visibleItems = computed(() => items.value.filter(matchesFilters))
const visibleRows = computed(() => visibleItems.value.map(buildRow))
const progressPercent = computed(() => {
  const sweep = latestSweep.value
  if (!sweep || sweep.total_count <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((sweep.processed_count / sweep.total_count) * 100)))
})
const lastSweepLabel = computed(() => {
  const sweep = latestSweep.value
  if (!sweep) return t('accountQuota.noSweepYet')
  const time = formatBeijingDateTime(sweep.finished_at ?? sweep.started_at ?? sweep.created_at)
  const timeLabel = time ? t('accountQuota.lastSweep', { time }) : t('accountQuota.lastSweepUnknownTime')
  return `${sweepStatusLabel(sweep.status)} · ${timeLabel}`
})
const confirmItems = computed(() => (confirmMode.value === 'all'
  ? items.value
  : items.value.filter((item) => selectedIDs.value.has(item.account.id))))
const confirmAutoResetItems = computed(() => confirmItems.value.filter((item) => item.account.auto_reset_credit_enabled))
const confirmTargetItems = computed(() => (includeAutoReset.value
  ? confirmItems.value
  : confirmItems.value.filter((item) => !item.account.auto_reset_credit_enabled)))
const confirmTargetCount = computed(() => confirmTargetItems.value.length)
const confirmScopeLabel = computed(() => (confirmMode.value === 'all'
  ? t('accountQuota.confirmScopeAll')
  : t('accountQuota.confirmScopeSelected', { count: confirmItems.value.length })))
const confirmCallsNote = computed(() => t('accountQuota.confirmCallsNote', { count: confirmTargetCount.value }))

function normalizeSweep(value: AccountQuotaSweep | null | undefined): AccountQuotaSweep | null {
  return value ?? null
}

function sweepStatusLabel(status: AccountQuotaSweepStatus): string {
  switch (status) {
    case 'queued':
      return t('accountQuota.sweepStatusQueued')
    case 'running':
      return t('accountQuota.sweepStatusRunning')
    case 'completed':
      return t('accountQuota.sweepStatusCompleted')
    case 'failed':
      return t('accountQuota.sweepStatusFailed')
    case 'abandoned':
      return t('accountQuota.sweepStatusAbandoned')
    default:
      return status
  }
}

function stopPolling() {
  if (pollTimer != null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function ensurePolling() {
  if (disposed || pollTimer != null) return
  pollTimer = setInterval(() => {
    void pollLatestSweep()
  }, POLL_INTERVAL_MS)
}

async function pollLatestSweep() {
  if (disposed || pollInFlight || !isOpenAI.value || !props.providerId) return
  const context = sweepContext
  pollInFlight = true
  try {
    const response = await getLatestAccountQuotaSweep({ provider_id: props.providerId })
    if (disposed || context !== sweepContext) return
    applySweep(normalizeSweep(response.data.data), { announceTerminal: true })
  } catch {
    // A transient poll failure keeps the interval alive; the next tick retries.
  } finally {
    pollInFlight = false
  }
}

function applySweep(next: AccountQuotaSweep | null, options: { announceTerminal?: boolean } = {}) {
  latestSweep.value = next
  if (isSweepActive.value) {
    ensurePolling()
    return
  }
  stopPolling()
  if (options.announceTerminal && next) finishSweep(next)
}

function finishSweep(sweep: AccountQuotaSweep) {
  if (sweep.status === 'completed' && sweep.failed_count === 0 && sweep.skipped_count === 0) {
    ElMessage.success(t('accountQuota.sweepFinished', { count: sweep.success_count }))
  } else {
    ElMessage.warning(t('accountQuota.sweepFinishedWithIssues', {
      success: sweep.success_count,
      failed: sweep.failed_count,
      skipped: sweep.skipped_count,
    }))
  }
  void loadList()
}

function resetState() {
  stopPolling()
  items.value = []
  latestSweep.value = null
  error.value = ''
  loading.value = false
  starting.value = false
  selectedIDs.value = new Set()
  search.value = ''
  statusFilter.value = ''
  groupFilter.value = ''
  resetCreditsFilter.value = ''
  onlyFailed.value = false
  onlyUnobserved.value = false
  confirmOpen.value = false
  includeAutoReset.value = false
}

function pruneSelection() {
  const available = new Set(items.value.map((item) => item.account.id))
  selectedIDs.value = new Set(Array.from(selectedIDs.value).filter((id) => available.has(id)))
}

async function loadList() {
  if (!isOpenAI.value || !props.providerId) {
    items.value = []
    latestSweep.value = null
    error.value = ''
    stopPolling()
    return
  }
  const request = ++listRequest
  loading.value = true
  error.value = ''
  try {
    const response = await listAccountQuotas({ provider_id: props.providerId, platform: props.platform })
    if (disposed || request !== listRequest) return
    const page = response.data.data
    items.value = page?.items ?? []
    pruneSelection()
    const pageSweep = normalizeSweep(page?.latest_sweep)
    // Do not drop a locally tracked running sweep when the page read predates it.
    if (pageSweep != null || !isSweepActive.value) applySweep(pageSweep)
  } catch (err: any) {
    if (disposed || request !== listRequest) return
    items.value = []
    error.value = err?.response?.data?.message || err?.message || t('accountQuota.loadFailed')
  } finally {
    if (!disposed && request === listRequest) loading.value = false
  }
}

watch(
  () => [props.providerId, props.platform] as const,
  () => {
    resetState()
    ++listRequest
    ++sweepContext
    if (isOpenAI.value) void loadList()
  },
  { immediate: true },
)

onUnmounted(() => {
  disposed = true
  stopPolling()
})

function isSelected(accountID: number): boolean {
  return selectedIDs.value.has(accountID)
}

function toggleSelection(accountID: number, checked: boolean) {
  if (busy.value) return
  const next = new Set(selectedIDs.value)
  if (checked) next.add(accountID)
  else next.delete(accountID)
  selectedIDs.value = next
}

function openConfirm(mode: 'all' | 'selected') {
  if (busy.value || !isOpenAI.value) return
  if (mode === 'all' && items.value.length === 0) return
  confirmMode.value = mode
  includeAutoReset.value = false
  confirmOpen.value = true
}

async function confirmStart() {
  if (!confirmOpen.value || busy.value) return
  const targets = confirmTargetItems.value
  if (targets.length === 0) {
    ElMessage.warning(t('accountQuota.confirmNoTargets'))
    return
  }
  const payload: StartAccountQuotaSweepInput = {
    provider_id: props.providerId,
    platform: 'openai',
  }
  if (confirmMode.value === 'selected') payload.account_ids = targets.map((item) => item.account.id)
  if (includeAutoReset.value) payload.include_auto_reset = true
  const context = sweepContext
  starting.value = true
  try {
    const response = await startAccountQuotaSweep(payload)
    if (disposed || context !== sweepContext) return
    confirmOpen.value = false
    applySweep(normalizeSweep(response.data.data), { announceTerminal: true })
  } catch (err: any) {
    if (disposed || context !== sweepContext) return
    ElMessage.error(err?.response?.data?.message || err?.message || t('accountQuota.sweepStartFailed'))
  } finally {
    if (!disposed) starting.value = false
  }
}

function resultFor(item: AccountQuotaItem): AccountQuotaSweepResult | null {
  return resultsByAccount.value.get(item.account.id) ?? null
}

function matchesFilters(item: AccountQuotaItem): boolean {
  const query = search.value.trim().toLowerCase()
  if (query) {
    const name = (item.account.name ?? '').toLowerCase()
    const email = (item.account.email ?? '').toLowerCase()
    if (!name.includes(query) && !email.includes(query)) return false
  }
  if (statusFilter.value && item.account.status !== statusFilter.value) return false
  if (groupFilter.value && !(item.account.groups ?? []).some((group) => String(group.id) === groupFilter.value)) return false
  if (resetCreditsFilter.value && resetCreditsBucket(item) !== resetCreditsFilter.value) return false
  if (onlyFailed.value && resultFor(item)?.status !== 'failed') return false
  if (onlyUnobserved.value && item.observation != null) return false
  return true
}

function observationFor(item: AccountQuotaItem): AccountQuotaObservation | null {
  return item.observation ?? null
}

function creditsFor(item: AccountQuotaItem): AccountQuotaCredits | null {
  return item.observation?.credits ?? item.snapshot?.credits ?? null
}

function resetCreditsFor(item: AccountQuotaItem): AccountQuotaResetCredits | null {
  return item.observation?.reset_credits ?? item.snapshot?.reset_credits ?? null
}

function windowFor(item: AccountQuotaItem, windowSeconds: number): AccountQuotaUsageWindow | null {
  return item.observation?.windows?.find((window) => window.window_seconds === windowSeconds) ?? null
}

function resetCreditsBucket(item: AccountQuotaItem): string {
  const resetCredits = resetCreditsFor(item)
  if (!resetCredits) return 'unknown'
  if (resetCredits.available >= 3) return '3+'
  return String(Math.max(0, resetCredits.available))
}

function statusLabel(status: string): string {
  switch (status) {
    case 'active':
      return t('accountQuota.accountStatusActive')
    case 'error':
      return t('accountQuota.accountStatusError')
    case 'disabled':
      return t('accountQuota.accountStatusDisabled')
    default:
      return status || '—'
  }
}

function statusTagType(status: string): 'success' | 'danger' | 'info' | 'warning' {
  switch (status) {
    case 'active':
      return 'success'
    case 'error':
      return 'danger'
    case 'disabled':
      return 'info'
    default:
      return 'warning'
  }
}

function formatPercent(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return `${Number.isInteger(value) ? value : value.toFixed(1)}%`
}

function creditsLabelFor(credits: AccountQuotaCredits | null): string {
  if (!credits) return '—'
  if (credits.unlimited) return t('accountQuota.creditsUnlimited')
  if (!credits.has_credits) return t('accountQuota.creditsNone')
  return credits.balance ?? '0'
}

function nearestExpiryFor(resetCredits: AccountQuotaResetCredits | null): string {
  if (!resetCredits || resetCredits.available <= 0) return ''
  let nearest: number | null = null
  for (const raw of resetCredits.expires_at ?? []) {
    const time = new Date(raw).getTime()
    if (Number.isNaN(time)) continue
    if (nearest == null || time < nearest) nearest = time
  }
  return nearest == null ? '' : formatBeijingDateTime(new Date(nearest).toISOString())
}

function reasonCodeLabel(code: string): string {
  switch (code) {
    case 'reauth-required':
      return t('accountQuota.errorCodeReauthRequired')
    case 'rate-limited':
      return t('accountQuota.errorCodeRateLimited')
    case 'upstream-unavailable':
      return t('accountQuota.errorCodeUpstreamUnavailable')
    case 'timeout':
      return t('accountQuota.errorCodeTimeout')
    case 'capability-unsupported':
      return t('accountQuota.errorCodeCapabilityUnsupported')
    case 'compliance-ack-required':
      return t('accountQuota.errorCodeComplianceAckRequired')
    case 'upstream-outcome-unknown':
      return t('accountQuota.errorCodeUpstreamOutcomeUnknown')
    case 'auto-reset-enabled':
      return t('accountQuota.errorCodeAutoResetEnabled')
    default:
      return code
  }
}

function reasonCodeText(code: string): string {
  if (!code) return ''
  const label = reasonCodeLabel(code)
  return label === code ? code : `${label} (${code})`
}

function buildRow(item: AccountQuotaItem): AccountQuotaRow {
  const result = resultFor(item)
  const resetCredits = resetCreditsFor(item)
  const observedAtValue = observationFor(item)?.observed_at ?? item.snapshot?.observed_at ?? ''
  return {
    item,
    account: item.account,
    window5h: windowFor(item, WINDOW_5H_SECONDS),
    window7d: windowFor(item, WINDOW_7D_SECONDS),
    creditsLabel: creditsLabelFor(creditsFor(item)),
    resetCreditsLabel: resetCredits ? String(resetCredits.available) : '—',
    nearestExpiry: nearestExpiryFor(resetCredits),
    observedAt: formatBeijingDateTime(observedAtValue),
    hasObservation: item.observation != null,
    planType: item.observation?.plan_type ?? '',
    groupNames: (item.account.groups ?? []).map((group) => group.name).filter(Boolean).join(', '),
    result,
    resultCodeText: result ? reasonCodeText(result.error_code) : '',
    selected: selectedIDs.value.has(item.account.id),
  }
}
</script>

<template>
  <div class="space-y-4" data-testid="account-quota-panel">
    <section
      v-if="!isOpenAI"
      class="rounded-lg border border-slate-200 bg-white p-5 text-sm text-slate-500"
      data-testid="quota-platform-hint"
    >
      {{ t('accountQuota.platformHint') }}
    </section>

    <template v-else>
      <section class="rounded-lg border border-slate-200 bg-white p-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="min-w-0">
            <div class="text-sm font-semibold text-slate-900" data-testid="quota-counts">
              {{ t('accountQuota.totalAccounts', { count: items.length }) }} · {{ t('accountQuota.eligibleAccounts', { count: eligibleCount }) }}
            </div>
            <div class="mt-1 text-xs text-slate-500" data-testid="quota-last-sweep">{{ lastSweepLabel }}</div>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <el-button
              data-testid="quota-refresh-all"
              type="primary"
              :loading="starting && confirmMode === 'all'"
              :disabled="busy || items.length === 0"
              @click="openConfirm('all')"
            >{{ t('accountQuota.refreshAll') }}</el-button>
            <el-button
              data-testid="quota-refresh-selected"
              :loading="starting && confirmMode === 'selected'"
              :disabled="busy || selectedCount === 0"
              @click="openConfirm('selected')"
            >{{ t('accountQuota.refreshSelected') }}</el-button>
            <el-button data-testid="quota-reload" :icon="Refresh" :loading="loading" @click="loadList">{{ t('accountQuota.reload') }}</el-button>
          </div>
        </div>

        <div v-if="isSweepActive && latestSweep" class="mt-3" data-testid="quota-progress">
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-600">
            <span>{{ t('accountQuota.sweepRunning') }}</span>
            <span class="tabular-nums" data-testid="quota-progress-counts">
              {{ t('accountQuota.sweepProgress', { processed: latestSweep.processed_count, total: latestSweep.total_count }) }}
            </span>
          </div>
          <el-progress class="mt-1" :percentage="progressPercent" :stroke-width="10" :show-text="false" />
          <div class="mt-2 flex flex-wrap gap-2">
            <el-tag size="small" type="success">{{ t('accountQuota.sweepPillSuccess', { count: latestSweep.success_count }) }}</el-tag>
            <el-tag size="small" type="danger">{{ t('accountQuota.sweepPillFailed', { count: latestSweep.failed_count }) }}</el-tag>
            <el-tag size="small" type="info">{{ t('accountQuota.sweepPillSkipped', { count: latestSweep.skipped_count }) }}</el-tag>
          </div>
        </div>

        <div v-else class="mt-3 flex flex-wrap items-center gap-2 text-xs text-slate-500">
          <el-tag size="small" type="success">{{ t('accountQuota.sweepPillSuccess', { count: latestSweep?.success_count ?? 0 }) }}</el-tag>
          <el-tag size="small" type="danger">{{ t('accountQuota.sweepPillFailed', { count: latestSweep?.failed_count ?? 0 }) }}</el-tag>
          <el-tag size="small" type="info">{{ t('accountQuota.sweepPillSkipped', { count: latestSweep?.skipped_count ?? 0 }) }}</el-tag>
        </div>

        <el-alert
          v-if="latestSweep?.last_error"
          class="mt-3"
          type="error"
          :closable="false"
          data-testid="quota-sweep-error"
          :title="t('accountQuota.sweepLastError', { message: latestSweep.last_error })"
        />
        <el-alert v-if="error" class="mt-3" type="error" :closable="false" data-testid="quota-error" :title="error" />
      </section>

      <section class="rounded-lg border border-slate-200 bg-white p-4">
        <div class="mb-3 flex flex-wrap items-center gap-2">
          <el-input
            v-model="search"
            data-testid="quota-search"
            size="small"
            class="w-56"
            clearable
            :placeholder="t('accountQuota.searchPlaceholder')"
          />
          <el-select v-model="statusFilter" data-testid="quota-status-filter" size="small" class="w-40">
            <el-option :label="t('accountQuota.allStatuses')" value="" />
            <el-option v-for="status in statusOptions" :key="status" :label="statusLabel(status)" :value="status" />
          </el-select>
          <el-select v-model="groupFilter" data-testid="quota-group-filter" size="small" class="w-44">
            <el-option :label="t('accountQuota.allGroups')" value="" />
            <el-option v-for="group in groupOptions" :key="group.id" :label="group.name" :value="String(group.id)" />
          </el-select>
          <el-select v-model="resetCreditsFilter" data-testid="quota-reset-filter" size="small" class="w-44">
            <el-option :label="t('accountQuota.allResetCredits')" value="" />
            <el-option :label="t('accountQuota.resetCreditsNone')" value="0" />
            <el-option :label="t('accountQuota.resetCreditsOne')" value="1" />
            <el-option :label="t('accountQuota.resetCreditsTwo')" value="2" />
            <el-option :label="t('accountQuota.resetCreditsThreePlus')" value="3+" />
            <el-option :label="t('accountQuota.resetCreditsUnknown')" value="unknown" />
          </el-select>
          <el-checkbox v-model="onlyFailed" data-testid="quota-only-failed">{{ t('accountQuota.onlyFailed') }}</el-checkbox>
          <el-checkbox v-model="onlyUnobserved" data-testid="quota-only-unobserved">{{ t('accountQuota.onlyUnobserved') }}</el-checkbox>
        </div>

        <el-table
          data-testid="quota-table"
          :data="visibleRows"
          stripe
          :row-key="(row) => String(row.account.id)"
        >
          <el-table-column :label="t('accountQuota.select')" width="64">
            <template #default="scope">
              <el-checkbox
                :data-testid="`quota-select-${scope.row.account.id}`"
                :model-value="scope.row.selected"
                :disabled="busy"
                @change="(value: string | number | boolean) => toggleSelection(scope.row.account.id, value === true)"
              />
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.account')" min-width="220">
            <template #default="scope">
              <div :data-testid="`quota-row-${scope.row.account.id}`">
                <div class="break-words font-medium text-slate-900">{{ scope.row.account.name }}</div>
                <div class="break-all text-xs text-slate-500">{{ scope.row.account.email }} · #{{ scope.row.account.id }}</div>
                <div v-if="scope.row.account.shadow || scope.row.account.auto_reset_credit_enabled" class="mt-1 flex flex-wrap gap-1">
                  <el-tag v-if="scope.row.account.shadow" size="small" type="info">{{ t('accountQuota.shadowTag') }}</el-tag>
                  <el-tag v-if="scope.row.account.auto_reset_credit_enabled" size="small" type="warning">{{ t('accountQuota.autoResetTag') }}</el-tag>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.status')" width="110">
            <template #default="scope">
              <el-tag :type="statusTagType(scope.row.account.status)">{{ statusLabel(scope.row.account.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.groups')" min-width="140">
            <template #default="scope">
              <span class="break-words text-xs text-slate-600">{{ scope.row.groupNames || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.window5h')" min-width="150">
            <template #default="scope">
              <div :data-testid="`quota-window-5h-${scope.row.account.id}`">
                <template v-if="scope.row.window5h">
                  <div class="tabular-nums font-medium text-slate-900">{{ formatPercent(scope.row.window5h.used_percent) }}</div>
                  <div class="text-xs text-slate-500">
                    {{ scope.row.window5h.resets_at ? t('accountQuota.windowReset', { time: formatBeijingDateTime(scope.row.window5h.resets_at) }) : t('accountQuota.noResetTime') }}
                  </div>
                </template>
                <span v-else class="text-slate-400">—</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.window7d')" min-width="150">
            <template #default="scope">
              <div :data-testid="`quota-window-7d-${scope.row.account.id}`">
                <template v-if="scope.row.window7d">
                  <div class="tabular-nums font-medium text-slate-900">{{ formatPercent(scope.row.window7d.used_percent) }}</div>
                  <div class="text-xs text-slate-500">
                    {{ scope.row.window7d.resets_at ? t('accountQuota.windowReset', { time: formatBeijingDateTime(scope.row.window7d.resets_at) }) : t('accountQuota.noResetTime') }}
                  </div>
                </template>
                <span v-else class="text-slate-400">—</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.credits')" min-width="120">
            <template #default="scope">
              <div :data-testid="`quota-credits-${scope.row.account.id}`" class="tabular-nums">{{ scope.row.creditsLabel }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.resetCredits')" min-width="150">
            <template #default="scope">
              <div :data-testid="`quota-reset-credits-${scope.row.account.id}`">
                <div class="tabular-nums font-medium text-slate-900">{{ scope.row.resetCreditsLabel }}</div>
                <div v-if="scope.row.nearestExpiry" class="text-xs text-slate-500">
                  {{ t('accountQuota.resetCreditsNearest', { time: scope.row.nearestExpiry }) }}
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.observedAt')" min-width="200">
            <template #default="scope">
              <div :data-testid="`quota-observed-${scope.row.account.id}`">
                <div v-if="scope.row.observedAt" class="flex flex-wrap items-center gap-1 text-xs text-slate-700">
                  <el-tag v-if="!scope.row.hasObservation" size="small" type="info">{{ t('accountQuota.snapshotTag') }}</el-tag>
                  <span class="tabular-nums">{{ scope.row.observedAt }}</span>
                </div>
                <span v-else class="text-slate-400">—</span>
                <div v-if="scope.row.planType" class="text-xs text-slate-500">{{ t('accountQuota.planType', { plan: scope.row.planType }) }}</div>
                <div
                  v-if="!scope.row.hasObservation"
                  class="text-xs text-amber-600"
                  :data-testid="`quota-no-observation-${scope.row.account.id}`"
                >{{ t('accountQuota.noObservationHint') }}</div>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('accountQuota.lastResult')" min-width="200">
            <template #default="scope">
              <div :data-testid="`quota-result-${scope.row.account.id}`">
                <template v-if="scope.row.result">
                  <el-tag v-if="scope.row.result.status === 'succeeded'" type="success">{{ t('accountQuota.resultSucceeded') }}</el-tag>
                  <template v-else-if="scope.row.result.status === 'failed'">
                    <el-tag type="danger">{{ t('accountQuota.resultFailed') }}</el-tag>
                    <div class="mt-1 break-words text-xs text-red-600" :data-testid="`quota-result-message-${scope.row.account.id}`">{{ scope.row.result.message || '—' }}</div>
                    <div v-if="scope.row.resultCodeText" class="break-all font-mono text-xs text-slate-500" :data-testid="`quota-result-code-${scope.row.account.id}`">{{ scope.row.resultCodeText }}</div>
                  </template>
                  <template v-else>
                    <el-tag type="info">{{ scope.row.account.auto_reset_credit_enabled ? t('accountQuota.resultSkippedAutoReset') : t('accountQuota.resultSkipped') }}</el-tag>
                    <div v-if="scope.row.result.message" class="mt-1 break-words text-xs text-slate-500">{{ scope.row.result.message }}</div>
                    <div v-if="scope.row.resultCodeText" class="break-all font-mono text-xs text-slate-500">{{ scope.row.resultCodeText }}</div>
                  </template>
                </template>
                <span v-else class="text-slate-400">—</span>
              </div>
            </template>
          </el-table-column>
          <template #empty>{{ t('accountQuota.noAccounts') }}</template>
        </el-table>
      </section>
    </template>

    <el-dialog
      v-model="confirmOpen"
      data-testid="quota-confirm-dialog"
      :title="t('accountQuota.confirmTitle')"
      append-to-body
      align-center
      width="min(100%, 34rem)"
      :close-on-click-modal="!starting"
      :close-on-press-escape="!starting"
    >
      <p class="text-sm text-slate-700" data-testid="quota-confirm-scope">{{ confirmScopeLabel }}</p>
      <el-alert class="mt-3" type="warning" :closable="false" show-icon :title="confirmCallsNote" />
      <el-alert
        v-if="confirmAutoResetItems.length > 0"
        class="mt-3"
        type="info"
        :closable="false"
        data-testid="quota-confirm-auto-reset-note"
        :title="t('accountQuota.confirmAutoResetNote', { count: confirmAutoResetItems.length })"
      />
      <dl class="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt class="text-slate-500">{{ t('accountQuota.confirmTargetCount') }}</dt>
        <dd class="font-medium text-slate-900" data-testid="quota-confirm-count">{{ confirmTargetCount }}</dd>
      </dl>
      <el-checkbox
        v-if="confirmAutoResetItems.length > 0"
        v-model="includeAutoReset"
        data-testid="quota-include-auto-reset"
        class="mt-3"
      >{{ t('accountQuota.confirmIncludeAutoReset') }}</el-checkbox>
      <p v-if="confirmTargetCount === 0" class="mt-2 text-xs text-amber-600" data-testid="quota-confirm-empty">
        {{ t('accountQuota.confirmNoTargets') }}
      </p>
      <template #footer>
        <el-button :disabled="starting" @click="confirmOpen = false">{{ t('accountQuota.cancel') }}</el-button>
        <el-button
          data-testid="quota-confirm-start"
          type="primary"
          :loading="starting"
          :disabled="confirmTargetCount === 0"
          @click="confirmStart"
        >{{ t('accountQuota.confirmStart') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>
