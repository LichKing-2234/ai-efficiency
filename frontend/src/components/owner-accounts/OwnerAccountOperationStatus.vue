<script setup lang="ts">
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'

const props = defineProps<{
  accountId: number
  operation: {
    id: number
    action: 'refresh_quota' | 'refresh_referral' | 'reset_credit' | 'send_invitation'
    status: 'running' | 'succeeded' | 'failed' | 'unknown'
    windowsReset?: number
    sent?: boolean
    errorCode?: string
  } | null
  unknownReset: boolean
  unknownInvite: boolean
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)
</script>

<template>
  <div class="space-y-2">
    <ElAlert
      v-if="unknownReset || unknownInvite"
      :data-testid="`owner-account-unknown-${accountId}`"
      type="error"
      :closable="false"
      show-icon
      :title="t('ownerAccounts.unknownTitle')"
    >
      <p class="text-sm">{{ unknownReset ? t('ownerAccounts.unknownReset') : t('ownerAccounts.unknownInvite') }}</p>
    </ElAlert>

    <ElAlert
      v-if="operation && operation.status !== 'unknown'"
      :data-testid="`owner-account-operation-${accountId}`"
      :type="operation.status === 'succeeded' ? 'success' : operation.status === 'failed' ? 'error' : 'info'"
      :closable="false"
      show-icon
      :title="operation.status === 'running'
        ? t('ownerAccounts.operationRunning')
        : operation.status === 'succeeded'
          ? (operation.action === 'reset_credit'
            ? t('ownerAccounts.resetSucceeded', { count: operation.windowsReset ?? 0 })
            : t('ownerAccounts.inviteSucceeded'))
          : t('ownerAccounts.operationFailed')"
    >
      <p v-if="operation.status === 'running'" class="text-sm">{{ t('ownerAccounts.operationProgress') }}</p>
      <p v-else-if="operation.status === 'failed' && operation.errorCode" class="text-sm">{{ operation.errorCode }}</p>
    </ElAlert>
  </div>
</template>
