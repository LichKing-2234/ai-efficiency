<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '@/i18n'
import type { UserUsageGroupPoolUsageState, UserUsageGroupQuotaState } from '@/types'

const props = defineProps<{
  quotas?: UserUsageGroupQuotaState | null
  poolUsage?: UserUsageGroupPoolUsageState | null
  poolLoading?: boolean
  loading?: boolean
  rangeLabel?: string
  showResetRequest?: boolean
  resetRequestLoading?: boolean
}>()

defineEmits<{
  requestReset: []
}>()

const { t } = useI18n()
const now = ref(Date.now())
let resetTimer: ReturnType<typeof setInterval> | null = null

const shouldHide = computed(() => {
  if (props.loading) return false
  if (!props.quotas) return true
  if (props.quotas.status === 'empty') return true
  if ((props.quotas.groups?.length ?? 0) === 0 && props.quotas.status !== 'unavailable') return true
  return false
})

const skeletonCount = computed(() => {
  const count = props.quotas?.groups?.length ?? 0
  if (count > 0) return count
  return 2
})

function formatCurrency(amount: number | null | undefined, unitLabel?: string) {
  if (amount == null) return '--'
  if ((unitLabel ?? '').toUpperCase() === 'USD' || !unitLabel) return `$${amount.toFixed(2)}`
  return `${unitLabel} ${amount.toFixed(2)}`
}

function displayQuotaValue(group: NonNullable<UserUsageGroupQuotaState['groups']>[number], unitLabel?: string) {
  if (group.is_unlimited) {
    return '∞'
  }
  if (group.quota_amount == null) {
    return '-'
  }
  return formatCurrency(group.quota_amount, unitLabel)
}

function quotaValueClass(group: NonNullable<UserUsageGroupQuotaState['groups']>[number]) {
  if (group.is_unlimited) {
    return 'text-green-600'
  }
  if (group.quota_amount == null || group.quota_amount <= 0 || group.used_amount == null) {
    return 'text-gray-900'
  }
  const pct = (group.used_amount / group.quota_amount) * 100
  if (pct >= 100) return 'text-red-600'
  if (pct >= 80) return 'text-amber-500'
  return 'text-green-600'
}

function quotaTitle(rangeLabel?: string) {
  if (!rangeLabel) return t('usageDashboard.groupQuotasTitle')
  if (rangeLabel === t('usageDashboard.today')) return t('usageDashboard.dailyQuotaTitle')
  if (rangeLabel === t('usageDashboard.sevenDays')) return t('usageDashboard.weeklyQuotaTitle')
  return t('usageDashboard.monthlyQuotaTitle')
}

function resetDateLabel(value?: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function resetCountdown(value?: string | null) {
  if (!value) return ''
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return ''
  const remaining = timestamp - now.value
  if (remaining <= 0) return t('usageDashboard.resetDue')
  const minutes = Math.max(1, Math.ceil(remaining / 60_000))
  if (minutes < 60) return t('usageDashboard.resetInMinutes', { count: minutes })
  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return t('usageDashboard.resetInHours', { count: hours })
  return t('usageDashboard.resetInDays', { count: Math.ceil(hours / 24) })
}

function poolItem(groupID: string) {
  return props.poolUsage?.groups?.find((item) => item.group_id === groupID) ?? null
}

function poolUtilization(item: NonNullable<ReturnType<typeof poolItem>>) {
  return `${item.average_weekly_utilization.toFixed(1)}%`
}

function poolAsOfLabel(value?: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

onMounted(() => {
  resetTimer = setInterval(() => {
    now.value = Date.now()
  }, 60_000)
})

onBeforeUnmount(() => {
  if (resetTimer) clearInterval(resetTimer)
  resetTimer = null
})
</script>

<template>
  <section v-if="!shouldHide" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm" data-testid="usage-group-quotas">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900">{{ quotaTitle(props.rangeLabel) }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('usageDashboard.groupQuotasHelp') }}</p>
      </div>
      <ElButton
        v-if="props.showResetRequest"
        type="primary"
        plain
        class="shrink-0"
        data-testid="open-quota-reset-request"
        :loading="props.resetRequestLoading"
        :disabled="props.resetRequestLoading"
        @click="$emit('requestReset')"
      >
        {{ t('quotaReset.requestReset') }}
      </ElButton>
    </div>

    <div v-if="props.loading" class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3" data-testid="usage-group-quotas-loading">
      <article
        v-for="index in skeletonCount"
        :key="index"
        class="rounded-lg border border-gray-200 bg-gray-50 p-4"
      >
        <div class="h-3 w-16 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-3 h-7 w-32 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-4 h-3 w-20 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-3 h-9 w-40 animate-pulse rounded bg-gray-200"></div>
      </article>
    </div>

    <ElAlert
      v-else-if="props.quotas?.status === 'unavailable'"
      class="mt-4"
      type="warning"
      :closable="false"
      :title="props.quotas?.message || t('usageDashboard.groupQuotasUnavailable')"
    />

    <div
      v-else
      class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3"
      :class="(props.quotas?.groups?.length ?? 0) === 1 ? 'max-w-xl lg:grid-cols-1 2xl:grid-cols-1' : ''"
    >
      <article
        v-for="group in props.quotas?.groups ?? []"
        :key="group.group_id"
        class="rounded-lg border border-gray-200 bg-gray-50 p-4"
      >
        <p class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ group.platform }}</p>
        <h3 class="mt-2 text-lg font-semibold text-gray-900">{{ group.group_name }}</h3>
        <p class="mt-2 text-xs font-medium uppercase text-gray-500">
          {{ t('usageDashboard.usedOverQuota') }}
        </p>
        <p class="mt-2 text-2xl font-semibold" :class="quotaValueClass(group)">
          {{ formatCurrency(group.used_amount, props.quotas?.unit_label) }} /
          {{ displayQuotaValue(group, props.quotas?.unit_label) }}
        </p>
        <div
          v-if="resetDateLabel(group.reset_at)"
          class="mt-2 text-xs text-gray-500"
          :data-testid="`usage-subscription-reset-${group.group_id}`"
        >
          <span class="font-medium text-gray-600">{{ t('usageDashboard.subscriptionReset') }}</span>
          <time class="ml-1" :datetime="group.reset_at ?? undefined">{{ resetDateLabel(group.reset_at) }}</time>
          <span class="ml-1">({{ resetCountdown(group.reset_at) }})</span>
        </div>
        <div
          v-if="props.poolLoading && !poolItem(group.group_id)"
          class="mt-3 rounded-md border border-dashed border-slate-200 bg-white px-3 py-2 text-xs text-slate-500"
          :data-testid="`usage-pool-loading-${group.group_id}`"
        >
          {{ t('usageDashboard.poolUsageLoading') }}
        </div>
        <div
          v-else-if="poolItem(group.group_id)"
          class="mt-3 rounded-md border border-slate-200 bg-white px-3 py-2 text-xs text-slate-600"
          :data-testid="`usage-pool-${group.group_id}`"
        >
          <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
            <span class="font-medium text-slate-700">{{ t('usageDashboard.poolUsageTitle') }}</span>
            <span class="text-sm font-semibold text-slate-900">{{ poolUtilization(poolItem(group.group_id)!) }}</span>
          </div>
          <p class="mt-1">{{ t('usageDashboard.poolUsageCoverage', { valid: poolItem(group.group_id)!.valid_oauth_accounts, total: poolItem(group.group_id)!.total_active_oauth_accounts }) }}</p>
          <p class="mt-1 text-slate-500">{{ t('usageDashboard.poolUsageHelp') }}</p>
          <p v-if="poolItem(group.group_id)!.next_reset_at" class="mt-1">
            {{ t('usageDashboard.poolUsageReset') }}
            <time class="ml-1" :datetime="poolItem(group.group_id)!.next_reset_at ?? undefined">{{ resetDateLabel(poolItem(group.group_id)!.next_reset_at) }}</time>
            <span class="ml-1">({{ resetCountdown(poolItem(group.group_id)!.next_reset_at) }})</span>
          </p>
          <p v-if="poolItem(group.group_id)!.as_of" class="mt-1 text-slate-400">
            {{ t('usageDashboard.poolUsageAsOf', { value: poolAsOfLabel(poolItem(group.group_id)!.as_of) }) }}
          </p>
        </div>
      </article>
    </div>
  </section>
</template>
