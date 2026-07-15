<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getQuotaResetApprovalChains, saveQuotaResetApprovalChains } from '@/api/quotaReset'
import { listDirectoryDepartments } from '@/api/directory'
import { useI18n } from '@/i18n'
import type {
  DirectoryDepartment,
  QuotaResetApprovalChainGroupOption,
  QuotaResetApprovalChainInput,
  QuotaResetApprovalChainDepartment,
} from '@/types'

const props = defineProps<{ sourceId: number | null }>()
const { t } = useI18n()

const chains = ref<QuotaResetApprovalChainInput[]>([])
const groups = ref<QuotaResetApprovalChainGroupOption[]>([])
const selectedGroupKey = ref('')
const draftDepartments = ref<QuotaResetApprovalChainDepartment[]>([])
const dropdownOpen = ref(false)
const departmentFilter = ref('')
const departmentOptions = ref<DirectoryDepartment[]>([])
const loading = ref(false)
const searching = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
let searchSequence = 0

const selectedGroup = computed(() => groups.value.find((group) => groupKey(group.provider_id, group.group_id) === selectedGroupKey.value))
const selectedChain = computed(() => {
  const group = selectedGroup.value
  return group ? chains.value.find((chain) => chain.provider_id === group.provider_id && chain.group_id === group.group_id) : undefined
})

onMounted(() => void loadChains())

watch(() => props.sourceId, () => {
  invalidateSearch()
  dropdownOpen.value = false
  departmentFilter.value = ''
  departmentOptions.value = []
  draftDepartments.value = []
})

function groupKey(providerId: number, groupId: string) {
  return `${providerId}/${groupId}`
}

function departmentLabel(department: DirectoryDepartment) {
  return department.display_path || department.name || department.path || department.external_id
}

async function loadChains() {
  loading.value = true
  error.value = ''
  try {
    const response = await getQuotaResetApprovalChains()
    chains.value = (response.data.data?.items ?? []).map((chain) => ({
      provider_id: chain.provider_id,
      group_id: chain.group_id,
      group_name: chain.group_name,
      enabled: chain.enabled,
      departments: [...chain.departments],
    }))
    groups.value = response.data.data?.groups ?? []
  } catch {
    error.value = t('quotaResetSettings.chainLoadFailed')
  } finally {
    loading.value = false
  }
}

function selectGroup() {
  const group = selectedGroup.value
  if (!group) {
    draftDepartments.value = []
    return
  }
  const existing = chains.value.find((chain) => chain.provider_id === group.provider_id && chain.group_id === group.group_id)
  draftDepartments.value = existing ? [...existing.departments] : []
}

function invalidateSearch() {
  searchSequence += 1
  searching.value = false
}

async function searchDepartments() {
  const sourceId = props.sourceId
  const sequence = ++searchSequence
  if (!sourceId) return
  searching.value = true
  try {
    const response = await listDirectoryDepartments({ source_id: sourceId, q: departmentFilter.value.trim() })
    if (sequence !== searchSequence) return
    departmentOptions.value = response.data.data?.items ?? []
  } catch {
    if (sequence === searchSequence) departmentOptions.value = []
  } finally {
    if (sequence === searchSequence) searching.value = false
  }
}

function toggleDepartmentDropdown() {
  if (!props.sourceId || !selectedGroup.value) return
  dropdownOpen.value = !dropdownOpen.value
  if (dropdownOpen.value) void searchDepartments()
  else invalidateSearch()
}

function addDepartment(department: DirectoryDepartment) {
  if (!props.sourceId || draftDepartments.value.some((item) => item.department_external_id === department.external_id)) return
  draftDepartments.value.push({
    directory_source_id: props.sourceId,
    department_external_id: department.external_id,
    department_display_path: departmentLabel(department),
  })
  invalidateSearch()
  dropdownOpen.value = false
  departmentFilter.value = ''
  departmentOptions.value = []
}

function moveDepartment(index: number, offset: number) {
  const target = index + offset
  if (target < 0 || target >= draftDepartments.value.length) return
  const next = [...draftDepartments.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  draftDepartments.value = next
}

function removeDepartment(index: number) {
  draftDepartments.value = draftDepartments.value.filter((_, itemIndex) => itemIndex !== index)
}

async function saveChains(items: QuotaResetApprovalChainInput[]) {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const response = await saveQuotaResetApprovalChains(items)
    chains.value = (response.data.data?.items ?? []).map((chain) => ({
      provider_id: chain.provider_id,
      group_id: chain.group_id,
      group_name: chain.group_name,
      enabled: chain.enabled,
      departments: [...chain.departments],
    }))
    if (response.data.data?.groups) groups.value = response.data.data.groups
    message.value = t('quotaResetSettings.chainSaved')
  } catch {
    error.value = t('quotaResetSettings.chainSaveFailed')
  } finally {
    saving.value = false
  }
}

function saveDraft() {
  const group = selectedGroup.value
  if (!group) return
  const item: QuotaResetApprovalChainInput = {
    provider_id: group.provider_id,
    group_id: group.group_id,
    group_name: group.group_name,
    enabled: selectedChain.value?.enabled ?? true,
    departments: [...draftDepartments.value],
  }
  const remaining = chains.value.filter((chain) => chain.provider_id !== group.provider_id || chain.group_id !== group.group_id)
  void saveChains([...remaining, item])
}

function removeChain(index: number) {
  void saveChains(chains.value.filter((_, chainIndex) => chainIndex !== index))
}
</script>

<template>
  <div data-testid="quota-reset-chain-settings">
    <h4 class="text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.chains') }}</h4>
    <p class="mt-1 text-xs text-gray-500">{{ t('quotaResetSettings.chainsHelp') }}</p>
    <p v-if="loading" class="mt-3 text-sm text-gray-500">{{ t('settings.loading') }}</p>
    <p v-if="error" class="mt-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="message" class="mt-3 text-sm text-emerald-700">{{ message }}</p>

    <div v-if="chains.length" class="mt-3 divide-y divide-slate-200 border-y border-slate-200">
      <div v-for="(chain, index) in chains" :key="groupKey(chain.provider_id, chain.group_id)" class="flex items-start justify-between gap-3 py-3 text-sm">
        <div class="min-w-0">
          <div class="font-medium text-slate-900">{{ chain.group_name || chain.group_id }}</div>
          <div class="mt-1 truncate text-xs text-slate-500">
            {{ chain.departments.map((item) => item.department_display_path || item.department_external_id).join(' -> ') || t('quotaResetSettings.adminFallback') }}
          </div>
        </div>
        <button type="button" class="text-sm font-medium text-red-700" :disabled="saving" @click="removeChain(index)">
          {{ t('settings.delete') }}
        </button>
      </div>
    </div>

    <div class="mt-4 grid gap-3 md:grid-cols-2">
      <label class="block">
        <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.subscriptionGroup') }}</span>
        <select v-model="selectedGroupKey" data-testid="quota-reset-chain-group" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm" @change="selectGroup">
          <option value="">{{ t('quotaResetSettings.selectSubscriptionGroup') }}</option>
          <option v-for="group in groups" :key="groupKey(group.provider_id, group.group_id)" :value="groupKey(group.provider_id, group.group_id)">
            {{ group.group_name }} · {{ group.provider_name }}
          </option>
        </select>
      </label>

      <div class="relative">
        <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.chainDepartment') }}</span>
        <button type="button" data-testid="quota-reset-chain-department-select" class="mt-1 flex w-full items-center justify-between rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm disabled:opacity-60" :disabled="!sourceId || !selectedGroup" @click="toggleDepartmentDropdown">
          <span>{{ t('quotaResetSettings.addDepartment') }}</span><span aria-hidden="true">v</span>
        </button>
        <div v-if="dropdownOpen" class="absolute z-20 mt-1 w-full border border-slate-200 bg-white p-2 shadow-lg">
          <input v-model="departmentFilter" data-testid="quota-reset-chain-department-filter" class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm" :placeholder="t('quotaResetSettings.departmentSearchPlaceholder')" @input="searchDepartments" />
          <p v-if="searching" class="px-3 py-2 text-sm text-gray-500">{{ t('settings.loading') }}</p>
          <div v-else class="mt-1 max-h-48 overflow-y-auto">
            <button v-for="department in departmentOptions" :key="department.external_id" type="button" :data-testid="`quota-reset-chain-department-option-${department.external_id}`" class="block w-full px-3 py-2 text-left text-sm hover:bg-slate-50" @click="addDepartment(department)">
              {{ departmentLabel(department) }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <ol v-if="draftDepartments.length" class="mt-3 divide-y divide-slate-200 border-y border-slate-200">
      <li v-for="(department, index) in draftDepartments" :key="department.department_external_id" class="flex items-center gap-2 py-2 text-sm">
        <span class="w-6 text-slate-400">{{ index + 1 }}</span>
        <span class="min-w-0 flex-1 truncate">{{ department.department_display_path || department.department_external_id }}</span>
        <button type="button" :data-testid="`quota-reset-chain-move-up-${index}`" class="h-8 w-8 text-slate-600 disabled:opacity-30" :disabled="index === 0" :aria-label="t('quotaResetSettings.moveUp')" :title="t('quotaResetSettings.moveUp')" @click="moveDepartment(index, -1)">↑</button>
        <button type="button" class="h-8 w-8 text-slate-600 disabled:opacity-30" :disabled="index === draftDepartments.length - 1" :aria-label="t('quotaResetSettings.moveDown')" :title="t('quotaResetSettings.moveDown')" @click="moveDepartment(index, 1)">↓</button>
        <button type="button" class="h-8 w-8 text-red-700" :aria-label="t('settings.delete')" :title="t('settings.delete')" @click="removeDepartment(index)">×</button>
      </li>
    </ol>

    <div class="mt-4 flex justify-end">
      <button type="button" data-testid="quota-reset-chain-save" class="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-60" :disabled="saving || !selectedGroup" @click="saveDraft">
        {{ saving ? t('settings.saving') : t('quotaResetSettings.saveChain') }}
      </button>
    </div>
  </div>
</template>
