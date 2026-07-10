<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { UserUsageGroupQuotaState } from '@/types'

const props = defineProps<{
  quotas?: UserUsageGroupQuotaState | null
  loading?: boolean
  rangeLabel?: string
  showResetRequest?: boolean
}>()

defineEmits<{
  requestReset: []
}>()

const { t } = useI18n()

const shouldHide = computed(() => {
  if (props.loading) {
    return (props.quotas?.groups?.length ?? 0) === 0
  }
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
</script>

<template>
  <section v-if="!shouldHide" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm" data-testid="usage-group-quotas">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900">{{ quotaTitle(props.rangeLabel) }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('usageDashboard.groupQuotasHelp') }}</p>
      </div>
      <button
        v-if="props.showResetRequest"
        type="button"
        class="inline-flex shrink-0 rounded-md border border-cyan-700 px-3 py-2 text-sm font-medium text-cyan-700 hover:bg-cyan-50"
        data-testid="open-quota-reset-request"
        @click="$emit('requestReset')"
      >
        {{ t('quotaReset.requestReset') }}
      </button>
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

    <div v-else-if="props.quotas?.status === 'unavailable'" class="mt-4 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
      {{ props.quotas?.message || t('usageDashboard.groupQuotasUnavailable') }}
    </div>

    <div v-else class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2 2xl:grid-cols-3">
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
      </article>
    </div>
  </section>
</template>
