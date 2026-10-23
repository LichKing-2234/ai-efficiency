<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import type { OwnerAccount, OwnerAccountQuota, OwnerAccountReferral } from '@/types'

const props = defineProps<{
  account: OwnerAccount
  quota: OwnerAccountQuota | null
  referral: OwnerAccountReferral | null
  mutationsEnabled: boolean
  querying: boolean
  cooldownActive: boolean
  unknownReset: boolean
  unknownInvite: boolean
}>()

const emit = defineEmits<{
  query: []
  reset: []
  invite: []
  history: []
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)

const resetHint = computed(() => {
  if (props.unknownReset) return t('ownerAccounts.resetBlockedUnknown')
  if (!props.quota || !props.quota.observed_at) return t('ownerAccounts.resetNeedsObservation')
  if (props.quota.reset_credits_remaining <= 0) return t('ownerAccounts.resetNoCredits')
  return ''
})

const inviteHint = computed(() => {
  if (props.unknownInvite) return t('ownerAccounts.inviteBlockedUnknown')
  if (!props.referral || !props.referral.observed_at) return t('ownerAccounts.inviteNeedsObservation')
  if (!props.referral.eligible || !props.referral.program_id) return t('ownerAccounts.inviteUnavailable')
  if (props.referral.remaining_invites <= 0) return t('ownerAccounts.inviteNoCapacity')
  return ''
})
</script>

<template>
  <div class="space-y-2">
    <div class="flex flex-wrap gap-2">
      <ElButton
        :data-testid="`owner-account-query-${account.id}`"
        size="small"
        :disabled="querying || cooldownActive"
        :loading="querying"
        @click="emit('query')"
      >
        {{ querying ? t('ownerAccounts.querying') : t('ownerAccounts.query') }}
      </ElButton>
      <ElButton
        v-if="mutationsEnabled"
        :data-testid="`owner-account-use-reset-${account.id}`"
        size="small"
        :disabled="!!resetHint"
        @click="emit('reset')"
      >
        {{ t('ownerAccounts.useResetCredit') }}
      </ElButton>
      <ElButton
        v-if="mutationsEnabled"
        :data-testid="`owner-account-invite-${account.id}`"
        size="small"
        :disabled="!!inviteHint"
        @click="emit('invite')"
      >
        {{ t('ownerAccounts.sendInvitation') }}
      </ElButton>
      <ElButton
        :data-testid="`owner-account-history-${account.id}`"
        size="small"
        text
        @click="emit('history')"
      >
        {{ t('ownerAccounts.history') }}
      </ElButton>
    </div>
    <p
      v-if="mutationsEnabled && resetHint"
      :data-testid="`owner-account-reset-hint-${account.id}`"
      class="text-xs text-slate-500"
    >
      {{ resetHint }}
    </p>
    <p
      v-if="mutationsEnabled && inviteHint"
      :data-testid="`owner-account-invite-hint-${account.id}`"
      class="text-xs text-slate-500"
    >
      {{ inviteHint }}
    </p>
  </div>
</template>
