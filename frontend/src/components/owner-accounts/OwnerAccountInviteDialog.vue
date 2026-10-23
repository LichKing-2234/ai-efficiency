<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import { ownerAccountDateTime } from '@/utils/ownerAccounts'
import type { OwnerAccount, OwnerAccountReferral } from '@/types'

const props = defineProps<{
  open: boolean
  account: OwnerAccount
  referral: OwnerAccountReferral | null
  submitting: boolean
  error?: string
}>()

const emit = defineEmits<{
  close: []
  confirm: [{ email: string; program_id: string | null; consent: boolean }]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)
const email = ref('')
const consent = ref(false)
const emailError = ref('')

watch(
  () => props.open,
  (open) => {
    if (!open) return
    email.value = ''
    consent.value = false
    emailError.value = ''
  },
  { immediate: true },
)

const consentRequired = computed(() => props.referral?.consent_required !== false)
const ready = computed(() => {
  const referral = props.referral
  if (!referral || !referral.observed_at) return false
  return referral.eligible && !!referral.program_id && referral.remaining_invites > 0
})
const canSubmit = computed(() => ready.value && (!consentRequired.value || consent.value))

function submit() {
  emailError.value = ''
  const trimmed = email.value.trim()
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)) {
    emailError.value = t('ownerAccounts.inviteEmailInvalid')
    return
  }
  if (!canSubmit.value) return
  emit('confirm', { email: trimmed, program_id: props.referral?.program_id ?? null, consent: true })
}
</script>

<template>
  <ElDialog
    :model-value="open"
    append-to-body
    align-center
    width="min(36rem, calc(100vw - 2rem))"
    :title="t('ownerAccounts.inviteDialogTitle')"
    @close="emit('close')"
  >
    <div data-testid="owner-account-invite-dialog" class="space-y-4">
      <p class="text-sm text-slate-600">{{ t('ownerAccounts.inviteDialogIntro') }}</p>

      <ElAlert
        v-if="!ready"
        data-testid="owner-account-invite-precondition"
        type="warning"
        :closable="false"
        show-icon
        :title="t('ownerAccounts.invitePrecondition')"
      />

      <dl v-if="referral" class="grid gap-1 text-sm">
        <div class="flex flex-wrap justify-between gap-2">
          <dt class="text-slate-500">{{ t('ownerAccounts.inviteProgram') }}</dt>
          <dd class="font-medium text-slate-900">{{ referral.program_name || referral.program_id || '-' }}</dd>
        </div>
        <div class="flex flex-wrap justify-between gap-2">
          <dt class="text-slate-500">{{ t('ownerAccounts.referral') }}</dt>
          <dd class="font-medium text-slate-900">{{ t('ownerAccounts.inviteCapacity', { count: referral.remaining_invites }) }}</dd>
        </div>
        <div class="flex flex-wrap justify-between gap-2">
          <dt class="text-slate-500">{{ t('ownerAccounts.observed') }}</dt>
          <dd class="font-medium text-slate-900">
            {{ referral.observed_at ? ownerAccountDateTime(referral.observed_at, locale) : t('ownerAccounts.observedUnknown') }}
          </dd>
        </div>
      </dl>

      <label class="block">
        <span class="text-sm font-medium text-slate-700">{{ t('ownerAccounts.inviteEmail') }}</span>
        <ElInput
          v-model="email"
          data-testid="owner-account-invite-email"
          class="mt-1 w-full"
          :placeholder="t('ownerAccounts.inviteEmailPlaceholder')"
          :disabled="submitting"
        />
      </label>

      <ElAlert
        v-if="emailError"
        data-testid="owner-account-invite-error"
        type="error"
        :closable="false"
        :title="emailError"
      />

      <ElAlert
        v-if="error"
        data-testid="owner-account-invite-submit-error"
        type="error"
        :closable="false"
        show-icon
        :title="error"
      />

      <ElCheckbox
        v-if="consentRequired"
        v-model="consent"
        data-testid="owner-account-invite-consent"
        :disabled="submitting"
      >
        {{ t('ownerAccounts.inviteConsent') }}
      </ElCheckbox>

      <ElAlert
        data-testid="owner-account-invite-identity"
        type="info"
        :closable="false"
        :title="t('ownerAccounts.inviteIdentity')"
      />
      <ElAlert
        data-testid="owner-account-invite-irreversible"
        type="warning"
        :closable="false"
        :title="t('ownerAccounts.inviteIrreversible')"
      />
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <ElButton :disabled="submitting" @click="emit('close')">{{ t('ownerAccounts.cancel') }}</ElButton>
        <ElButton
          data-testid="owner-account-invite-confirm"
          type="primary"
          :disabled="!canSubmit || submitting"
          :loading="submitting"
          @click="submit"
        >
          {{ submitting ? t('ownerAccounts.submitting') : t('ownerAccounts.confirmInvite') }}
        </ElButton>
      </div>
    </template>
  </ElDialog>
</template>
