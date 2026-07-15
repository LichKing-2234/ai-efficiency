<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  getQuotaResetApproverConfigs,
  getQuotaResetNotificationSettings,
  listQuotaResetApproverCandidates,
  saveQuotaResetApproverConfigs,
  testQuotaResetNotificationSettings,
  updateQuotaResetNotificationSettings,
} from '@/api/quotaReset'
import { listDirectoryDepartments, listDirectorySources } from '@/api/directory'
import { useI18n } from '@/i18n'
import QuotaResetApprovalChainSettings from '@/components/settings/QuotaResetApprovalChainSettings.vue'
import type {
  Credential,
  DirectoryDepartment,
  DirectorySource,
  QuotaResetApproverCandidate,
  QuotaResetApproverConfig,
  QuotaResetApproverConfigInput,
  QuotaResetNotificationSettings,
  QuotaResetUnmatchedApproverRepresentative,
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
const searchingDepartments = ref(false)
const loadingApproverCandidates = ref(false)
const message = ref('')
const error = ref('')
const departmentSources = ref<DirectorySource[]>([])
const selectedDirectorySourceID = ref<number | null>(null)
const departmentSearch = ref('')
const departmentDropdownOpen = ref(false)
const departmentOptions = ref<DirectoryDepartment[]>([])
const approverCandidates = ref<QuotaResetApproverCandidate[]>([])
const unmatchedRepresentatives = ref<QuotaResetUnmatchedApproverRepresentative[]>([])
let departmentSearchRequestSeq = 0

const configForm = ref({
  department_external_id: '',
  department_display_path: '',
  approver_user_id: null as number | null,
  enabled: true,
})

const notification = ref<QuotaResetNotificationSettings>({
  enabled: false,
  channel: 'generic_webhook',
  url: '',
  auth_type: 'none',
  credential_id: null,
})

const bearerCredentials = computed(() => props.credentials.filter((credential) => credential.kind === 'secret_text'))
const selectedDepartmentLabel = computed(() => configForm.value.department_display_path || configForm.value.department_external_id)

onMounted(() => {
  void loadSettings()
  void loadDirectorySourceOptions()
})

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
      channel: notificationRes.data.data?.channel ?? 'generic_webhook',
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

async function loadDirectorySourceOptions() {
  try {
    const res = await listDirectorySources()
    departmentSources.value = res.data.data?.items ?? []
    const current = departmentSources.value.find((source) => source.last_successful_run_id) ?? departmentSources.value[0]
    selectedDirectorySourceID.value = current?.id ?? null
  } catch {
    departmentSources.value = []
    selectedDirectorySourceID.value = null
  }
}

function departmentDisplayPath(department: DirectoryDepartment) {
  return department.display_path || department.name || department.path || department.external_id
}

function approverOptionLabel(candidate: QuotaResetApproverCandidate) {
  const name = candidate.display_name || candidate.username || `User #${candidate.user_id}`
  return candidate.email ? `${name} · ${candidate.email}` : name
}

function unmatchedRepresentativeLabel(representative: QuotaResetUnmatchedApproverRepresentative) {
  const name = representative.display_name || representative.directory_member_external_id
  return representative.email ? `${name} · ${representative.email}` : name
}

async function searchDepartments() {
  const requestSeq = ++departmentSearchRequestSeq
  const sourceID = selectedDirectorySourceID.value
  if (!sourceID) {
    if (requestSeq === departmentSearchRequestSeq) {
      departmentOptions.value = []
      searchingDepartments.value = false
    }
    return
  }
  const query = departmentSearch.value.trim()
  searchingDepartments.value = true
  try {
    const res = await listDirectoryDepartments({
      source_id: sourceID,
      q: query,
    })
    if (requestSeq !== departmentSearchRequestSeq) return
    departmentOptions.value = res.data.data?.items ?? []
  } catch {
    if (requestSeq !== departmentSearchRequestSeq) return
    departmentOptions.value = []
  } finally {
    if (requestSeq === departmentSearchRequestSeq) {
      searchingDepartments.value = false
    }
  }
}

function invalidateDepartmentSearch() {
  departmentSearchRequestSeq += 1
  searchingDepartments.value = false
}

function toggleDepartmentDropdown() {
  if (!selectedDirectorySourceID.value) return
  departmentDropdownOpen.value = !departmentDropdownOpen.value
  if (departmentDropdownOpen.value) {
    void searchDepartments()
  } else {
    invalidateDepartmentSearch()
  }
}

function selectDepartment(department: DirectoryDepartment) {
  invalidateDepartmentSearch()
  configForm.value.department_external_id = department.external_id
  configForm.value.department_display_path = departmentDisplayPath(department)
  configForm.value.approver_user_id = null
  departmentSearch.value = ''
  departmentDropdownOpen.value = false
  departmentOptions.value = []
  unmatchedRepresentatives.value = []
  void loadApproverCandidates()
}

function resetSelectedDepartment() {
  invalidateDepartmentSearch()
  configForm.value.department_external_id = ''
  configForm.value.department_display_path = ''
  configForm.value.approver_user_id = null
  departmentSearch.value = ''
  departmentDropdownOpen.value = false
  departmentOptions.value = []
  approverCandidates.value = []
  unmatchedRepresentatives.value = []
}

async function loadApproverCandidates() {
  const sourceID = selectedDirectorySourceID.value
  const departmentID = configForm.value.department_external_id.trim()
  configForm.value.approver_user_id = null
  if (!sourceID || !departmentID) {
    approverCandidates.value = []
    unmatchedRepresentatives.value = []
    return
  }
  loadingApproverCandidates.value = true
  try {
    const res = await listQuotaResetApproverCandidates({
      source_id: sourceID,
      department_external_id: departmentID,
    })
    approverCandidates.value = res.data.data?.items ?? []
    unmatchedRepresentatives.value = res.data.data?.unmatched_representatives ?? []
  } catch {
    approverCandidates.value = []
    unmatchedRepresentatives.value = []
  } finally {
    loadingApproverCandidates.value = false
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
  const approverID = Number(configForm.value.approver_user_id)
  if (!departmentID || !Number.isInteger(approverID) || approverID <= 0) {
    return existing
  }
  return [
    ...existing,
    {
      department_external_id: departmentID,
      department_display_path: configForm.value.department_display_path.trim() || departmentID,
      approver_user_id: approverID,
      enabled: configForm.value.enabled,
    },
  ]
}

async function saveConfigs() {
  savingConfigs.value = true
  message.value = ''
  error.value = ''
  try {
    const res = await saveQuotaResetApproverConfigs(configRowsForSave(), 'replace_all')
    configs.value = res.data.data?.items ?? []
    configForm.value = { department_external_id: '', department_display_path: '', approver_user_id: null, enabled: true }
    invalidateDepartmentSearch()
    departmentSearch.value = ''
    departmentDropdownOpen.value = false
    departmentOptions.value = []
    approverCandidates.value = []
    unmatchedRepresentatives.value = []
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
      channel: notification.value.channel,
      url: notification.value.url.trim(),
      auth_type: notification.value.auth_type,
      credential_id: notification.value.auth_type === 'bearer_token' ? notification.value.credential_id ?? null : null,
    }
    const res = await updateQuotaResetNotificationSettings(payload)
    notification.value = {
      enabled: res.data.data?.enabled ?? payload.enabled,
      channel: res.data.data?.channel ?? payload.channel,
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
  } catch (err: any) {
    error.value = err?.response?.data?.message || err?.message || t('quotaResetSettings.notificationTestFailed')
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
	                  <div class="font-medium text-slate-800">
	                    {{ config.department_display_path || config.department_external_id }}
	                  </div>
	                  <div class="mt-1 text-xs text-slate-500">
	                    {{ config.department_external_id }}
	                  </div>
	                </td>
	                <td class="min-w-[14rem] px-3 py-2 text-gray-700">
	                  <div class="font-medium text-slate-800">
	                    {{ config.approver_username || `User #${config.approver_user_id}` }}
	                  </div>
	                  <div class="mt-1 text-xs text-slate-500">
	                    <span v-if="config.approver_email">{{ config.approver_email }}</span>
	                    <span v-else>User #{{ config.approver_user_id }}</span>
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

      <div class="grid gap-3 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_auto] md:items-start">
        <div class="block">
          <div class="relative">
            <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.departmentSearch') }}</span>
            <button
              type="button"
              data-testid="quota-reset-department-select"
              class="mt-1 flex w-full items-center justify-between gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm text-gray-900 hover:bg-gray-50 disabled:opacity-60"
              aria-haspopup="listbox"
              :aria-expanded="departmentDropdownOpen ? 'true' : 'false'"
              :disabled="!selectedDirectorySourceID"
              @click="toggleDepartmentDropdown"
            >
              <span class="truncate">
                {{ selectedDepartmentLabel || t('quotaResetSettings.departmentSelectPlaceholder') }}
              </span>
              <span aria-hidden="true" class="text-xs text-gray-400">v</span>
            </button>
            <div
              v-if="departmentDropdownOpen"
              class="absolute z-20 mt-1 w-full rounded-md border border-gray-200 bg-white p-2 shadow-lg"
            >
              <input
                v-model="departmentSearch"
                data-testid="quota-reset-department-filter"
                type="text"
                :placeholder="t('quotaResetSettings.departmentSearchPlaceholder')"
                class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                @input="searchDepartments"
                @keyup.enter="searchDepartments"
              />
              <div v-if="searchingDepartments" class="px-3 py-3 text-sm text-gray-500">
                {{ t('settings.loading') }}
              </div>
              <div
                v-else-if="departmentOptions.length > 0"
                class="mt-2 max-h-44 overflow-y-auto"
                role="listbox"
              >
                <button
                  v-for="department in departmentOptions"
                  :key="department.id"
                  type="button"
                  :data-testid="`quota-reset-department-option-${department.external_id}`"
                  class="block w-full rounded px-3 py-2 text-left text-sm hover:bg-slate-50"
                  role="option"
                  @click="selectDepartment(department)"
                >
                  <span class="block truncate font-medium text-slate-800">{{ departmentDisplayPath(department) }}</span>
                  <span class="block truncate text-xs text-slate-500">{{ department.external_id }}</span>
                </button>
              </div>
              <div v-else-if="departmentSearch" class="px-3 py-3 text-sm text-gray-500">
                {{ t('quotaResetSettings.noDepartmentMatches') }}
              </div>
            </div>
          </div>
          <label v-if="departmentSources.length > 1" class="mt-2 block">
            <span class="text-xs font-medium text-gray-500">{{ t('quotaResetSettings.directorySource') }}</span>
            <select
              v-model.number="selectedDirectorySourceID"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
              @change="resetSelectedDepartment"
            >
              <option v-for="source in departmentSources" :key="source.id" :value="source.id">{{ source.name }}</option>
            </select>
          </label>
        </div>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.approverSelect') }}</span>
          <select
            v-model.number="configForm.approver_user_id"
            data-testid="quota-reset-approver-select"
            class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            :disabled="loadingApproverCandidates || !configForm.department_external_id || approverCandidates.length === 0"
          >
            <option :value="null">
              {{ loadingApproverCandidates ? t('settings.loading') : t('quotaResetSettings.selectApproverPlaceholder') }}
            </option>
            <option v-for="candidate in approverCandidates" :key="candidate.user_id" :value="candidate.user_id">
              {{ approverOptionLabel(candidate) }}
            </option>
          </select>
          <div v-if="configForm.department_external_id && !loadingApproverCandidates && approverCandidates.length === 0 && unmatchedRepresentatives.length === 0" class="mt-2 text-xs text-gray-500">
            {{ t('quotaResetSettings.noApproverCandidates') }}
          </div>
          <div
            v-else-if="configForm.department_external_id && !loadingApproverCandidates && approverCandidates.length === 0 && unmatchedRepresentatives.length > 0"
            class="mt-2 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-800"
            data-testid="quota-reset-unmatched-representatives"
          >
            <div>
              {{ t('quotaResetSettings.unmatchedRepresentatives', { count: unmatchedRepresentatives.length }) }}
            </div>
            <div class="mt-1 space-y-1">
              <div v-for="representative in unmatchedRepresentatives" :key="representative.directory_member_external_id" class="truncate">
                {{ unmatchedRepresentativeLabel(representative) }}
              </div>
            </div>
          </div>
        </label>
        <button
          type="button"
          data-testid="quota-reset-save-approvers"
          class="self-start rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-60 md:mt-6"
          :disabled="savingConfigs"
          @click="saveConfigs"
        >
          {{ savingConfigs ? t('settings.saving') : t('quotaResetSettings.saveApprovers') }}
        </button>
      </div>
    </div>

    <div class="mt-6 border-t border-gray-200 pt-5">
      <QuotaResetApprovalChainSettings :source-id="selectedDirectorySourceID" />
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
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.channel') }}</span>
          <select v-model="notification.channel" data-testid="quota-reset-webhook-channel" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
            <option value="generic_webhook">{{ t('quotaResetSettings.channelGeneric') }}</option>
            <option value="wecom_group_robot">{{ t('quotaResetSettings.channelWeCom') }}</option>
          </select>
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
