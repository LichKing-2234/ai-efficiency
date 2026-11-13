<script setup lang="ts">
import { computed } from 'vue'
import type { UserUsageKiroSnapshot } from '@/types'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'

const props = withDefaults(defineProps<{
  snapshot: UserUsageKiroSnapshot | null
  rangeLabel: string
  loading?: boolean
}>(), {
  loading: false,
})

const { t } = useI18n()

const credits = computed(() => props.snapshot?.credits ?? 0)
const requests = computed(() => props.snapshot?.requests ?? 0)
const saved = computed(() => props.snapshot?.credits_saved ?? 0)
const lifetime = computed(() => props.snapshot?.lifetime_credits ?? 0)
const tokens = computed(() => (props.snapshot?.input_tokens ?? 0) + (props.snapshot?.output_tokens ?? 0))
const average = computed(() => (requests.value > 0 ? credits.value / requests.value : 0))

function formatCredits(value: number): string {
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatNumber(value: number): string {
  return value.toLocaleString()
}
</script>

<template>
  <div class="grid grid-cols-2 gap-4 xl:grid-cols-4">
    <template v-if="props.loading">
      <section
        v-for="index in 4"
        :key="index"
        class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm"
        data-testid="kiro-stats-loading"
      >
        <div class="h-3 w-20 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-3 h-9 w-28 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-2 h-3 w-24 animate-pulse rounded bg-gray-200"></div>
      </section>
    </template>

    <template v-else>
      <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
        <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsUsed') }} · {{ rangeLabel }}</p>
        <p class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCredits(credits) }}</p>
        <p class="mt-1 text-xs text-gray-500">{{ t('usageDashboard.kiroCreditsSaved') }} {{ formatCredits(saved) }} · {{ t('usageDashboard.kiroCreditsUnit') }}</p>
      </section>

      <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
        <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsRequests') }}</p>
        <p class="mt-2 text-2xl font-semibold text-gray-900">{{ formatNumber(requests) }}</p>
        <p class="mt-1 text-xs text-gray-500">{{ t('usageDashboard.kiroCreditsRequestsHelp') }}</p>
      </section>

      <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
        <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsEstimateTokens') }}</p>
        <p class="mt-2 text-2xl font-semibold text-gray-900">{{ formatTokenCount(tokens) }}</p>
        <p class="mt-1 text-xs text-gray-500">
          {{ t('usageDashboard.inputShort') }} {{ formatTokenCount(props.snapshot?.input_tokens ?? 0) }} · {{ t('usageDashboard.outputShort') }} {{ formatTokenCount(props.snapshot?.output_tokens ?? 0) }}
        </p>
      </section>

      <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
        <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.kiroCreditsPerRequest') }}</p>
        <p class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCredits(average) }}</p>
        <p class="mt-1 text-xs text-gray-500">
          {{ t('usageDashboard.kiroCreditsLifetime') }} {{ formatCredits(lifetime) }}
        </p>
      </section>
    </template>
  </div>
</template>
