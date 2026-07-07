<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  getQuotaResetApproverConfigs,
  getQuotaResetNotificationSettings,
  saveQuotaResetApproverConfigs,
  testQuotaResetNotificationSettings,
  updateQuotaResetNotificationSettings,
} from '@/api/quotaReset'
import { useI18n } from '@/i18n'
import type {
  Credential,
  QuotaResetApproverConfig,
  QuotaResetApproverConfigInput,
  QuotaResetNotificationSettings,
} from '@/types'

const props = defineProps<{
  credentials: Credential[]
}>()

const { t } = useI18n()
const configs = ref<QuotaResetApproverConfig[]>([])
const loading = ref(false)
const savingConfigs = ref(false)
const savingNotification = ref(false)
const testingNotification = ref(false)
const message = ref('')
const error = ref('')

const configForm = ref({
  department_external_id: '',
  department_display_path: '',
  approver_user_ids: '',
  enabled: true,
})

const notification = ref<QuotaResetNotificationSettings>({
  enabled: false,
  url: '',
  auth_type: 'none',
  credential_id: null,
})

const bearerCredentials = computed(() => props.credentials.filter((credential) => credential.kind === 'secret_text'))

onMounted(loadSettings)

async function loadSettings() {
  loading.value = true
  error.value = ''
  try {
    const [configsRes, notificationRes] = await Promise.all([
      getQuotaResetApproverConfigs(),
      getQuotaResetNotificationSettings(),
    ])
    configs.value = configsRes.data.data?.items ?? []
    notification.value = {
      enabled: notificationRes.data.data?.enabled ?? false,
      url: notificationRes.data.data?.url ?? '',
      auth_type: notificationRes.data.data?.auth_type ?? 'none',
      credential_id: notificationRes.data.data?.credential_id ?? null,
      updated_at: notificationRes.data.data?.updated_at,
    }
  } catch {
    error.value = t('quotaResetSettings.loadFailed')
  } finally {
    loading.value = false
  }
}

function configRowsForSave(): QuotaResetApproverConfigInput[] {
  const existing = configs.value.map((config) => ({
    department_external_id: config.department_external_id.trim(),
    department_display_path: config.department_display_path.trim(),
    approver_user_id: Number(config.approver_user_id),
    enabled: config.enabled,
  })).filter((config) => config.department_external_id && Number.isInteger(config.approver_user_id) && config.approver_user_id > 0)
  const departmentID = configForm.value.department_external_id.trim()
  const approverIDs = configForm.value.approver_user_ids
    .split(',')
    .map((part) => Number(part.trim()))
    .filter((id) => Number.isInteger(id) && id > 0)
  if (!departmentID || approverIDs.length === 0) {
    return existing
  }
  return [
    ...existing,
    ...approverIDs.map((approverID) => ({
      department_external_id: departmentID,
      department_display_path: configForm.value.department_display_path.trim() || departmentID,
      approver_user_id: approverID,
      enabled: configForm.value.enabled,
    })),
  ]
}

async function saveConfigs() {
  savingConfigs.value = true
  message.value = ''
  error.value = ''
  try {
    const res = await saveQuotaResetApproverConfigs(configRowsForSave(), 'replace_all')
    configs.value = res.data.data?.items ?? []
    configForm.value = { department_external_id: '', department_display_path: '', approver_user_ids: '', enabled: true }
    message.value = t('quotaResetSettings.configSaved')
  } catch {
    error.value = t('quotaResetSettings.configSaveFailed')
  } finally {
    savingConfigs.value = false
  }
}

function removeConfig(index: number) {
  configs.value = configs.value.filter((_, i) => i !== index)
}

async function saveNotification() {
  savingNotification.value = true
  message.value = ''
  error.value = ''
  try {
    const payload: QuotaResetNotificationSettings = {
      enabled: notification.value.enabled,
      url: notification.value.url.trim(),
      auth_type: notification.value.auth_type,
      credential_id: notification.value.auth_type === 'bearer_token' ? notification.value.credential_id ?? null : null,
    }
    const res = await updateQuotaResetNotificationSettings(payload)
    notification.value = {
      enabled: res.data.data?.enabled ?? payload.enabled,
      url: res.data.data?.url ?? payload.url,
      auth_type: res.data.data?.auth_type ?? payload.auth_type,
      credential_id: res.data.data?.credential_id ?? payload.credential_id,
      updated_at: res.data.data?.updated_at,
    }
    message.value = t('quotaResetSettings.notificationSaved')
  } catch {
    error.value = t('quotaResetSettings.notificationSaveFailed')
  } finally {
    savingNotification.value = false
  }
}

async function testNotification() {
  testingNotification.value = true
  message.value = ''
  error.value = ''
  try {
    await testQuotaResetNotificationSettings()
    message.value = t('quotaResetSettings.notificationTestSent')
  } catch {
    error.value = t('quotaResetSettings.notificationTestFailed')
  } finally {
    testingNotification.value = false
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
  <section class="rounded-lg bg-white p-6 shadow" data-testid="quota-reset-approval-settings">
    <div>
      <h3 class="text-lg font-semibold text-gray-900">{{ t('quotaResetSettings.title') }}</h3>
      <p class="mt-1 text-sm text-gray-500">{{ t('quotaResetSettings.subtitle') }}</p>
    </div>

    <div v-if="loading" class="mt-4 text-sm text-gray-500">{{ t('settings.loading') }}</div>
    <div v-if="error" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>
    <div v-if="message" class="mt-4 rounded-md bg-emerald-50 p-3 text-sm text-emerald-700">{{ message }}</div>

    <div class="mt-5 space-y-4">
      <div>
        <h4 class="text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.approvers') }}</h4>
        <div class="mt-2 overflow-x-auto rounded-md border border-gray-200">
          <table class="min-w-full divide-y divide-gray-200 text-sm">
            <thead class="bg-gray-50 text-left text-xs font-medium uppercase text-gray-500">
              <tr>
	                <th class="px-3 py-2">{{ t('quotaResetSettings.department') }}</th>
	                <th class="px-3 py-2">{{ t('quotaResetSettings.approver') }}</th>
	                <th class="px-3 py-2">{{ t('settings.enabled') }}</th>
	                <th class="px-3 py-2">{{ t('settings.actions') }}</th>
	              </tr>
	            </thead>
	            <tbody class="divide-y divide-gray-100 bg-white">
	              <tr v-for="(config, index) in configs" :key="config.id || `${config.department_external_id}-${config.approver_user_id}`">
	                <td class="min-w-[18rem] px-3 py-2">
	                  <input
	                    v-model="config.department_display_path"
	                    :aria-label="t('quotaResetSettings.displayPath')"
	                    type="text"
	                    class="w-full rounded-md border border-gray-300 px-2 py-1 text-sm"
	                  />
	                  <input
	                    v-model="config.department_external_id"
	                    :aria-label="t('quotaResetSettings.departmentID')"
	                    type="text"
	                    class="mt-1 w-full rounded-md border border-gray-200 px-2 py-1 text-xs text-gray-600"
	                  />
	                </td>
	                <td class="min-w-[14rem] px-3 py-2 text-gray-700">
	                  <input
	                    v-model.number="config.approver_user_id"
	                    :aria-label="t('quotaResetSettings.approverIDs')"
	                    type="number"
	                    min="1"
	                    class="w-full rounded-md border border-gray-300 px-2 py-1 text-sm"
	                  />
	                  <div class="mt-1 text-xs text-gray-500">
	                    {{ config.approver_username || config.approver_user_id }}
	                    <span v-if="config.approver_email">· {{ config.approver_email }}</span>
	                  </div>
	                </td>
	                <td class="px-3 py-2 text-gray-700">
	                  <input v-model="config.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600" />
	                </td>
	                <td class="px-3 py-2 text-right">
	                  <button
	                    type="button"
	                    :data-testid="`quota-reset-config-remove-${config.id}`"
	                    class="text-sm font-medium text-red-600 hover:text-red-700"
	                    @click="removeConfig(index)"
	                  >
	                    {{ t('settings.delete') }}
	                  </button>
	                </td>
	              </tr>
	              <tr v-if="configs.length === 0">
	                <td colspan="4" class="px-3 py-3 text-gray-500">{{ t('quotaResetSettings.noApprovers') }}</td>
	              </tr>
	            </tbody>
	          </table>
        </div>
      </div>

      <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end">
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.departmentID') }}</span>
          <input v-model="configForm.department_external_id" type="text" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.displayPath') }}</span>
          <input v-model="configForm.department_display_path" type="text" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.approverIDs') }}</span>
          <input v-model="configForm.approver_user_ids" type="text" placeholder="12,34" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </label>
	        <button
	          type="button"
	          data-testid="quota-reset-save-approvers"
	          class="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-60"
	          :disabled="savingConfigs"
          @click="saveConfigs"
        >
          {{ savingConfigs ? t('settings.saving') : t('quotaResetSettings.saveApprovers') }}
        </button>
      </div>
    </div>

    <div class="mt-6 border-t border-gray-200 pt-5">
      <h4 class="text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.webhook') }}</h4>
      <div class="mt-3 grid gap-3 md:grid-cols-2">
        <label class="flex items-center gap-2 text-sm text-gray-700">
          <input
            v-model="notification.enabled"
            data-testid="quota-reset-webhook-enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-indigo-600"
          />
          {{ t('settings.enabled') }}
        </label>
        <label class="block md:col-span-2">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.webhookURL') }}</span>
          <input
            v-model="notification.url"
            data-testid="quota-reset-webhook-url"
            type="url"
            placeholder="https://hooks.example.com/ai-efficiency"
            class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
          />
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.authType') }}</span>
          <select v-model="notification.auth_type" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
            <option value="none">{{ t('quotaResetSettings.authNone') }}</option>
            <option value="bearer_token">{{ t('quotaResetSettings.authBearer') }}</option>
          </select>
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.credential') }}</span>
          <select v-model.number="notification.credential_id" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm" :disabled="notification.auth_type !== 'bearer_token'">
            <option :value="null">{{ t('settings.selectApiCredential') }}</option>
            <option v-for="credential in bearerCredentials" :key="credential.id" :value="credential.id">
              {{ credentialOptionLabel(credential) }}
            </option>
          </select>
        </label>
      </div>
      <div class="mt-4 flex flex-wrap justify-end gap-2">
        <button
          type="button"
          class="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-60"
          :disabled="testingNotification"
          @click="testNotification"
        >
          {{ testingNotification ? t('settings.testing') : t('quotaResetSettings.testWebhook') }}
        </button>
        <button
          type="button"
          data-testid="quota-reset-save-notification"
          class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
          :disabled="savingNotification"
          @click="saveNotification"
        >
          {{ savingNotification ? t('settings.saving') : t('quotaResetSettings.saveWebhook') }}
        </button>
      </div>
    </div>
  </section>
</template>
