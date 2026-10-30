<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Monitor, Refresh } from '@element-plus/icons-vue'
import AppLayout from '@/components/AppLayout.vue'
import OpsMonitoringGroupDetail from '@/components/admin/OpsMonitoringGroupDetail.vue'
import { useI18n } from '@/i18n'
import { opsMonitoringMessages } from '@/locales/opsMonitoring'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { formatBeijingClock, formatBeijingDateTime } from '@/utils/opsMonitoring'
import { useWideContentLayout } from '@/composables/useMediaQuery'
import { listRelayProviders } from '@/api/relayProvider'
import {
  getOpsMonitoringGroupAccounts,
  getOpsMonitoringGroups,
  setOpsMonitoringAccountConcurrency,
  testOpsMonitoringProxy,
  type OpsMonitoringAccount,
  type OpsMonitoringGroup,
  type OpsMonitoringGroupsData,
  type OpsMonitoringProxyTestResult,
  type OpsMonitoringWindow,
} from '@/api/opsMonitoring'
import type { RelayProvider } from '@/types'

interface OpsAccountsState {
  loading: boolean
  loaded: boolean
  error: string
  asOf: string
  accounts: OpsMonitoringAccount[]
}

interface OpsProxyTestState {
  loading: boolean
  error: string
  result: OpsMonitoringProxyTestResult | null
}

interface OpsConcurrencyState {
  saving: boolean
  error: string
  saved: boolean
}

type OpsNumericKey =
  | 'request_count_total'
  | 'sla'
  | 'error_rate'
  | 'upstream_error_rate'
  | 'upstream_error_count_excl_429_529'
  | 'upstream_429_count'
  | 'upstream_529_count'
  | 'health_score'

interface OpsMetricColumn {
  key: OpsNumericKey | 'active_account_count'
  label: string
}

type OpsSortKey = OpsMetricColumn['key'] | 'group_name' | 'platform'

const windows: OpsMonitoringWindow[] = ['1h', '6h', '24h']
const rateKeys = new Set<string>(['sla', 'error_rate', 'upstream_error_rate'])
const AUTO_REFRESH_MS = 60_000

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'opsMonitoring.', opsMonitoringMessages)
const wideContentLayout = useWideContentLayout()

const providers = ref<RelayProvider[]>([])
const providerID = ref(0)
const window = ref<OpsMonitoringWindow>('1h')
const platform = ref('')
const data = ref<OpsMonitoringGroupsData | null>(null)
const loading = ref(false)
const error = ref('')
const autoRefresh = ref(false)
const expandedGroupIDs = ref<string[]>([])
const accountStates = reactive<Record<number, OpsAccountsState>>({})
const proxyTests = reactive<Record<string, OpsProxyTestState>>({})
const concurrencyStates = reactive<Record<number, OpsConcurrencyState>>({})
const sortKey = ref<OpsSortKey>('upstream_error_rate')
const sortOrder = ref<'ascending' | 'descending'>('descending')
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

const groups = computed(() => data.value?.groups ?? [])
const failedGroupCount = computed(() => data.value?.failed_group_count ?? 0)
// A failed read keeps the previous snapshot on screen: the whole-page error
// state is reserved for a page that has no data at all.
const hasSnapshot = computed(() => data.value !== null)
const platforms = computed(() => Array.from(new Set(groups.value.map((group) => group.platform).filter(Boolean))).sort())
const filteredGroups = computed(() => (
  platform.value ? groups.value.filter((group) => group.platform === platform.value) : groups.value
))
// Failed fetches stay pinned to the top in directory order, whatever the sort.
const sortedGroups = computed(() => {
  const failed = filteredGroups.value.filter((group) => group.status === 'failed')
  const healthy = filteredGroups.value.filter((group) => group.status !== 'failed').sort(compareGroups)
  return [...failed, ...healthy]
})
const asOfText = computed(() => formatBeijingDateTime(data.value?.as_of))
const windowRangeText = computed(() => {
  const start = formatBeijingClock(data.value?.start_time)
  const end = formatBeijingClock(data.value?.end_time)
  if (!start || !end) return ''
  return t('opsMonitoring.windowRange', {
    window: t(windowDurationKey(window.value)),
    start,
    end,
  })
})
const metricColumns = computed<OpsMetricColumn[]>(() => [
  { key: 'request_count_total', label: t('opsMonitoring.requests') },
  { key: 'sla', label: t('opsMonitoring.sla') },
  { key: 'error_rate', label: t('opsMonitoring.errorRate') },
  { key: 'upstream_error_rate', label: t('opsMonitoring.upstreamErrorRate') },
  { key: 'upstream_error_count_excl_429_529', label: t('opsMonitoring.upstreamErrorCount') },
  { key: 'upstream_429_count', label: '429' },
  { key: 'upstream_529_count', label: '529' },
  { key: 'health_score', label: t('opsMonitoring.healthScore') },
  { key: 'active_account_count', label: t('opsMonitoring.activeAccounts') },
])

function windowDurationKey(value: OpsMonitoringWindow) {
  if (value === '6h') return 'opsMonitoring.windowDuration6h' as const
  if (value === '24h') return 'opsMonitoring.windowDuration24h' as const
  return 'opsMonitoring.windowDuration1h' as const
}

function finiteNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

/**
 * Presentation-only classification of an upstream payload that carries no
 * traffic. It never derives a metric; every displayed number stays verbatim.
 */
function isNoTraffic(group: OpsMonitoringGroup): boolean {
  const overview = group.overview
  if (group.status === 'failed' || !overview) return false
  return [overview.request_count_total, overview.request_count_sla, overview.success_count, overview.error_count_total, overview.error_count_sla, overview.token_consumed]
    .every((value) => finiteNumber(value) === null || finiteNumber(value) === 0)
}

// The traffic-derived table cells where a returned zero is meaningless and is
// therefore shown as 无数据. The other five traffic fields (request_count_sla,
// success_count, error_count_total, error_count_sla, token_consumed) appear only
// in the group detail panel, which prints every returned value verbatim.
const noTrafficDisplayKeys = new Set<string>([
  'request_count_total',
  'sla',
  'error_rate',
  'upstream_error_rate',
])

function cellFor(group: OpsMonitoringGroup, key: OpsMetricColumn['key']): { text: string; muted: boolean } {
  if (group.status === 'failed') return { text: '—', muted: true }
  if (key === 'active_account_count') {
    return { text: `${group.active_account_count} / ${group.account_count}`, muted: false }
  }
  const value = finiteNumber(group.overview?.[key])
  if (value === null) return { text: t('opsMonitoring.noData'), muted: true }
  // Only a zero in a traffic-derived cell of a zero-traffic group is muted;
  // every non-zero upstream value is always displayed.
  if (value === 0 && noTrafficDisplayKeys.has(key) && isNoTraffic(group)) {
    return { text: t('opsMonitoring.noData'), muted: true }
  }
  // Rate fields arrive as fractions; only the display format changes here.
  return { text: rateKeys.has(key) ? `${(value * 100).toFixed(2)}%` : String(value), muted: false }
}

function sortValue(group: OpsMonitoringGroup, key: OpsMetricColumn['key']): number | null {
  if (key === 'active_account_count') return finiteNumber(group.active_account_count)
  return finiteNumber(group.overview?.[key])
}

function compareGroups(left: OpsMonitoringGroup, right: OpsMonitoringGroup): number {
  const direction = sortOrder.value === 'ascending' ? 1 : -1
  if (sortKey.value === 'group_name' || sortKey.value === 'platform') {
    const leftText = left[sortKey.value] || ''
    const rightText = right[sortKey.value] || ''
    if (leftText !== rightText) return leftText.localeCompare(rightText) * direction
    return left.group_id - right.group_id
  }
  const leftValue = sortValue(left, sortKey.value)
  const rightValue = sortValue(right, sortKey.value)
  if (leftValue === null && rightValue === null) return left.group_id - right.group_id
  if (leftValue === null) return 1
  if (rightValue === null) return -1
  if (leftValue !== rightValue) return (leftValue - rightValue) * direction
  return left.group_id - right.group_id
}

function handleSortChange(payload: { prop: string | null; order: string | null }) {
  if (!payload.prop || !payload.order) {
    sortKey.value = 'upstream_error_rate'
    sortOrder.value = 'descending'
    return
  }
  sortKey.value = payload.prop as OpsSortKey
  sortOrder.value = payload.order === 'ascending' ? 'ascending' : 'descending'
}

function accountState(groupID: number): OpsAccountsState {
  return accountStates[groupID] ?? { loading: false, loaded: false, error: '', asOf: '', accounts: [] }
}

function asGroup(row: unknown): OpsMonitoringGroup {
  return row as OpsMonitoringGroup
}

function isExpanded(groupID: number): boolean {
  return expandedGroupIDs.value.includes(String(groupID))
}

function resetDetails() {
  expandedGroupIDs.value = []
  for (const key of Object.keys(accountStates)) delete accountStates[Number(key)]
  for (const key of Object.keys(proxyTests)) delete proxyTests[key]
  for (const key of Object.keys(concurrencyStates)) delete concurrencyStates[Number(key)]
}

function applyGroups(next: OpsMonitoringGroupsData | null) {
  data.value = next
  const activeKeys = new Set((next?.groups ?? []).map((group) => String(group.group_id)))
  for (const key of Object.keys(accountStates)) {
    if (!activeKeys.has(key)) delete accountStates[Number(key)]
  }
  for (const group of next?.groups ?? []) {
    accountStates[group.group_id] ??= { loading: false, loaded: false, error: '', asOf: '', accounts: [] }
  }
  if (platform.value && !(next?.groups ?? []).some((group) => group.platform === platform.value)) {
    platform.value = ''
  }
  expandedGroupIDs.value = expandedGroupIDs.value.filter((key) => activeKeys.has(key))
}

async function loadProviders() {
  const response = await listRelayProviders()
  const items = response.data.data ?? []
  providers.value = items
  const preferred = items.find((item) => item.is_primary) ?? items[0]
  providerID.value = preferred?.id ?? 0
}

async function loadGroups(force = false) {
  if (!providerID.value) return
  loading.value = true
  error.value = ''
  try {
    const response = await getOpsMonitoringGroups({
      provider_id: providerID.value,
      window: window.value,
      // `force` skips the 60-second snapshot cache read: only an explicit
      // refresh asks upstream now. Automatic ticks keep reading the cache.
      force,
      // The platform filter is applied client-side so its options can be
      // derived from every loaded group.
    })
    applyGroups(response.data.data ?? null)
  } catch (err: any) {
    // No automatic retry: surface the failure and let the operator refresh.
    error.value = err.response?.data?.message || err.message || t('opsMonitoring.loadFailed')
  } finally {
    loading.value = false
  }
}

function selectProvider(value: unknown) {
  const next = Number(value)
  if (!Number.isFinite(next) || next === providerID.value) return
  providerID.value = next
  resetDetails()
  void loadGroups()
}

function selectWindow(value: unknown) {
  if (value !== '1h' && value !== '6h' && value !== '24h') return
  if (value === window.value) return
  window.value = value
  resetDetails()
  void loadGroups()
}

async function ensureAccounts(groupID: number) {
  const state = accountStates[groupID] ??= { loading: false, loaded: false, error: '', asOf: '', accounts: [] }
  if (state.loading || state.loaded) return
  state.loading = true
  state.error = ''
  try {
    const response = await getOpsMonitoringGroupAccounts(groupID, { provider_id: providerID.value })
    const payload = response.data.data
    state.accounts = payload?.accounts ?? []
    state.asOf = formatBeijingDateTime(payload?.as_of)
    state.loaded = true
    for (const account of state.accounts) {
      if (account.proxy) proxyTests[String(account.proxy.proxy_id)] ??= { loading: false, error: '', result: null }
    }
  } catch (err: any) {
    state.error = err.response?.data?.message || err.message || t('opsMonitoring.accountsFailed')
  } finally {
    state.loading = false
  }
}

function toggleGroup(groupID: number) {
  const key = String(groupID)
  if (isExpanded(groupID)) {
    expandedGroupIDs.value = expandedGroupIDs.value.filter((id) => id !== key)
    return
  }
  expandedGroupIDs.value = [...expandedGroupIDs.value, key]
  void ensureAccounts(groupID)
}

// `expand-change` is emitted as an array for expand columns and as a boolean for
// tree/lazy rows; both shapes keep the expanded-key list authoritative.
function handleExpandChange(row: OpsMonitoringGroup, expanded: OpsMonitoringGroup[] | boolean) {
  if (Array.isArray(expanded)) {
    const nextKeys = expanded.map((item) => String(item.group_id))
    for (const key of nextKeys) {
      if (!expandedGroupIDs.value.includes(key)) void ensureAccounts(Number(key))
    }
    expandedGroupIDs.value = nextKeys
    return
  }
  const key = String(asGroup(row).group_id)
  if (expanded) {
    if (!expandedGroupIDs.value.includes(key)) {
      expandedGroupIDs.value = [...expandedGroupIDs.value, key]
      void ensureAccounts(Number(key))
    }
    return
  }
  expandedGroupIDs.value = expandedGroupIDs.value.filter((id) => id !== key)
}

async function refreshProxy(proxyID: number) {
  const state = proxyTests[String(proxyID)] ??= { loading: false, error: '', result: null }
  if (state.loading) return
  state.loading = true
  state.error = ''
  state.result = null
  try {
    const response = await testOpsMonitoringProxy(proxyID, { provider_id: providerID.value, force: true })
    state.result = response.data.data ?? null
  } catch (err: any) {
    state.error = err.response?.data?.message || err.message || t('opsMonitoring.proxyTestFailed')
  } finally {
    state.loading = false
  }
}

async function setAccountConcurrency(groupID: number, accountID: number, concurrency: number) {
  if (!concurrencyStates[accountID]) {
    concurrencyStates[accountID] = { saving: false, error: '', saved: false }
  }
  // Re-read through the reactive map: the `??=` shorthand yields the raw object
  // on its first assignment, which would bypass reactivity for every update.
  const state = concurrencyStates[accountID]
  if (state.saving) return
  state.saving = true
  state.error = ''
  state.saved = false
  try {
    const response = await setOpsMonitoringAccountConcurrency(groupID, accountID, concurrency, { provider_id: providerID.value })
    const capacity = response.data.data
    const account = accountStates[groupID]?.accounts.find((item) => item.account_id === accountID)
    if (account) {
      account.concurrency = capacity?.concurrency ?? concurrency
      account.current_concurrency = capacity?.current_concurrency ?? account.current_concurrency
    }
    state.saved = true
  } catch (err: any) {
    // No automatic retry: keep the previous capacity and report the failure.
    state.error = err.response?.data?.message || err.message || t('opsMonitoring.concurrencyUpdateFailed')
  } finally {
    state.saving = false
  }
}

function stopAutoRefresh() {
  if (autoRefreshTimer !== null) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

watch(autoRefresh, (enabled) => {
  stopAutoRefresh()
  // Automatic ticks read the cache; only the manual refresh forces upstream.
  if (enabled) autoRefreshTimer = setInterval(() => void loadGroups(false), AUTO_REFRESH_MS)
})

onMounted(async () => {
  try {
    await loadProviders()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('opsMonitoring.loadFailed')
  }
  await loadGroups()
})

onBeforeUnmount(stopAutoRefresh)
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-5">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div class="min-w-0">
          <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-blue-700">
            <el-icon><Monitor /></el-icon>
            {{ t('opsMonitoring.eyebrow') }}
          </div>
          <h1 class="mt-1 text-2xl font-semibold text-slate-900">{{ t('opsMonitoring.title') }}</h1>
          <p class="mt-1 text-sm text-slate-500">{{ t('opsMonitoring.subtitle') }}</p>
          <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500">
            <span data-testid="ops-as-of">{{ t('opsMonitoring.asOf', { time: asOfText }) }}</span>
            <span v-if="windowRangeText" data-testid="ops-window-range">{{ windowRangeText }}</span>
            <el-tag v-if="data?.refreshing" size="small" type="info">{{ t('opsMonitoring.refreshing') }}</el-tag>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <el-select
            v-if="providers.length > 1"
            :model-value="providerID"
            data-testid="ops-provider-select"
            class="w-56"
            :placeholder="t('opsMonitoring.selectProvider')"
            @change="selectProvider"
          >
            <el-option v-for="item in providers" :key="item.id" :label="item.display_name || item.name" :value="item.id" />
          </el-select>
          <el-radio-group :model-value="window" data-testid="ops-window-switcher" @change="selectWindow">
            <el-radio-button v-for="item in windows" :key="item" :value="item">{{ item }}</el-radio-button>
          </el-radio-group>
          <el-select :model-value="platform" data-testid="ops-platform-filter" class="w-36" @change="platform = String($event ?? '')">
            <el-option :label="t('opsMonitoring.allPlatforms')" value="" />
            <el-option v-for="item in platforms" :key="item" :label="item" :value="item" />
          </el-select>
          <el-switch
            :model-value="autoRefresh"
            data-testid="ops-auto-refresh"
            :active-text="t('opsMonitoring.autoRefresh')"
            @change="autoRefresh = $event === true"
          />
          <el-button data-testid="ops-refresh" type="primary" :icon="Refresh" :loading="loading" @click="loadGroups(true)">
            {{ t('opsMonitoring.refresh') }}
          </el-button>
        </div>
      </header>

      <el-alert
        v-if="failedGroupCount > 0"
        data-testid="ops-failed-banner"
        type="warning"
        :closable="false"
        :title="t('opsMonitoring.failedGroups', { count: failedGroupCount })"
      />

      <el-alert
        v-if="error && hasSnapshot"
        data-testid="ops-inline-error"
        type="error"
        :closable="false"
        :title="error"
      />

      <section v-if="error && !hasSnapshot" data-testid="ops-error-state" class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <el-alert type="error" :closable="false" :title="error" />
        <p class="mt-3 text-sm text-slate-500">{{ t('opsMonitoring.errorHelp') }}</p>
      </section>

      <section v-else class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
        <el-empty v-if="!sortedGroups.length" data-testid="ops-empty" :description="t('opsMonitoring.noGroups')" />
        <div v-else-if="wideContentLayout" data-testid="ops-group-table-layout">
          <el-table
            :data="sortedGroups"
            row-key="group_id"
            :expand-row-keys="expandedGroupIDs"
            :default-sort="{ prop: sortKey, order: sortOrder }"
            stripe
            @sort-change="handleSortChange"
            @expand-change="handleExpandChange"
          >
            <el-table-column type="expand">
              <template #default="scope">
                <OpsMonitoringGroupDetail
                  :group="asGroup(scope.row)"
                  :accounts="accountState(scope.row.group_id).accounts"
                  :accounts-loading="accountState(scope.row.group_id).loading"
                  :accounts-error="accountState(scope.row.group_id).error"
                  :accounts-as-of="accountState(scope.row.group_id).asOf"
                  :proxy-tests="proxyTests"
                  :concurrency-states="concurrencyStates"
                  @test-proxy="refreshProxy"
                  @set-concurrency="(accountID, concurrency) => setAccountConcurrency(asGroup(scope.row).group_id, accountID, concurrency)"
                />
              </template>
            </el-table-column>
            <el-table-column :label="t('opsMonitoring.group')" prop="group_name" min-width="200" sortable="custom">
              <template #default="scope">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="break-words font-medium text-slate-900" :data-testid="`ops-group-name-${scope.row.group_id}`">{{ scope.row.group_name }}</span>
                  <el-tag v-if="scope.row.status === 'failed'" size="small" type="danger" :data-testid="`ops-group-failed-${scope.row.group_id}`">{{ t('opsMonitoring.fetchFailed') }}</el-tag>
                  <el-tag v-else-if="isNoTraffic(asGroup(scope.row))" size="small" type="info" :data-testid="`ops-group-nodata-${scope.row.group_id}`">{{ t('opsMonitoring.noData') }}</el-tag>
                </div>
                <div class="font-mono text-[11px] text-slate-400">#{{ scope.row.group_id }}</div>
                <div v-if="scope.row.status === 'failed' && scope.row.error" class="mt-1 break-words text-xs text-red-600" :data-testid="`ops-group-error-${scope.row.group_id}`">
                  {{ scope.row.error }}
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('opsMonitoring.platform')" prop="platform" min-width="110" sortable="custom" />
            <el-table-column
              v-for="column in metricColumns"
              :key="column.key"
              :label="column.label"
              :prop="column.key"
              min-width="120"
              sortable="custom"
            >
              <template #default="scope">
                <span :data-testid="`ops-cell-${column.key}-${scope.row.group_id}`" :class="cellFor(asGroup(scope.row), column.key).muted ? 'text-slate-400' : 'tabular-nums'">
                  {{ cellFor(asGroup(scope.row), column.key).text }}
                </span>
              </template>
            </el-table-column>
            <el-table-column width="110">
              <template #default="scope">
                <el-button link type="primary" :data-testid="`ops-group-toggle-${scope.row.group_id}`" @click="toggleGroup(scope.row.group_id)">
                  {{ isExpanded(scope.row.group_id) ? t('opsMonitoring.collapse') : t('opsMonitoring.expand') }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div v-else data-testid="ops-group-card-layout" class="space-y-3">
          <article v-for="group in sortedGroups" :key="group.group_id" :data-testid="`ops-group-card-${group.group_id}`" class="rounded-md border border-slate-200 p-3">
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="break-words font-medium text-slate-900" :data-testid="`ops-group-name-${group.group_id}`">{{ group.group_name }}</span>
                  <el-tag v-if="group.status === 'failed'" size="small" type="danger" :data-testid="`ops-group-failed-${group.group_id}`">{{ t('opsMonitoring.fetchFailed') }}</el-tag>
                  <el-tag v-else-if="isNoTraffic(group)" size="small" type="info" :data-testid="`ops-group-nodata-${group.group_id}`">{{ t('opsMonitoring.noData') }}</el-tag>
                </div>
                <div class="text-xs text-slate-500">#{{ group.group_id }} · {{ group.platform }}</div>
                <div v-if="group.status === 'failed' && group.error" class="mt-1 break-words text-xs text-red-600" :data-testid="`ops-group-error-${group.group_id}`">
                  {{ group.error }}
                </div>
              </div>
              <el-button link type="primary" :data-testid="`ops-group-toggle-${group.group_id}`" @click="toggleGroup(group.group_id)">
                {{ isExpanded(group.group_id) ? t('opsMonitoring.collapse') : t('opsMonitoring.expand') }}
              </el-button>
            </div>
            <dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2">
              <div v-for="column in metricColumns" :key="column.key">
                <dt class="text-xs text-slate-500">{{ column.label }}</dt>
                <dd :data-testid="`ops-cell-${column.key}-${group.group_id}`" :class="cellFor(group, column.key).muted ? 'text-sm text-slate-400' : 'text-sm tabular-nums text-slate-900'">
                  {{ cellFor(group, column.key).text }}
                </dd>
              </div>
            </dl>
            <OpsMonitoringGroupDetail
              v-if="isExpanded(group.group_id)"
              class="mt-3"
              :group="group"
              :accounts="accountState(group.group_id).accounts"
              :accounts-loading="accountState(group.group_id).loading"
              :accounts-error="accountState(group.group_id).error"
              :accounts-as-of="accountState(group.group_id).asOf"
              :proxy-tests="proxyTests"
              :concurrency-states="concurrencyStates"
              @test-proxy="refreshProxy"
              @set-concurrency="(accountID, concurrency) => setAccountConcurrency(group.group_id, accountID, concurrency)"
            />
          </article>
        </div>
      </section>
    </div>
  </AppLayout>
</template>
