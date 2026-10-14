<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { opsMonitoringMessages } from '@/locales/opsMonitoring'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { formatBeijingDateTime } from '@/utils/opsMonitoring'
import type {
  OpsMonitoringAccount,
  OpsMonitoringDurationStat,
  OpsMonitoringGroup,
  OpsMonitoringOutputTpsStat,
  OpsMonitoringProxyTestResult,
  OpsMonitoringWindowStat,
} from '@/api/opsMonitoring'

const props = defineProps<{
  group: OpsMonitoringGroup
  accounts: OpsMonitoringAccount[]
  accountsLoading: boolean
  accountsError: string
  accountsAsOf: string
  proxyTests: Record<string, { loading: boolean; error: string; result: OpsMonitoringProxyTestResult | null }>
}>()

const emit = defineEmits<{ testProxy: [proxyID: number] }>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'opsMonitoring.', opsMonitoringMessages)

const noValue = computed(() => t('opsMonitoring.noData'))

function numericText(value: unknown): string | null {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : null
}

function statText(value: OpsMonitoringWindowStat | null | undefined): string | null {
  if (!value) return null
  const parts: string[] = []
  const avg = numericText(value.avg)
  const current = numericText(value.current)
  const peak = numericText(value.peak)
  if (avg !== null) parts.push(`${t('opsMonitoring.avgLabel')} ${avg}`)
  if (current !== null) parts.push(`${t('opsMonitoring.currentLabel')} ${current}`)
  if (peak !== null) parts.push(`${t('opsMonitoring.peakLabel')} ${peak}`)
  return parts.length ? parts.join(' · ') : null
}

function outputTpsText(value: OpsMonitoringOutputTpsStat | null | undefined): string | null {
  if (!value) return null
  const parts: string[] = []
  const avg = numericText(value.avg)
  const p50 = numericText(value.p50)
  const p10 = numericText(value.p10)
  const p5 = numericText(value.p5)
  const samples = numericText(value.sample_count)
  if (avg !== null) parts.push(`${t('opsMonitoring.avgLabel')} ${avg}`)
  if (p50 !== null) parts.push(`p50 ${p50}`)
  if (p10 !== null) parts.push(`p10 ${p10}`)
  if (p5 !== null) parts.push(`p5 ${p5}`)
  if (samples !== null) parts.push(`${t('opsMonitoring.samplesLabel')} ${samples}`)
  return parts.length ? parts.join(' · ') : null
}

function durationText(value: OpsMonitoringDurationStat | null | undefined): string | null {
  if (!value) return null
  const parts: string[] = []
  const avg = numericText(value.avg_ms)
  const max = numericText(value.max_ms)
  if (avg !== null) parts.push(`${t('opsMonitoring.avgLabel')} ${avg} ms`)
  if (max !== null) parts.push(`${t('opsMonitoring.maxLabel')} ${max} ms`)
  for (const [label, raw] of [['p50', value.p50_ms], ['p90', value.p90_ms], ['p95', value.p95_ms], ['p99', value.p99_ms]] as const) {
    const text = numericText(raw)
    if (text !== null) parts.push(`${label} ${text} ms`)
  }
  return parts.length ? parts.join(' · ') : null
}

function locationText(result: OpsMonitoringProxyTestResult): string {
  return [result.city, result.region, result.country].filter(Boolean).join(' · ')
}

const scalarRows = computed(() => {
  const overview = props.group.overview
  return [
    { key: 'token_consumed', label: t('opsMonitoring.tokenConsumed'), value: numericText(overview?.token_consumed) },
    { key: 'success_count', label: t('opsMonitoring.successCount'), value: numericText(overview?.success_count) },
    { key: 'error_count_total', label: t('opsMonitoring.errorCountTotal'), value: numericText(overview?.error_count_total) },
    { key: 'error_count_sla', label: t('opsMonitoring.errorCountSla'), value: numericText(overview?.error_count_sla) },
    { key: 'business_limited_count', label: t('opsMonitoring.businessLimitedCount'), value: numericText(overview?.business_limited_count) },
    { key: 'request_count_sla', label: t('opsMonitoring.requestCountSla'), value: numericText(overview?.request_count_sla) },
  ]
})

const statRows = computed(() => {
  const overview = props.group.overview
  return [
    { key: 'qps', label: t('opsMonitoring.qps'), value: statText(overview?.qps) },
    { key: 'tps', label: t('opsMonitoring.tps'), value: statText(overview?.tps) },
    { key: 'output_tps', label: t('opsMonitoring.outputTps'), value: outputTpsText(overview?.output_tps) },
  ]
})

const durationRows = computed(() => {
  const overview = props.group.overview
  return [
    { key: 'duration', label: t('opsMonitoring.duration'), value: durationText(overview?.duration) },
    { key: 'ttft', label: t('opsMonitoring.ttft'), value: durationText(overview?.ttft) },
  ]
})

const accountRows = computed(() => props.accounts.map((account) => ({
  account,
  proxy: account.proxy,
  state: account.proxy
    ? props.proxyTests[String(account.proxy.proxy_id)] ?? { loading: false, error: '', result: null }
    : null,
})))
</script>

<template>
  <div class="grid gap-5 rounded-md bg-slate-50 p-4 lg:grid-cols-2">
    <section data-testid="ops-group-metrics">
      <div class="text-xs font-semibold uppercase tracking-wide text-slate-500">
        {{ t('opsMonitoring.detailsTitle') }}
      </div>
      <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-2">
        <div v-for="row in scalarRows" :key="row.key">
          <dt class="text-xs text-slate-500">{{ row.label }}</dt>
          <dd :data-testid="`ops-metric-${row.key}`" :class="row.value === null ? 'text-sm text-slate-400' : 'text-sm tabular-nums text-slate-900'">
            {{ row.value ?? noValue }}
          </dd>
        </div>
      </dl>
      <dl class="mt-3 space-y-2">
        <div v-for="row in statRows" :key="row.key">
          <dt class="text-xs text-slate-500">{{ row.label }}</dt>
          <dd :data-testid="`ops-metric-${row.key}`" :class="row.value === null ? 'text-sm text-slate-400' : 'text-sm tabular-nums text-slate-900'">
            {{ row.value ?? noValue }}
          </dd>
        </div>
        <div v-for="row in durationRows" :key="row.key">
          <dt class="text-xs text-slate-500">{{ row.label }}</dt>
          <dd :data-testid="`ops-metric-${row.key}`" :class="row.value === null ? 'text-sm text-slate-400' : 'text-sm tabular-nums text-slate-900'">
            {{ row.value ?? noValue }}
          </dd>
        </div>
      </dl>
    </section>

    <section data-testid="ops-group-accounts">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <div class="text-xs font-semibold uppercase tracking-wide text-slate-500">
          {{ t('opsMonitoring.accountsTitle') }}
        </div>
        <span v-if="accountsAsOf" class="text-xs text-slate-500">{{ t('opsMonitoring.asOf', { time: accountsAsOf }) }}</span>
      </div>
      <div v-if="accountsLoading" class="mt-2 text-sm text-slate-500">{{ t('opsMonitoring.accountsLoading') }}</div>
      <el-alert v-else-if="accountsError" class="mt-2" type="error" :closable="false" :title="accountsError" />
      <div v-else-if="!accountRows.length" class="mt-2 text-sm text-slate-500">{{ t('opsMonitoring.noAccounts') }}</div>
      <ul v-else class="mt-2 divide-y divide-slate-200">
        <li
          v-for="row in accountRows"
          :key="row.account.account_id"
          :data-testid="`ops-account-${group.group_id}-${row.account.account_id}`"
          class="py-2 first:pt-0"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-medium text-slate-900">{{ row.account.name }}</span>
            <span class="text-xs text-slate-500">#{{ row.account.account_id }}</span>
            <el-tag size="small" type="info">{{ row.account.type }}</el-tag>
            <el-tag size="small" :type="row.account.status === 'active' ? 'success' : 'warning'">{{ row.account.status }}</el-tag>
            <el-tag size="small" :type="row.account.schedulable ? 'success' : 'info'">
              {{ row.account.schedulable ? t('opsMonitoring.schedulable') : t('opsMonitoring.notSchedulable') }}
            </el-tag>
          </div>
          <div v-if="row.proxy" class="mt-1 flex flex-wrap items-center gap-2">
            <span class="text-xs text-slate-500" :data-testid="`ops-account-proxy-${group.group_id}-${row.account.account_id}`">
              {{ row.proxy.name }} · {{ row.proxy.protocol }} · {{ row.proxy.host }}:{{ row.proxy.port }}
            </span>
            <el-button
              size="small"
              :loading="row.state?.loading"
              :data-testid="`ops-proxy-test-${group.group_id}-${row.account.account_id}`"
              @click="emit('testProxy', row.proxy.proxy_id)"
            >
              {{ t('opsMonitoring.refreshEgressIp') }}
            </el-button>
          </div>
          <div v-else class="mt-1 text-xs text-slate-500" :data-testid="`ops-account-direct-${group.group_id}-${row.account.account_id}`">
            {{ t('opsMonitoring.direct') }}
          </div>
          <div
            v-if="row.proxy && row.state && (row.state.result || row.state.error)"
            class="mt-1 text-xs"
            :data-testid="`ops-proxy-result-${group.group_id}-${row.account.account_id}`"
          >
            <div v-if="row.state.result && row.state.result.success" class="text-slate-700">
              {{ t('opsMonitoring.egressIp') }}: {{ row.state.result.ip_address }}
              <span v-if="locationText(row.state.result)"> · {{ locationText(row.state.result) }}</span>
              · {{ t('opsMonitoring.latency') }}: {{ row.state.result.latency_ms }} ms
              · {{ t('opsMonitoring.checkedAt') }}: {{ formatBeijingDateTime(row.state.result.checked_at) }}
              <span v-if="row.state.result.cached"> · {{ t('opsMonitoring.cached') }}</span>
            </div>
            <div v-if="row.state.result && row.state.result.success && row.state.result.message" class="text-slate-500">
              {{ row.state.result.message }}
            </div>
            <div v-if="row.state.result && !row.state.result.success" class="text-red-600">
              {{ t('opsMonitoring.proxyTestFailed') }}: {{ row.state.result.message }}
            </div>
            <div v-if="row.state.error" class="text-red-600">
              {{ t('opsMonitoring.proxyTestFailed') }}: {{ row.state.error }}
            </div>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>
