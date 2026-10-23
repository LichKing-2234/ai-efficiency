<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import { ownerAccountCountdown, ownerAccountDateTime, ownerAccountUtilization } from '@/utils/ownerAccounts'
import type { OwnerAccount, OwnerAccountQuota, OwnerAccountUsage } from '@/types'

const props = defineProps<{
  open: boolean
  account: OwnerAccount
  usage: OwnerAccountUsage | null
  quota: OwnerAccountQuota | null
  submitting: boolean
  error?: string
}>()

const emit = defineEmits<{
  close: []
  confirm: []
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)
const acknowledged = ref(false)
const now = Date.now()

watch(
  () => props.open,
  (open) => {
    if (open) acknowledged.value = false
  },
  { immediate: true },
)

const hasFreshQuota = computed(() => !!props.quota?.observed_at)
const hasCredits = computed(() => (props.quota?.reset_credits_remaining ?? 0) > 0)
const canSubmit = computed(() => hasFreshQuota.value && hasCredits.value)
const expiry = computed(() => props.quota?.reset_credit_expiries?.[0] ?? '')
const observation = computed(() => ownerAccountDateTime(props.quota?.observed_at, locale.value))
</script>

<template>
  <ElDialog
    :model-value="open"
    append-to-body
    align-center
    width="min(38rem, calc(100vw - 2rem))"
    :title="t('ownerAccounts.resetDialogTitle')"
    @close="emit('close')"
  >
    <div data-testid="owner-account-reset-dialog" class="space-y-4">
      <p class="text-sm text-slate-600">{{ t('ownerAccounts.resetDialogIntro') }}</p>

      <ElAlert
        v-if="!canSubmit"
        data-testid="owner-account-reset-precondition"
        type="warning"
        :closable="false"
        show-icon
        :title="t('ownerAccounts.resetPrecondition')"
      />

      <div v-if="usage" class="space-y-2 rounded-md border border-slate-200 bg-slate-50 p-3 text-sm">
        <p
          v-for="item in [
            { key: 'five_hour', label: t('ownerAccounts.window5h') },
            { key: 'seven_day', label: t('ownerAccounts.window7d') },
          ] as const"
          :key="item.key"
          class="flex flex-wrap justify-between gap-2"
        >
          <span class="text-slate-600">{{ item.label }}</span>
          <span class="font-medium text-slate-900">
            {{ ownerAccountUtilization(usage[item.key].utilization_percent) }}
            <template v-if="usage[item.key].resets_at">
              · {{ t('ownerAccounts.resetsIn', { duration: ownerAccountCountdown(usage[item.key].resets_at, now, t) }) }}
            </template>
          </span>
        </p>
      </div>

      <dl class="grid gap-1 text-sm">
        <div class="flex flex-wrap justify-between gap-2">
          <dt class="text-slate-500">{{ t('ownerAccounts.resetObservationLabel') }}</dt>
          <dd class="font-medium text-slate-900">{{ observation || t('ownerAccounts.observedUnknown') }}</dd>
        </div>
        <div class="flex flex-wrap justify-between gap-2">
          <dt class="text-slate-500">{{ t('ownerAccounts.resetCredits') }}</dt>
          <dd class="font-medium text-slate-900">{{ t('ownerAccounts.resetCreditsRemaining', { count: quota?.reset_credits_remaining ?? 0 }) }}</dd>
        </div>
        <div class="flex flex-wrap justify-between gap-2">
          <dt class="text-slate-500">{{ t('ownerAccounts.creditExpiryLabel') }}</dt>
          <dd class="font-medium text-slate-900">
            {{ expiry ? ownerAccountDateTime(expiry, locale) : t('ownerAccounts.creditNoExpiry') }}
          </dd>
        </div>
      </dl>

      <ElAlert
        v-if="quota?.auto_reset_credit"
        data-testid="owner-account-reset-caveat"
        type="info"
        :closable="false"
        :title="t('ownerAccounts.resetCaveat')"
      />
      <ElAlert
        data-testid="owner-account-reset-irreversible"
        type="warning"
        :closable="false"
        :title="t('ownerAccounts.resetIrreversible')"
      />

      <ElAlert
        v-if="error"
        data-testid="owner-account-reset-error"
        type="error"
        :closable="false"
        show-icon
        :title="error"
      />

      <ElCheckbox
        v-model="acknowledged"
        data-testid="owner-account-reset-ack"
        :disabled="!canSubmit || submitting"
      >
        {{ t('ownerAccounts.resetAcknowledge') }}
      </ElCheckbox>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <ElButton :disabled="submitting" @click="emit('close')">{{ t('ownerAccounts.cancel') }}</ElButton>
        <ElButton
          data-testid="owner-account-reset-confirm"
          type="primary"
          :disabled="!acknowledged || !canSubmit || submitting"
          :loading="submitting"
          @click="emit('confirm')"
        >
          {{ submitting ? t('ownerAccounts.submitting') : t('ownerAccounts.confirmReset') }}
        </ElButton>
      </div>
    </template>
  </ElDialog>
</template>
