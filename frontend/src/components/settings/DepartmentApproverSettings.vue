<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RefreshCw } from '@lucide/vue'
import {
  getQuotaResetApproverConfigs,
  listQuotaResetApproverCandidates,
  saveQuotaResetApproverConfigs,
} from '@/api/quotaReset'
import { listDirectoryDepartments, listDirectorySources } from '@/api/directory'
import { useI18n } from '@/i18n'
import type {
  DirectoryDepartment,
  DirectorySource,
  QuotaResetApproverCandidate,
  QuotaResetApproverConfig,
  QuotaResetApproverConfigInput,
} from '@/types'

const emit = defineEmits<{
  saved: []
}>()

const { t } = useI18n()
const configs = ref<QuotaResetApproverConfig[]>([])
const directorySources = ref<DirectorySource[]>([])
const selectedDirectorySourceID = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const configsAuthoritative = ref(false)
const error = ref('')
const directoryError = ref('')
const candidateError = ref('')
const message = ref('')

const departmentDropdownOpen = ref(false)
const departmentSearch = ref('')
const departmentOptions = ref<DirectoryDepartment[]>([])
const departmentLoading = ref(false)
let departmentRequestSequence = 0

const approverDropdownOpen = ref(false)
const candidateSearch = ref('')
const candidates = ref<QuotaResetApproverCandidate[]>([])
const candidateLoading = ref(false)
const candidatePage = ref(0)
const candidatePageSize = ref(20)
const candidateTotal = ref(0)
let candidateRequestSequence = 0

const form = ref({
  department_external_id: '',
  department_display_path: '',
  approver_user_id: null as number | null,
  approver_label: '',
  enabled: true,
})

const selectedDepartmentLabel = computed(() => (
  form.value.department_display_path || form.value.department_external_id
))
const selectedDirectorySource = computed(() => (
  directorySources.value.find(source => source.id === selectedDirectorySourceID.value)
))
const currentSourceError = computed(() => (
  configsAuthoritative.value && selectedDirectorySourceID.value === null
    ? t('quotaResetSettings.directoryUnavailable')
    : ''
))
const canLoadMoreCandidates = computed(() => candidateTotal.value > candidates.value.length)
const visibleError = computed(() => (
  error.value || candidateError.value || directoryError.value || currentSourceError.value
))

onMounted(() => {
  void loadConfigs()
  void loadDirectorySources()
})

function errorMessage(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

async function loadConfigs() {
  if (loading.value || saving.value) return
  loading.value = true
  configsAuthoritative.value = false
  error.value = ''
  message.value = ''
  try {
    const response = await getQuotaResetApproverConfigs()
    const data = response.data.data
    const items = data?.items
    const sourceID = data?.directory_source_id
    if (
      !Array.isArray(items)
      || (sourceID !== null && (typeof sourceID !== 'number' || !Number.isInteger(sourceID) || sourceID <= 0))
    ) {
      throw new Error(t('quotaResetSettings.approverLoadFailed'))
    }
    configs.value = items
    selectedDirectorySourceID.value = sourceID
    configsAuthoritative.value = true
  } catch (err) {
    error.value = errorMessage(err, t('quotaResetSettings.approverLoadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadDirectorySources() {
  directoryError.value = ''
  try {
    const response = await listDirectorySources()
    directorySources.value = response.data.data?.items ?? []
  } catch (err) {
    directorySources.value = []
    directoryError.value = errorMessage(err, t('quotaResetSettings.directoryLoadFailed'))
  }
}

function departmentDisplayPath(department: DirectoryDepartment) {
  return department.display_path || department.name || department.path || department.external_id
}

function candidateDisplayName(candidate: QuotaResetApproverCandidate) {
  return candidate.display_name || candidate.username || candidate.email
}

function closeDepartmentDropdown() {
  departmentRequestSequence += 1
  departmentLoading.value = false
  departmentDropdownOpen.value = false
}

function closeApproverDropdown() {
  candidateRequestSequence += 1
  candidateLoading.value = false
  approverDropdownOpen.value = false
}

function resetCandidateResults() {
  candidates.value = []
  candidatePage.value = 0
  candidatePageSize.value = 20
  candidateTotal.value = 0
}

function mergeUniqueCandidates(
  existing: QuotaResetApproverCandidate[],
  incoming: QuotaResetApproverCandidate[],
) {
  const merged = new Map<number, QuotaResetApproverCandidate>()
  for (const candidate of [...existing, ...incoming]) {
    if (!merged.has(candidate.user_id)) {
      merged.set(candidate.user_id, candidate)
    }
  }
  return [...merged.values()]
}

async function searchDepartments() {
  const sourceID = selectedDirectorySourceID.value
  const sequence = ++departmentRequestSequence
  if (sourceID === null) {
    departmentOptions.value = []
    departmentLoading.value = false
    return
  }

  departmentLoading.value = true
  try {
    const response = await listDirectoryDepartments({
      source_id: sourceID,
      q: departmentSearch.value.trim(),
    })
    if (sequence !== departmentRequestSequence) return
    departmentOptions.value = response.data.data?.items ?? []
  } catch {
    if (sequence !== departmentRequestSequence) return
    departmentOptions.value = []
  } finally {
    if (sequence === departmentRequestSequence) {
      departmentLoading.value = false
    }
  }
}

function toggleDepartmentDropdown() {
  if (saving.value || selectedDirectorySourceID.value === null) return
  if (departmentDropdownOpen.value) {
    closeDepartmentDropdown()
    return
  }
  closeApproverDropdown()
  departmentDropdownOpen.value = true
  void searchDepartments()
}

function selectDepartment(department: DirectoryDepartment) {
  closeDepartmentDropdown()
  form.value.department_external_id = department.external_id
  form.value.department_display_path = departmentDisplayPath(department)
  form.value.approver_user_id = null
  form.value.approver_label = ''
  departmentSearch.value = ''
  departmentOptions.value = []
  candidateSearch.value = ''
  resetCandidateResults()
  candidateError.value = ''
}

function resetSelectedDepartment() {
  closeDepartmentDropdown()
  closeApproverDropdown()
  form.value = {
    department_external_id: '',
    department_display_path: '',
    approver_user_id: null,
    approver_label: '',
    enabled: true,
  }
  departmentSearch.value = ''
  departmentOptions.value = []
  candidateSearch.value = ''
  resetCandidateResults()
  candidateError.value = ''
}

async function loadCandidatePage(page: number, append: boolean) {
  const sourceID = selectedDirectorySourceID.value
  const sequence = ++candidateRequestSequence
  if (!append) {
    resetCandidateResults()
  }
  if (sourceID === null) {
    candidateLoading.value = false
    return
  }

  candidateLoading.value = true
  candidateError.value = ''
  try {
    const response = await listQuotaResetApproverCandidates({
      source_id: sourceID,
      q: candidateSearch.value.trim(),
      page,
      page_size: candidatePageSize.value,
    })
    if (sequence !== candidateRequestSequence) return
    const data = response.data.data
    const pageItems = data?.items ?? []
    candidates.value = mergeUniqueCandidates(append ? candidates.value : [], pageItems)
    candidatePage.value = data?.page ?? page
    candidatePageSize.value = data?.page_size ?? candidatePageSize.value
    candidateTotal.value = data?.total ?? candidates.value.length
    candidateError.value = ''
  } catch (err) {
    if (sequence !== candidateRequestSequence) return
    candidateError.value = errorMessage(err, t('quotaResetSettings.approverSearchFailed'))
  } finally {
    if (sequence === candidateRequestSequence) {
      candidateLoading.value = false
    }
  }
}

function searchCandidates() {
  return loadCandidatePage(1, false)
}

function loadMoreCandidates() {
  if (candidateLoading.value || !canLoadMoreCandidates.value) return
  return loadCandidatePage(candidatePage.value + 1, true)
}

function toggleApproverDropdown() {
  if (saving.value || !form.value.department_external_id || selectedDirectorySourceID.value === null) return
  if (approverDropdownOpen.value) {
    closeApproverDropdown()
    return
  }
  closeDepartmentDropdown()
  approverDropdownOpen.value = true
  void searchCandidates()
}

function selectApprover(candidate: QuotaResetApproverCandidate) {
  form.value.approver_user_id = candidate.user_id
  form.value.approver_label = candidateDisplayName(candidate)
  candidateSearch.value = ''
  resetCandidateResults()
  closeApproverDropdown()
}

function removeConfig(index: number) {
  configs.value = configs.value.filter((_, currentIndex) => currentIndex !== index)
}

function rowsForSave(): QuotaResetApproverConfigInput[] {
  const rows = configs.value
    .map(config => ({
      department_external_id: config.department_external_id.trim(),
      department_display_path: config.department_display_path.trim(),
      approver_user_id: Number(config.approver_user_id),
      enabled: config.enabled,
    }))
    .filter(config => (
      Boolean(config.department_external_id)
      && Number.isInteger(config.approver_user_id)
      && config.approver_user_id > 0
    ))

  const departmentID = form.value.department_external_id.trim()
  const approverID = Number(form.value.approver_user_id)
  if (!departmentID || !Number.isInteger(approverID) || approverID <= 0) {
    return rows
  }

  return [
    ...rows,
    {
      department_external_id: departmentID,
      department_display_path: form.value.department_display_path.trim() || departmentID,
      approver_user_id: approverID,
      enabled: form.value.enabled,
    },
  ]
}

async function saveConfigs() {
  if (!configsAuthoritative.value || loading.value || saving.value) return
  saving.value = true
  message.value = ''
  error.value = ''
  closeDepartmentDropdown()
  closeApproverDropdown()
  candidateSearch.value = ''
  resetCandidateResults()
  candidateError.value = ''
  try {
    const response = await saveQuotaResetApproverConfigs(rowsForSave(), 'replace_all')
    configs.value = response.data.data?.items ?? []
    selectedDirectorySourceID.value = response.data.data?.directory_source_id ?? null
    configsAuthoritative.value = true
    resetSelectedDepartment()
    message.value = t('quotaResetSettings.configSaved')
    emit('saved')
  } catch (err) {
    error.value = errorMessage(err, t('quotaResetSettings.configSaveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section
    class="rounded-md border border-gray-200 bg-white p-4 sm:p-5"
    data-testid="department-approver-settings"
  >
    <div class="flex min-w-0 items-center justify-between gap-3">
      <h4 class="min-w-0 text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.approvers') }}</h4>
      <button
        type="button"
        data-testid="quota-reset-reload-approvers"
        class="flex h-9 w-9 shrink-0 items-center justify-center rounded border border-gray-300 text-gray-600 hover:bg-gray-50 disabled:opacity-40"
        :aria-label="t('quotaResetSettings.reloadApprovers')"
        :title="t('quotaResetSettings.reloadApprovers')"
        :disabled="loading || saving"
        @click="loadConfigs"
      >
        <RefreshCw aria-hidden="true" class="h-4 w-4" />
      </button>
    </div>

    <div v-if="visibleError" class="mt-3 break-words rounded-md bg-red-50 p-3 text-sm text-red-700">
      {{ visibleError }}
    </div>
    <div v-if="message" class="mt-3 rounded-md bg-emerald-50 p-3 text-sm text-emerald-700">
      {{ message }}
    </div>
    <div v-if="loading" class="mt-3 text-sm text-gray-500">{{ t('settings.loading') }}</div>

    <div v-else class="mt-3 overflow-x-auto rounded-md border border-gray-200">
      <table class="min-w-full divide-y divide-gray-200 text-sm">
        <thead class="bg-gray-50 text-left text-xs font-medium uppercase text-gray-500">
          <tr>
            <th class="px-3 py-2">{{ t('quotaResetSettings.department') }}</th>
            <th class="px-3 py-2">{{ t('quotaResetSettings.approver') }}</th>
            <th class="px-3 py-2">{{ t('settings.enabled') }}</th>
            <th class="px-3 py-2 text-right">{{ t('settings.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white">
          <tr
            v-for="(config, index) in configs"
            :key="config.id || `${config.department_external_id}-${config.approver_user_id}`"
            :data-testid="`quota-reset-config-row-${config.id}`"
          >
            <td class="min-w-52 max-w-80 px-3 py-2">
              <div class="break-words font-medium text-slate-800">
                {{ config.department_display_path || t('quotaResetSettings.departmentUnavailable') }}
              </div>
            </td>
            <td class="min-w-52 max-w-80 px-3 py-2">
              <div class="break-words font-medium text-slate-800">
                {{ config.approver_username || config.approver_email || t('quotaResetSettings.approverUnavailable') }}
              </div>
              <div v-if="config.approver_email" class="mt-0.5 break-all text-xs text-slate-500">
                {{ config.approver_email }}
              </div>
            </td>
            <td class="px-3 py-2">
              <input
                v-model="config.enabled"
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-indigo-600"
                :aria-label="t('quotaResetSettings.approverEnabled', { department: config.department_display_path })"
              />
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

    <div class="mt-4 grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-start">
      <div class="min-w-0">
        <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.departmentSearch') }}</span>
        <div class="relative mt-1">
          <button
            type="button"
            data-testid="quota-reset-department-select"
            class="flex min-h-10 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm text-gray-900 hover:bg-gray-50 disabled:opacity-60"
            aria-haspopup="listbox"
            :aria-expanded="departmentDropdownOpen ? 'true' : 'false'"
            :disabled="saving || selectedDirectorySourceID === null"
            @click="toggleDepartmentDropdown"
          >
            <span class="min-w-0 truncate">
              {{ selectedDepartmentLabel || t('quotaResetSettings.departmentSelectPlaceholder') }}
            </span>
            <span aria-hidden="true" class="shrink-0 text-xs text-gray-400">▼</span>
          </button>
          <div
            v-if="departmentDropdownOpen"
            class="absolute z-30 mt-1 w-full min-w-0 rounded-md border border-gray-200 bg-white p-2 shadow-lg"
          >
            <input
              v-model="departmentSearch"
              data-testid="quota-reset-department-filter"
              type="search"
              :placeholder="t('quotaResetSettings.departmentSearchPlaceholder')"
              :aria-label="t('quotaResetSettings.departmentSearchPlaceholder')"
              class="w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
              @input="searchDepartments"
            />
            <div v-if="departmentLoading" class="px-3 py-3 text-sm text-gray-500">
              {{ t('settings.loading') }}
            </div>
            <div v-else class="mt-2 max-h-52 overflow-y-auto" role="listbox">
              <button
                v-for="department in departmentOptions"
                :key="department.id"
                type="button"
                :data-testid="`quota-reset-department-option-${department.external_id}`"
                class="block w-full min-w-0 rounded px-3 py-2 text-left text-sm hover:bg-slate-50"
                role="option"
                @click="selectDepartment(department)"
              >
                <span class="block break-words font-medium text-slate-800">{{ departmentDisplayPath(department) }}</span>
              </button>
              <div v-if="departmentOptions.length === 0" class="px-3 py-3 text-sm text-gray-500">
                {{ t('quotaResetSettings.noDepartmentMatches') }}
              </div>
            </div>
          </div>
        </div>
        <div v-if="directorySources.length > 1 && selectedDirectorySource" class="mt-2 block">
          <span class="text-xs font-medium text-gray-500">{{ t('quotaResetSettings.directorySource') }}</span>
          <div
            data-testid="quota-reset-current-directory-source"
            class="mt-1 break-words text-sm text-gray-700"
          >
            {{ selectedDirectorySource.name }}
          </div>
        </div>
      </div>

      <div class="min-w-0">
        <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.approverSelect') }}</span>
        <div class="relative mt-1">
          <button
            type="button"
            data-testid="quota-reset-approver-select"
            class="flex min-h-10 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm text-gray-900 hover:bg-gray-50 disabled:opacity-60"
            aria-haspopup="listbox"
            :aria-expanded="approverDropdownOpen ? 'true' : 'false'"
            :disabled="saving || !form.department_external_id || selectedDirectorySourceID === null"
            @click="toggleApproverDropdown"
          >
            <span class="min-w-0 truncate">
              {{ form.approver_label || t('quotaResetSettings.selectApproverPlaceholder') }}
            </span>
            <span aria-hidden="true" class="shrink-0 text-xs text-gray-400">▼</span>
          </button>
          <div
            v-if="approverDropdownOpen"
            class="absolute z-30 mt-1 w-full min-w-0 rounded-md border border-gray-200 bg-white p-2 shadow-lg"
          >
            <input
              v-model="candidateSearch"
              data-testid="quota-reset-approver-filter"
              type="search"
              :placeholder="t('quotaResetSettings.approverSearchPlaceholder')"
              :aria-label="t('quotaResetSettings.approverSearchPlaceholder')"
              class="w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
              @input="searchCandidates"
            />
            <div
              v-if="candidateLoading && candidates.length === 0"
              data-testid="quota-reset-approver-loading"
              class="px-3 py-3 text-sm text-gray-500"
            >
              {{ t('settings.loading') }}
            </div>
            <div v-else class="mt-2 max-h-72 overflow-y-auto">
              <div role="listbox">
                <button
                  v-for="candidate in candidates"
                  :key="candidate.user_id"
                  type="button"
                  :data-testid="`quota-reset-approver-option-${candidate.user_id}`"
                  class="block w-full min-w-0 rounded px-3 py-2 text-left hover:bg-slate-50"
                  role="option"
                  @click="selectApprover(candidate)"
                >
                  <span class="block break-words text-sm font-medium text-slate-800">
                    {{ candidateDisplayName(candidate) }}
                  </span>
                  <span class="block break-all text-xs text-slate-500">{{ candidate.email }}</span>
                  <span v-if="candidate.department_paths.length" class="mt-1 block break-words text-xs text-slate-500">
                    {{ candidate.department_paths.join(' · ') }}
                  </span>
                  <span
                    class="mt-1 block text-xs font-medium"
                    :class="candidate.wecom_mention_available ? 'text-emerald-700' : 'text-amber-700'"
                  >
                    {{ candidate.wecom_mention_available
                      ? t('quotaResetSettings.weComMentionAvailable')
                      : t('quotaResetSettings.weComMentionUnavailable') }}
                  </span>
                </button>
              </div>
              <div v-if="candidates.length === 0" class="px-3 py-3 text-sm text-gray-500">
                {{ t('quotaResetSettings.noApproverCandidates') }}
              </div>
              <button
                v-if="canLoadMoreCandidates"
                type="button"
                data-testid="quota-reset-approver-load-more"
                class="mt-2 min-h-9 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-60"
                :aria-label="t('quotaResetSettings.loadMoreApprovers')"
                :disabled="candidateLoading"
                @click="loadMoreCandidates"
              >
                {{ candidateLoading ? t('settings.loading') : t('quotaResetSettings.loadMoreApprovers') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <button
        type="button"
        data-testid="quota-reset-save-approvers"
        class="min-h-10 rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-60 lg:mt-6"
        :disabled="saving || loading || !configsAuthoritative"
        @click="saveConfigs"
      >
        {{ saving ? t('settings.saving') : t('quotaResetSettings.saveApprovers') }}
      </button>
    </div>
  </section>
</template>
