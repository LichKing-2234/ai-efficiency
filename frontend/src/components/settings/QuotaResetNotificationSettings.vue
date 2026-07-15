<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RefreshCw } from '@lucide/vue'
import {
  getQuotaResetNotificationSettings,
  testQuotaResetNotificationSettings,
  updateQuotaResetNotificationSettings,
} from '@/api/quotaReset'
import { useI18n } from '@/i18n'
import type {
  Credential,
  QuotaResetNotificationChannel,
  QuotaResetNotificationSettings,
  QuotaResetNotificationSettingsInput,
} from '@/types'

const props = defineProps<{
  credentials: Credential[]
}>()

const { t } = useI18n()
const enabled = ref(false)
const channelType = ref<QuotaResetNotificationChannel>('wecom_group_robot')
const authType = ref<'none' | 'bearer_token'>('none')
const credentialID = ref<number | null>(null)
const urlConfigured = ref(false)
const existingURLPreview = ref('')
const replacementURL = ref('')
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const settingsLoaded = ref(false)
const authoritativeChannelType = ref<QuotaResetNotificationChannel | null>(null)
const feedback = ref<{ kind: 'success' | 'warning' | 'error'; text: string } | null>(null)

const bearerCredentials = computed(() => (
  props.credentials.filter(credential => credential.kind === 'secret_text')
))
const operationInProgress = computed(() => loading.value || saving.value || testing.value)
const formLocked = computed(() => operationInProgress.value || !settingsLoaded.value)

onMounted(() => {
  void loadSettings()
})

function errorMessage(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

function applySettings(settings: QuotaResetNotificationSettings) {
  enabled.value = settings.enabled
  channelType.value = settings.channel_type
  authoritativeChannelType.value = settings.channel_type
  authType.value = settings.channel_type === 'generic_webhook' ? settings.auth_type : 'none'
  credentialID.value = settings.channel_type === 'generic_webhook'
    && settings.auth_type === 'bearer_token'
    ? settings.credential_id ?? null
    : null
  urlConfigured.value = settings.url_configured
  existingURLPreview.value = settings.url_preview
  replacementURL.value = ''
}

function isNotificationSettings(value: unknown): value is QuotaResetNotificationSettings {
  if (!value || typeof value !== 'object') return false
  const settings = value as Partial<QuotaResetNotificationSettings>
  if (
    typeof settings.enabled !== 'boolean'
    || (settings.channel_type !== 'wecom_group_robot' && settings.channel_type !== 'generic_webhook')
    || typeof settings.template_version !== 'number'
    || typeof settings.url_configured !== 'boolean'
    || typeof settings.url_preview !== 'string'
    || (settings.auth_type !== 'none' && settings.auth_type !== 'bearer_token')
  ) {
    return false
  }
  if (settings.channel_type === 'wecom_group_robot') {
    return settings.auth_type === 'none'
  }
  if (settings.auth_type === 'none') return true
  return Number.isInteger(settings.credential_id) && Number(settings.credential_id) > 0
}

async function loadSettings() {
  if (operationInProgress.value) return
  loading.value = true
  settingsLoaded.value = false
  feedback.value = null
  try {
    const response = await getQuotaResetNotificationSettings()
    const settings = response.data.data
    if (!isNotificationSettings(settings)) {
      throw new Error(t('quotaResetSettings.notificationLoadFailed'))
    }
    applySettings(settings)
    settingsLoaded.value = true
  } catch (err) {
    feedback.value = {
      kind: 'error',
      text: errorMessage(err, t('quotaResetSettings.notificationLoadFailed')),
    }
  } finally {
    loading.value = false
  }
}

function onChannelChange() {
  feedback.value = null
  if (channelType.value === 'wecom_group_robot') {
    authType.value = 'none'
    credentialID.value = null
  }
}

function onAuthChange() {
  feedback.value = null
  if (authType.value === 'none') {
    credentialID.value = null
  }
}

function replacementPart() {
  const url = replacementURL.value.trim()
  return url ? { url } : {}
}

function buildPayload(): QuotaResetNotificationSettingsInput | null {
  if (
    authoritativeChannelType.value !== null
    && channelType.value !== authoritativeChannelType.value
    && replacementURL.value.trim() === ''
  ) {
    feedback.value = {
      kind: 'error',
      text: t('quotaResetSettings.channelChangeURLRequired'),
    }
    return null
  }

  if (channelType.value === 'wecom_group_robot') {
    return {
      enabled: enabled.value,
      channel_type: 'wecom_group_robot',
      auth_type: 'none',
      credential_id: null,
      ...replacementPart(),
    }
  }

  if (authType.value === 'none') {
    return {
      enabled: enabled.value,
      channel_type: 'generic_webhook',
      auth_type: 'none',
      credential_id: null,
      ...replacementPart(),
    }
  }

  const selectedCredentialID = Number(credentialID.value)
  if (!Number.isInteger(selectedCredentialID) || selectedCredentialID <= 0) {
    feedback.value = {
      kind: 'error',
      text: t('quotaResetSettings.credentialRequired'),
    }
    return null
  }

  return {
    enabled: enabled.value,
    channel_type: 'generic_webhook',
    auth_type: 'bearer_token',
    credential_id: selectedCredentialID,
    ...replacementPart(),
  }
}

async function saveSettings() {
  if (!settingsLoaded.value || operationInProgress.value) return
  feedback.value = null
  const payload = buildPayload()
  if (!payload) return

  saving.value = true
  try {
    const response = await updateQuotaResetNotificationSettings(payload)
    const settings = response.data.data
    if (!isNotificationSettings(settings)) {
      throw new Error(t('quotaResetSettings.notificationSaveFailed'))
    }
    applySettings(settings)
    settingsLoaded.value = true
    feedback.value = {
      kind: 'success',
      text: t('quotaResetSettings.notificationSaved'),
    }
  } catch (err) {
    feedback.value = {
      kind: 'error',
      text: errorMessage(err, t('quotaResetSettings.notificationSaveFailed')),
    }
  } finally {
    saving.value = false
  }
}

async function testSettings() {
  if (!settingsLoaded.value || operationInProgress.value) return
  testing.value = true
  feedback.value = null
  try {
    const response = await testQuotaResetNotificationSettings()
    const result = response.data.data
    if (result?.delivered && result.warning === 'wecom_recipient_unavailable') {
      feedback.value = {
        kind: 'warning',
        text: t('quotaResetSettings.weComTestMentionUnavailable'),
      }
    } else if (result?.delivered) {
      feedback.value = {
        kind: 'success',
        text: t('quotaResetSettings.notificationTestSent'),
      }
    } else {
      feedback.value = {
        kind: 'error',
        text: result?.warning || t('quotaResetSettings.notificationTestFailed'),
      }
    }
  } catch (err) {
    feedback.value = {
      kind: 'error',
      text: errorMessage(err, t('quotaResetSettings.notificationTestFailed')),
    }
  } finally {
    testing.value = false
  }
}

function credentialPreview(credential: Credential) {
  if (typeof credential.summary?.preview === 'string') return credential.summary.preview
  if (typeof credential.summary?.username === 'string') return credential.summary.username
  if (typeof credential.summary?.fingerprint === 'string') return credential.summary.fingerprint
  return ''
}

function credentialOptionLabel(credential: Credential) {
  const preview = credentialPreview(credential)
  return preview ? `${credential.name} · ${preview}` : credential.name
}
</script>

<template>
  <section
    class="rounded-md border border-gray-200 bg-white p-4 sm:p-5"
    data-testid="quota-reset-notification-settings"
  >
    <h4 class="text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.notificationSettings') }}</h4>

    <div
      v-if="feedback"
      data-testid="quota-reset-notification-feedback"
      class="mt-3 break-words rounded-md p-3 text-sm"
      :class="{
        'bg-emerald-50 text-emerald-700': feedback.kind === 'success',
        'bg-amber-50 text-amber-800': feedback.kind === 'warning',
        'bg-red-50 text-red-700': feedback.kind === 'error',
      }"
    >
      {{ feedback.text }}
    </div>
    <div v-if="loading" class="mt-3 text-sm text-gray-500">{{ t('settings.loading') }}</div>

    <div v-else class="mt-3 grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,0.8fr)]">
      <div class="min-w-0 space-y-3">
        <label class="flex items-center gap-2 text-sm text-gray-700">
          <input
            v-model="enabled"
            data-testid="quota-reset-notification-enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-indigo-600"
            :disabled="formLocked"
          />
          {{ t('settings.enabled') }}
        </label>

        <label class="block min-w-0">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.channelType') }}</span>
          <select
            v-model="channelType"
            data-testid="quota-reset-notification-channel"
            class="mt-1 w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
            :disabled="formLocked"
            @change="onChannelChange"
          >
            <option value="wecom_group_robot">{{ t('quotaResetSettings.channelWeCom') }}</option>
            <option value="generic_webhook">{{ t('quotaResetSettings.channelGeneric') }}</option>
          </select>
        </label>

        <div v-if="urlConfigured" class="min-w-0 rounded-md bg-gray-50 px-3 py-2">
          <div class="text-xs font-medium text-gray-600">{{ t('quotaResetSettings.endpointConfigured') }}</div>
          <code class="mt-1 block break-all text-xs text-gray-700">{{ existingURLPreview }}</code>
        </div>

        <label class="block min-w-0">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.replaceEndpoint') }}</span>
          <input
            v-model="replacementURL"
            data-testid="quota-reset-notification-url"
            type="url"
            autocomplete="off"
            placeholder="https://hooks.example.com/quota-reset"
            class="mt-1 w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
            :disabled="formLocked"
          />
        </label>

        <template v-if="channelType === 'generic_webhook'">
          <label class="block min-w-0">
            <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.authType') }}</span>
            <select
              v-model="authType"
              data-testid="quota-reset-notification-auth"
              class="mt-1 w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
              :disabled="formLocked"
              @change="onAuthChange"
            >
              <option value="none">{{ t('quotaResetSettings.authNone') }}</option>
              <option value="bearer_token">{{ t('quotaResetSettings.authBearer') }}</option>
            </select>
          </label>

          <label v-if="authType === 'bearer_token'" class="block min-w-0">
            <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.credential') }}</span>
            <select
              v-model.number="credentialID"
              data-testid="quota-reset-notification-credential"
              class="mt-1 w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
              :disabled="formLocked"
            >
              <option :value="null">{{ t('settings.selectApiCredential') }}</option>
              <option v-for="credential in bearerCredentials" :key="credential.id" :value="credential.id">
                {{ credentialOptionLabel(credential) }}
              </option>
            </select>
          </label>
        </template>
      </div>

      <div class="min-w-0">
        <div class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.presetPreview') }}</div>
        <div
          data-testid="quota-reset-notification-preview"
          class="mt-1 min-w-0 rounded-md border border-gray-200 bg-gray-50 p-3 text-sm text-gray-700"
          aria-live="polite"
        >
          <template v-if="channelType === 'wecom_group_robot'">
            <div class="font-semibold text-gray-900">{{ t('quotaResetSettings.previewTitle') }}</div>
            <div class="mt-2 space-y-1">
              <div class="break-words">{{ t('quotaResetSettings.previewRequester', { value: 'Alice' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewTeam', { value: 'Department Alpha / Platform' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewGroup', { value: 'Group Alpha' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewReason', { value: 'Complete a time-sensitive build investigation.' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewNode', { value: '2/3 · Department Beta' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewProgress', { value: '1/3' }) }}</div>
            </div>
            <div class="mt-2 font-medium text-indigo-700">@Bob</div>
          </template>
          <template v-else>
            <div class="font-semibold text-gray-900">{{ t('quotaResetSettings.previewGenericTitle') }}</div>
            <div class="mt-2 space-y-1">
              <div class="break-words">{{ t('quotaResetSettings.previewRequester', { value: 'Alice' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewGroup', { value: 'Group Alpha' }) }}</div>
              <div class="break-words">{{ t('quotaResetSettings.previewNode', { value: '2/3 · Department Beta' }) }}</div>
            </div>
          </template>
        </div>
      </div>
    </div>

    <div class="mt-4 flex flex-wrap justify-end gap-2">
      <button
        type="button"
        data-testid="quota-reset-reload-notification"
        class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-50 disabled:opacity-40"
        :aria-label="t('quotaResetSettings.reloadNotification')"
        :title="t('quotaResetSettings.reloadNotification')"
        :disabled="operationInProgress"
        @click="loadSettings"
      >
        <RefreshCw aria-hidden="true" class="h-4 w-4" />
      </button>
      <button
        type="button"
        data-testid="quota-reset-test-notification"
        class="min-h-10 rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-60"
        :disabled="formLocked"
        @click="testSettings"
      >
        {{ testing ? t('settings.testing') : t('quotaResetSettings.testWebhook') }}
      </button>
      <button
        type="button"
        data-testid="quota-reset-save-notification"
        class="min-h-10 rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
        :disabled="formLocked"
        @click="saveSettings"
      >
        {{ saving ? t('settings.saving') : t('quotaResetSettings.saveWebhook') }}
      </button>
    </div>
  </section>
</template>
