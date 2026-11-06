<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  revealAdminTeamAccountCredentials,
  revealTeamAccountCredentials,
} from '@/api/teamAccounts'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { teamAccountErrorMessage } from '@/utils/teamAccounts'
import type { TeamAccountCredentials } from '@/types'

const props = defineProps<{
  modelValue: boolean
  accountId: number | null
  accountName?: string
  mode?: 'user' | 'admin'
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)

const credentials = ref<TeamAccountCredentials | null>(null)
const loading = ref(false)
const error = ref('')

watch(
  () => [props.modelValue, props.accountId] as const,
  ([open]) => {
    if (!open) {
      credentials.value = null
      error.value = ''
      return
    }
    void load()
  },
  { immediate: true },
)

async function load() {
  if (props.accountId === null || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const response = props.mode === 'admin'
      ? await revealAdminTeamAccountCredentials(props.accountId)
      : await revealTeamAccountCredentials(props.accountId)
    credentials.value = response.data.data ?? null
  } catch (e: unknown) {
    error.value = teamAccountErrorMessage(e, t('teamAccounts.credentials.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    ElMessage.success(t('teamAccounts.copied'))
  } catch {
    ElMessage.error(t('teamAccounts.copyFailed'))
  }
}

function close() {
  emit('update:modelValue', false)
}
</script>

<template>
  <ElDialog
    :model-value="modelValue"
    data-testid="team-account-credentials-dialog"
    append-to-body
    align-center
    width="min(100%, 30rem)"
    :show-close="false"
    @update:model-value="(value) => { if (!value) close() }"
  >
    <template #header>
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 class="text-base font-semibold text-gray-900">{{ t('teamAccounts.credentials.title') }}</h2>
          <p v-if="accountName" class="mt-1 break-words text-sm text-gray-500">{{ accountName }}</p>
        </div>
        <ElButton @click="close">{{ t('teamAccounts.close') }}</ElButton>
      </div>
    </template>

    <div class="space-y-4">
      <ElAlert
        v-if="error"
        type="error"
        :closable="false"
        show-icon
        :title="error"
      />
      <ElSkeleton v-else-if="loading" :rows="3" animated />
      <template v-else-if="credentials">
        <div class="space-y-1">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.credentials.username') }}</div>
          <div class="flex items-center gap-2">
            <code data-testid="team-account-credentials-username" class="min-w-0 flex-1 break-all rounded bg-gray-50 px-2 py-1 text-sm">{{ credentials.username }}</code>
            <ElButton size="small" @click="copy(credentials.username)">{{ t('teamAccounts.copy') }}</ElButton>
          </div>
        </div>
        <div class="space-y-1">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.credentials.password') }}</div>
          <div class="flex items-center gap-2">
            <code data-testid="team-account-credentials-password" class="min-w-0 flex-1 break-all rounded bg-gray-50 px-2 py-1 text-sm">{{ credentials.password }}</code>
            <ElButton size="small" @click="copy(credentials.password)">{{ t('teamAccounts.copy') }}</ElButton>
          </div>
        </div>
        <p class="text-xs text-amber-700">{{ t('teamAccounts.credentials.warning') }}</p>
      </template>
    </div>
  </ElDialog>
</template>
