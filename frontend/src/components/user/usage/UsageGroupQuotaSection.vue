<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { UserUsageGroupQuotaState } from '@/types'

const props = defineProps<{
  quotas?: UserUsageGroupQuotaState | null
}>()

const { t } = useI18n()

const shouldHide = computed(() => {
  if (!props.quotas) return true
  if (props.quotas.status === 'empty') return true
  if ((props.quotas.groups?.length ?? 0) === 0 && props.quotas.status !== 'unavailable') return true
  return false
})

function formatCurrency(amount: number | null | undefined, unitLabel?: string) {
  if (amount == null) return '--'
  if ((unitLabel ?? '').toUpperCase() === 'USD' || !unitLabel) return `$${amount.toFixed(2)}`
  return `${unitLabel} ${amount.toFixed(2)}`
}
</script>

<template>
  <section v-if="!shouldHide" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm" data-testid="usage-group-quotas">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.groupQuotasTitle') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('usageDashboard.groupQuotasHelp') }}</p>
      </div>
    </div>

    <div v-if="props.quotas?.status === 'unavailable'" class="mt-4 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
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
          {{ group.is_unlimited ? t('usageDashboard.usedOverUnlimited') : t('usageDashboard.usedOverQuota') }}
        </p>
        <p class="mt-2 text-2xl font-semibold text-gray-900">
          {{ formatCurrency(group.used_amount, props.quotas?.unit_label) }} /
          {{ group.is_unlimited ? '∞' : formatCurrency(group.quota_amount, props.quotas?.unit_label) }}
        </p>
      </article>
    </div>
  </section>
</template>
