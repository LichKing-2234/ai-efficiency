<script setup lang="ts">
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import { ownerAccountDateTime } from '@/utils/ownerAccounts'
import type { OwnerAccount } from '@/types'

const props = defineProps<{ account: OwnerAccount }>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)

function yesNo(value: boolean) {
  return value ? t('ownerAccounts.yes') : t('ownerAccounts.no')
}
</script>

<template>
  <dl :data-testid="`owner-account-health-${account.id}`" class="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
    <div class="flex justify-between gap-3 border-b border-slate-100 py-1">
      <dt class="text-slate-500">{{ t('ownerAccounts.schedulable') }}</dt>
      <dd class="font-medium text-slate-900">{{ yesNo(account.health.schedulable) }}</dd>
    </div>
    <div class="flex justify-between gap-3 border-b border-slate-100 py-1">
      <dt class="text-slate-500">{{ t('ownerAccounts.reauthRequired') }}</dt>
      <dd class="font-medium text-slate-900">{{ yesNo(account.health.reauth_required) }}</dd>
    </div>
    <div class="flex justify-between gap-3 border-b border-slate-100 py-1">
      <dt class="text-slate-500">{{ t('ownerAccounts.banned') }}</dt>
      <dd class="font-medium text-slate-900">{{ yesNo(account.health.banned) }}</dd>
    </div>
    <div class="flex justify-between gap-3 border-b border-slate-100 py-1">
      <dt class="text-slate-500">{{ t('ownerAccounts.forbidden') }}</dt>
      <dd class="font-medium text-slate-900">{{ yesNo(account.health.forbidden) }}</dd>
    </div>
    <div class="flex justify-between gap-3 border-b border-slate-100 py-1">
      <dt class="text-slate-500">{{ t('ownerAccounts.rateLimitResetLabel') }}</dt>
      <dd class="font-medium text-slate-900">
        {{ account.health.rate_limit_reset_at ? ownerAccountDateTime(account.health.rate_limit_reset_at, locale) : t('ownerAccounts.notReported') }}
      </dd>
    </div>
  </dl>
</template>
