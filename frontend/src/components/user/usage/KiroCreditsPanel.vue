<script setup lang="ts">
import { computed } from 'vue'
import type { UserUsageKiroSnapshot } from '@/types'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'

const props = withDefaults(defineProps<{
  snapshot: UserUsageKiroSnapshot | null
  loading?: boolean
  rangeLabel: string
}>(), {
  loading: false,
})

const { t } = useI18n()

const available = computed(() => props.snapshot?.available === true)
const unavailableMessage = computed(() => {
  if (!props.snapshot) return t('usageDashboard.kiroCreditsUnavailable')
  switch (props.snapshot.status) {
    case 'no_source':
      return t('usageDashboard.kiroCreditsNoSource')
    case 'unlinked':
      return t('usageDashboard.kiroCreditsUnlinked')
    case 'no_key':
      return t('usageDashboard.kiroCreditsNoKey')
    default:
      return t('usageDashboard.kiroCreditsUnavailable')
  }
})

function formatCredits(value: number): string {
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatNumber(value: number): string {
  return value.toLocaleString()
}
</script>

<template>
  <section data-testid="usage-kiro-credits" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
      <div class="flex flex-wrap items-baseline gap-2">
        <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.kiroCreditsTitle') }}</h2>
        <ElTag size="small" type="warning">kiro2cc</ElTag>
        <span class="text-xs text-gray-500">{{ rangeLabel }}</span>
      </div>
      <ElTag v-if="available && props.snapshot?.partial" size="small" type="info">
        {{ t('usageDashboard.kiroCreditsPartial') }}
      </ElTag>
    </div>

    <div v-if="props.loading" class="mt-4 space-y-2" data-testid="usage-kiro-credits-loading">
      <div class="h-8 w-40 animate-pulse rounded bg-gray-200"></div>
      <div class="h-3 w-64 animate-pulse rounded bg-gray-200"></div>
    </div>

    <p v-else-if="!available" class="mt-3 text-sm text-gray-500" data-testid="usage-kiro-credits-unavailable">
      {{ unavailableMessage }}
      <span v-if="props.snapshot?.status === 'no_source'" class="text-gray-400">
        — {{ t('usageDashboard.kiroCreditsNoSourceHelp') }}
      </span>
    </p>

    <template v-else>
      <div class="mt-4 grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div>
          <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsUsed') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900">{{ formatCredits(props.snapshot?.credits ?? 0) }}</p>
          <p class="mt-1 text-xs text-gray-400">{{ t('usageDashboard.kiroCreditsUnit') }}</p>
        </div>
        <div>
          <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsRequests') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900">{{ formatNumber(props.snapshot?.requests ?? 0) }}</p>
          <p class="mt-1 text-xs text-gray-400">{{ t('usageDashboard.kiroCreditsRequestsHelp') }}</p>
        </div>
        <div>
          <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsSaved') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900">{{ formatCredits(props.snapshot?.credits_saved ?? 0) }}</p>
          <p class="mt-1 text-xs text-gray-400">{{ t('usageDashboard.kiroCreditsSavedHelp') }}</p>
        </div>
        <div>
          <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsLifetime') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900">{{ formatCredits(props.snapshot?.lifetime_credits ?? 0) }}</p>
          <p class="mt-1 text-xs text-gray-400">
            {{ formatNumber(props.snapshot?.lifetime_requests ?? 0) }} {{ t('usageDashboard.kiroCreditsRequestsSuffix') }}
          </p>
        </div>
      </div>

      <div v-if="(props.snapshot?.by_model?.length ?? 0) > 0" class="mt-5">
        <ElTable :data="props.snapshot?.by_model ?? []" row-key="model" class="w-full" size="small">
          <ElTableColumn :label="t('usageDashboard.model')" min-width="150">
            <template #default="{ row: model }">
              <span class="block max-w-[16rem] truncate font-medium text-gray-900">{{ model.model }}</span>
            </template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.requests')" align="right" min-width="90">
            <template #default="{ row: model }">{{ formatNumber(model.requests) }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.kiroCreditsColumn')" align="right" min-width="110">
            <template #default="{ row: model }">{{ formatCredits(model.credits) }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.kiroCreditsEstimateTokens')" align="right" min-width="120">
            <template #default="{ row: model }">{{ formatTokenCount(model.input_tokens + model.output_tokens) }}</template>
          </ElTableColumn>
        </ElTable>
      </div>

      <p class="mt-3 text-xs text-gray-400">{{ t('usageDashboard.kiroCreditsNote') }}</p>
    </template>
  </section>
</template>
