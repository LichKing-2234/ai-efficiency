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
import { listDirectoryDepartments } from '@/api/directory'
import { useI18n } from '@/i18n'
import type {
  Credential,
  DirectoryDepartment,
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
const selectedDirectorySourceID = ref<number | null>(null)
const departmentSearch = ref('')
const departmentDropdownOpen = ref(false)
const departmentOptions = ref<DirectoryDepartment[]>([])
const approverCandidates = ref<QuotaResetApproverCandidate[]>([])
const approverFilter = ref<string | null>(null)
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
const selectedApprover = computed(() => approverCandidates.value.find((candidate) => candidate.user_id === configForm.value.approver_user_id))
const filteredApproverCandidates = computed(() => {
  const query = approverFilter.value?.trim().toLowerCase()
  return query ? approverCandidates.value.filter((candidate) => approverOptionLabel(candidate).toLowerCase().includes(query)) : approverCandidates.value
})

onMounted(() => {
  void loadSettings()
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
    selectedDirectorySourceID.value = configsRes.data.data?.current_directory_source_id ?? null
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

function departmentDisplayPath(department: DirectoryDepartment) {
  return department.display_path || department.name || department.path || department.external_id
}

function approverOptionLabel(candidate: QuotaResetApproverCandidate) {
  const name = candidate.display_name || candidate.username || `User #${candidate.user_id}`
  const identity = candidate.email ? `${name} · ${candidate.email}` : name
  return candidate.representative
    ? `${identity} · ${t('quotaResetSettings.representativeMarker')}`
    : identity
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

async function loadApproverCandidates() {
  const sourceID = selectedDirectorySourceID.value
  const departmentID = configForm.value.department_external_id.trim()
  configForm.value.approver_user_id = null
  approverFilter.value = null
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

function selectApprover(candidate: QuotaResetApproverCandidate) {
  configForm.value.approver_user_id = candidate.user_id
  approverFilter.value = null
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
    approverFilter.value = null
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
    <ElAlert v-if="error" class="mt-4" type="error" :title="error" :closable="false" />
    <ElAlert v-if="message" class="mt-4" type="success" :title="message" :closable="false" />

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
	                  <ElSwitch v-model="config.enabled" />
	                </td>
	                <td class="px-3 py-2 text-right">
	                  <ElButton
	                    :data-testid="`quota-reset-config-remove-${config.id}`"
	                    link
	                    type="danger"
	                    @click="removeConfig(index)"
	                  >
	                    {{ t('settings.delete') }}
	                  </ElButton>
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
            <ElButton
              data-testid="quota-reset-department-select"
              class="mt-1 !flex !w-full !items-center !justify-between !gap-2 !px-3 !py-2 !text-left"
              aria-haspopup="listbox"
              :aria-expanded="departmentDropdownOpen ? 'true' : 'false'"
              :disabled="!selectedDirectorySourceID"
              @click="toggleDepartmentDropdown"
            >
              <span class="truncate">
                {{ selectedDepartmentLabel || t('quotaResetSettings.departmentSelectPlaceholder') }}
              </span>
              <span aria-hidden="true" class="text-xs text-gray-400">v</span>
            </ElButton>
            <div
              v-if="departmentDropdownOpen"
              class="absolute z-20 mt-1 w-full rounded-md border border-gray-200 bg-white p-2 shadow-lg"
            >
              <ElInput
                v-model="departmentSearch"
                data-testid="quota-reset-department-filter"
                :placeholder="t('quotaResetSettings.departmentSearchPlaceholder')"
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
                <ElButton
                  v-for="department in departmentOptions"
                  :key="department.id"
                  :data-testid="`quota-reset-department-option-${department.external_id}`"
                  text
                  class="!m-0 !block !h-auto !w-full !whitespace-normal !rounded !px-3 !py-2 !text-left"
                  role="option"
                  @click="selectDepartment(department)"
                >
                  <span class="block truncate font-medium text-slate-800">{{ departmentDisplayPath(department) }}</span>
                  <span class="block truncate text-xs text-slate-500">{{ department.external_id }}</span>
                </ElButton>
              </div>
              <div v-else-if="departmentSearch" class="px-3 py-3 text-sm text-gray-500">
                {{ t('quotaResetSettings.noDepartmentMatches') }}
              </div>
            </div>
          </div>
        </div>
        <div class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.approverSelect') }}</span>
          <div class="relative">
            <ElButton
              data-testid="quota-reset-approver-select"
              class="mt-1 !flex !w-full !items-center !justify-between !gap-2 !px-3 !py-2 !text-left"
              :disabled="loadingApproverCandidates || !configForm.department_external_id || approverCandidates.length === 0"
              aria-haspopup="listbox"
              :aria-expanded="approverFilter !== null ? 'true' : 'false'"
              @click="approverFilter = approverFilter === null ? '' : null"
            >
              <span class="truncate">
                {{ selectedApprover
                  ? approverOptionLabel(selectedApprover)
                  : (loadingApproverCandidates ? t('settings.loading') : t('quotaResetSettings.selectApproverPlaceholder')) }}
              </span>
              <span aria-hidden="true" class="text-xs text-gray-400">v</span>
            </ElButton>
            <div
              v-if="approverFilter !== null"
              class="absolute z-20 mt-1 w-full rounded-md border border-gray-200 bg-white p-2 shadow-lg"
            >
              <ElInput
                v-model="approverFilter"
                data-testid="quota-reset-approver-filter"
                :placeholder="t('quotaResetSettings.selectApproverPlaceholder')"
              />
              <div class="mt-1 max-h-44 overflow-y-auto" role="listbox">
                <ElButton
                  v-for="candidate in filteredApproverCandidates"
                  :key="candidate.user_id"
                  :data-testid="`quota-reset-approver-option-${candidate.user_id}`"
                  text
                  class="!m-0 !block !h-auto !w-full !whitespace-normal !rounded !px-3 !py-2 !text-left"
                  role="option"
                  :aria-selected="candidate.user_id === configForm.approver_user_id ? 'true' : 'false'"
                  @click="selectApprover(candidate)"
                >
                  {{ approverOptionLabel(candidate) }}
                </ElButton>
              </div>
            </div>
          </div>
          <div v-if="configForm.department_external_id && !loadingApproverCandidates && approverCandidates.length === 0 && unmatchedRepresentatives.length === 0" class="mt-2 text-xs text-gray-500">
            {{ t('quotaResetSettings.noApproverCandidates') }}
          </div>
          <ElAlert
            v-else-if="configForm.department_external_id && !loadingApproverCandidates && approverCandidates.length === 0 && unmatchedRepresentatives.length > 0"
            class="mt-2"
            type="warning"
            :closable="false"
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
          </ElAlert>
        </div>
        <ElButton
          data-testid="quota-reset-save-approvers"
          type="primary"
          class="self-start md:mt-6"
          :loading="savingConfigs"
          @click="saveConfigs"
        >
          {{ t('quotaResetSettings.saveApprovers') }}
        </ElButton>
      </div>
    </div>

    <div class="mt-6 border-t border-gray-200 pt-5">
      <h4 class="text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.webhook') }}</h4>
      <div class="mt-3 grid gap-3 md:grid-cols-2">
        <label class="flex items-center gap-2 text-sm text-gray-700">
          <ElSwitch
            v-model="notification.enabled"
            data-testid="quota-reset-webhook-enabled"
          />
          {{ t('settings.enabled') }}
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.channel') }}</span>
          <ElSelect
            v-model="notification.channel"
            data-testid="quota-reset-webhook-channel"
            class="mt-1 w-full"
            :teleported="false"
          >
            <ElOption value="generic_webhook" :label="t('quotaResetSettings.channelGeneric')" />
            <ElOption value="wecom_group_robot" :label="t('quotaResetSettings.channelWeCom')" />
          </ElSelect>
        </label>
        <label class="block md:col-span-2">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.webhookURL') }}</span>
          <ElInput
            v-model="notification.url"
            data-testid="quota-reset-webhook-url"
            placeholder="https://hooks.example.com/ai-efficiency"
            class="mt-1 w-full"
          />
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.authType') }}</span>
          <ElSelect v-model="notification.auth_type" class="mt-1 w-full" :teleported="false">
            <ElOption value="none" :label="t('quotaResetSettings.authNone')" />
            <ElOption value="bearer_token" :label="t('quotaResetSettings.authBearer')" />
          </ElSelect>
        </label>
        <label class="block">
          <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.credential') }}</span>
          <ElSelect v-model="notification.credential_id" class="mt-1 w-full" :placeholder="t('settings.selectApiCredential')" :disabled="notification.auth_type !== 'bearer_token'" :teleported="false" clearable>
            <ElOption v-for="credential in bearerCredentials" :key="credential.id" :value="credential.id" :label="credentialOptionLabel(credential)" />
          </ElSelect>
        </label>
      </div>
      <div class="mt-4 flex flex-wrap justify-end gap-2">
        <ElButton
          :loading="testingNotification"
          @click="testNotification"
        >
          {{ t('quotaResetSettings.testWebhook') }}
        </ElButton>
        <ElButton
          data-testid="quota-reset-save-notification"
          type="primary"
          :loading="savingNotification"
          @click="saveNotification"
        >
          {{ t('quotaResetSettings.saveWebhook') }}
        </ElButton>
      </div>
    </div>
  </section>
</template>
