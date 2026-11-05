<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import TeamAccountUserPicker from './TeamAccountUserPicker.vue'
import { transferAdminTeamAccount, transferTeamAccount } from '@/api/teamAccounts'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { teamAccountErrorMessage } from '@/utils/teamAccounts'
import type { TeamAccountSummary } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: boolean
  account: TeamAccountSummary | null
  mode?: 'user' | 'admin'
}>(), {
  mode: 'user',
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  transferred: [account: TeamAccountSummary]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)
const newOwnerUserID = ref<number | null>(null)
const confirmation = ref('')
const transferring = ref(false)
const error = ref('')

const parsedOwnerUserID = computed(() => newOwnerUserID.value)

const confirmationMatches = computed(() => {
  if (!props.account) return false
  return confirmation.value.trim() === props.account.name.trim()
})

const canTransfer = computed(() => (
  parsedOwnerUserID.value !== null
  && confirmationMatches.value
  && !transferring.value
))

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    newOwnerUserID.value = null
    confirmation.value = ''
    transferring.value = false
    error.value = ''
  },
)

function close() {
  if (transferring.value) return
  emit('update:modelValue', false)
}

async function submit() {
  if (!props.account) return
  if (parsedOwnerUserID.value === null) {
    error.value = t('teamAccounts.transfer.ownerRequired')
    return
  }
  if (!confirmationMatches.value) {
    error.value = t('teamAccounts.transfer.nameMismatch')
    return
  }
  if (transferring.value) return
  transferring.value = true
  error.value = ''
  try {
    if (props.mode === 'admin') {
      await transferAdminTeamAccount(props.account.id, parsedOwnerUserID.value)
    } else {
      await transferTeamAccount(props.account.id, parsedOwnerUserID.value)
    }
    ElMessage.success(t('teamAccounts.transfer.success'))
    emit('transferred', props.account)
    emit('update:modelValue', false)
  } catch (e: unknown) {
    error.value = teamAccountErrorMessage(e, t('teamAccounts.transfer.failed'))
  } finally {
    transferring.value = false
  }
}
</script>

<template>
  <ElDialog
    :model-value="props.modelValue"
    data-testid="team-account-transfer-dialog"
    append-to-body
    align-center
    :show-close="false"
    width="min(100%, 32rem)"
    :close-on-click-modal="!transferring"
    :close-on-press-escape="!transferring"
    @update:model-value="(value) => { if (!value) close() }"
  >
    <template #header>
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 class="text-base font-semibold text-gray-900">{{ t('teamAccounts.transfer.title') }}</h2>
          <p v-if="props.account" class="mt-1 break-words text-sm text-gray-500">{{ props.account.name }}</p>
        </div>
        <ElButton :disabled="transferring" @click="close">
          {{ t('teamAccounts.close') }}
        </ElButton>
      </div>
    </template>

    <div class="space-y-4">
      <p class="text-sm text-gray-600">{{ t('teamAccounts.transfer.help') }}</p>

      <label class="block text-sm">
        <span class="font-medium text-gray-700">{{ t('teamAccounts.transfer.newOwnerUserId') }}</span>
        <TeamAccountUserPicker
          v-model="newOwnerUserID"
          data-testid="team-account-transfer-owner"
          class="mt-1"
          :disabled="transferring"
        />
      </label>

      <label v-if="props.account" class="block text-sm">
        <span class="font-medium text-gray-700">
          {{ t('teamAccounts.transfer.confirmLabel', { name: props.account.name }) }}
        </span>
        <ElInput
          v-model="confirmation"
          data-testid="team-account-transfer-confirm-input"
          class="mt-1"
          :placeholder="props.account.name"
          :disabled="transferring"
        />
        <span v-if="confirmation.length > 0 && !confirmationMatches" class="mt-1 block text-xs text-red-600">
          {{ t('teamAccounts.transfer.nameMismatch') }}
        </span>
      </label>

      <ElAlert
        v-if="error"
        data-testid="team-account-transfer-error"
        type="error"
        :closable="false"
        show-icon
        :title="error"
      />
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <ElButton :disabled="transferring" @click="close">
          {{ t('teamAccounts.cancel') }}
        </ElButton>
        <ElButton
          data-testid="team-account-transfer-submit"
          type="primary"
          :loading="transferring"
          :disabled="!canTransfer"
          @click="submit"
        >
          {{ transferring ? t('teamAccounts.transfer.transferring') : t('teamAccounts.transfer.confirm') }}
        </ElButton>
      </div>
    </template>
  </ElDialog>
</template>
