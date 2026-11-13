<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageKiroSnapshot } from '@/types'
import { useI18n } from '@/i18n'
import KiroStatsCards from '@/components/user/usage/KiroStatsCards.vue'

const KiroTrendChart = defineAsyncComponent(() => import('@/components/user/usage/KiroTrendChart.vue'))
const KiroModelChart = defineAsyncComponent(() => import('@/components/user/usage/KiroModelChart.vue'))

const props = withDefaults(defineProps<{
  snapshot: UserUsageKiroSnapshot | null
  loading?: boolean
  rangeLabel: string
}>(), {
  loading: false,
})

const { t } = useI18n()

const available = computed(() => props.snapshot?.available === true)
const partial = computed(() => available.value && props.snapshot?.partial === true)

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

const unavailableHelp = computed(() => {
  if (props.snapshot?.status === 'no_source') return t('usageDashboard.kiroCreditsNoSourceHelp')
  if (props.snapshot?.status === 'no_key') return t('usageDashboard.kiroCreditsNoKeyHelp')
  return ''
})
</script>

<template>
  <div class="space-y-6" data-testid="kiro-credits-dashboard">
    <template v-if="props.loading && !props.snapshot">
      <KiroStatsCards :snapshot="null" :range-label="rangeLabel" loading />
      <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
          <div class="h-72 animate-pulse rounded bg-gray-100"></div>
        </section>
        <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
          <div class="h-72 animate-pulse rounded bg-gray-100"></div>
        </section>
      </div>
    </template>

    <template v-else-if="!available">
      <section class="rounded-lg border border-gray-200 bg-white p-8 text-center shadow-sm" data-testid="kiro-credits-unavailable">
        <p class="text-base font-medium text-gray-900">{{ unavailableMessage }}</p>
        <p v-if="unavailableHelp" class="mx-auto mt-2 max-w-xl text-sm text-gray-500">{{ unavailableHelp }}</p>
        <p v-if="props.snapshot?.ambiguous_key" class="mt-2 text-xs text-amber-600">{{ t('usageDashboard.kiroCreditsAmbiguousKey') }}</p>
      </section>
    </template>

    <template v-else>
      <ElAlert
        v-if="partial"
        data-testid="kiro-credits-partial"
        type="info"
        :closable="false"
        show-icon
        :title="t('usageDashboard.kiroCreditsPartialHelp')"
      />
      <KiroStatsCards :snapshot="props.snapshot" :range-label="rangeLabel" />
      <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <KiroTrendChart :data="props.snapshot?.by_day ?? []" :loading="false" />
        <KiroModelChart :data="props.snapshot?.by_model ?? []" :loading="false" />
      </div>
      <p class="text-xs text-gray-400">{{ t('usageDashboard.kiroCreditsNote') }}</p>
    </template>
  </div>
</template>
