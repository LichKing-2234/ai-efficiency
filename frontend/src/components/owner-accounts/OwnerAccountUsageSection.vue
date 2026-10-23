<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import { formatCompactNumber } from '@/utils/formatters'
import {
  ownerAccountCost,
  ownerAccountCountdown,
  ownerAccountDateTime,
  ownerAccountUtilization,
} from '@/utils/ownerAccounts'
import type { OwnerAccount, OwnerAccountUsage } from '@/types'

const props = defineProps<{
  account: OwnerAccount
  usage: OwnerAccountUsage | null
  loading: boolean
  error: string
  cooldownUntil: string
  now: number
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)
const cooldownTime = computed(() => ownerAccountDateTime(props.cooldownUntil, locale.value))
const observed = computed(() => {
  if (!props.usage?.observed_at) return t('ownerAccounts.observedUnknown')
  return t('ownerAccounts.observedAt', { time: ownerAccountDateTime(props.usage.observed_at, locale.value) })
})

function countdown(value: string | null) {
  return ownerAccountCountdown(value, props.now, t)
}
</script>

<template>
  <section :data-testid="`owner-account-usage-${account.id}`" class="space-y-3">
    <h3 class="text-sm font-semibold text-slate-900">{{ t('ownerAccounts.usage') }}</h3>

    <ElAlert
      v-if="cooldownUntil"
      :data-testid="`owner-account-cooldown-${account.id}`"
      type="warning"
      :closable="false"
      :title="cooldownTime ? t('ownerAccounts.cooldown', { time: cooldownTime }) : t('ownerAccounts.cooldownShort')"
    />

    <div
      v-if="loading && !usage"
      :data-testid="`owner-account-usage-loading-${account.id}`"
      class="text-sm text-slate-500"
    >
      {{ baseT('usageDashboard.loading') }}
    </div>

    <ElAlert
      v-else-if="error && !usage"
      :data-testid="`owner-account-usage-error-${account.id}`"
      type="warning"
      :closable="false"
      show-icon
      :title="t('ownerAccounts.usageUnavailable')"
    />

    <div v-if="usage" class="grid gap-3 md:grid-cols-2">
      <div
        v-for="window in ([
          { key: 'five_hour', testid: '5h', label: t('ownerAccounts.window5h') },
          { key: 'seven_day', testid: '7d', label: t('ownerAccounts.window7d') },
        ] as const)"
        :key="window.key"
        :data-testid="`owner-account-usage-${window.testid}-${account.id}`"
        class="rounded-md border border-slate-200 bg-slate-50 p-3"
      >
        <div class="flex items-baseline justify-between gap-2">
          <span class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ window.label }}</span>
          <span class="text-lg font-semibold text-slate-900">
            {{ ownerAccountUtilization(usage[window.key].utilization_percent) }}
          </span>
        </div>
        <p v-if="usage[window.key].resets_at" class="mt-1 text-xs text-slate-600">
          {{ t('ownerAccounts.resetsIn', { duration: countdown(usage[window.key].resets_at) }) }}
        </p>
        <dl class="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-xs text-slate-600">
          <div class="flex justify-between gap-2">
            <dt>{{ t('ownerAccounts.requests') }}</dt>
            <dd class="font-medium text-slate-900">{{ formatCompactNumber(usage[window.key].requests) }}</dd>
          </div>
          <div class="flex justify-between gap-2">
            <dt>{{ t('ownerAccounts.tokens') }}</dt>
            <dd class="font-medium text-slate-900">{{ formatCompactNumber(usage[window.key].tokens) }}</dd>
          </div>
          <div class="flex justify-between gap-2">
            <dt>{{ t('ownerAccounts.apiCost') }}</dt>
            <dd class="font-medium text-slate-900">{{ ownerAccountCost(usage[window.key].api_cost_usd) }}</dd>
          </div>
          <div class="flex justify-between gap-2">
            <dt>{{ t('ownerAccounts.userCost') }}</dt>
            <dd class="font-medium text-slate-900">{{ ownerAccountCost(usage[window.key].user_cost_usd) }}</dd>
          </div>
        </dl>
      </div>
    </div>

    <p v-if="usage" class="text-xs text-slate-500">{{ observed }}</p>
    <ElAlert
      v-if="error && usage"
      :data-testid="`owner-account-usage-error-${account.id}`"
      type="warning"
      :closable="false"
      show-icon
      :title="t('ownerAccounts.sectionUnavailable')"
    />
  </section>
</template>
