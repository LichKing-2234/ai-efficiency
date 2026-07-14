<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowDown, ArrowUp, RefreshCw, Trash2 } from '@lucide/vue'
import {
  getQuotaResetApprovalChainOptions,
  getQuotaResetApprovalChains,
  saveQuotaResetApprovalChains,
} from '@/api/quotaReset'
import { useI18n } from '@/i18n'
import type {
  QuotaResetApprovalChainDepartmentOption,
  QuotaResetApprovalChainGroupOption,
  QuotaResetApprovalChainInput,
  QuotaResetApprovalChainNodeInput,
} from '@/types'

const props = defineProps<{
  approverRevision: number
}>()

const { t } = useI18n()
const groups = ref<QuotaResetApprovalChainGroupOption[]>([])
const departments = ref<QuotaResetApprovalChainDepartmentOption[]>([])
const chains = ref<QuotaResetApprovalChainInput[]>([])
const selectedGroupKey = ref('')
const loading = ref(false)
const saving = ref(false)
const chainsAuthoritative = ref(false)
const error = ref('')
const message = ref('')
let loadSequence = 0

const groupDropdownOpen = ref(false)
const groupSearch = ref('')
const departmentDropdownOpen = ref(false)
const departmentSearch = ref('')

const groupChoices = computed(() => {
  const result = [...groups.value]
  for (const chain of chains.value) {
    if (!result.some(group => group.provider_id === chain.provider_id && group.group_id === chain.group_id)) {
      result.push({
        provider_id: chain.provider_id,
        group_id: chain.group_id,
        group_name: chain.group_name,
        platform: '',
      })
    }
  }
  return result
})

const filteredGroups = computed(() => {
  const query = groupSearch.value.trim().toLowerCase()
  if (!query) return groupChoices.value
  return groupChoices.value.filter(group => (
    group.group_name.toLowerCase().includes(query)
    || group.group_id.toLowerCase().includes(query)
  ))
})

const filteredDepartments = computed(() => {
  const query = departmentSearch.value.trim().toLowerCase()
  if (!query) return departments.value
  return departments.value.filter(department => (
    department.department_display_path.toLowerCase().includes(query)
    || department.department_external_id.toLowerCase().includes(query)
  ))
})

const selectedChain = computed(() => {
  const separator = selectedGroupKey.value.indexOf(':')
  if (separator < 1) return undefined
  const providerID = Number(selectedGroupKey.value.slice(0, separator))
  const groupID = selectedGroupKey.value.slice(separator + 1)
  return chains.value.find(chain => (
    chain.provider_id === providerID && chain.group_id === groupID
  ))
})

const selectedGroupLabel = computed(() => selectedChain.value?.group_name || '')
const selectedGroupIsCurrent = computed(() => {
  if (!selectedChain.value) return true
  return groups.value.some(group => (
    group.provider_id === selectedChain.value?.provider_id
    && group.group_id === selectedChain.value?.group_id
  ))
})

watch(
  () => props.approverRevision,
  () => {
    void loadChains()
  },
  { immediate: true },
)

function errorMessage(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

function groupKey(providerID: number, groupID: string) {
  return `${providerID}:${groupID}`
}

function toInputChains(items: Array<QuotaResetApprovalChainInput>) {
  return items.map(chain => ({
    provider_id: chain.provider_id,
    group_id: chain.group_id,
    group_name: chain.group_name,
    enabled: chain.enabled,
    nodes: chain.nodes.map(node => ({
      directory_source_id: node.directory_source_id,
      department_external_id: node.department_external_id,
      department_display_path: node.department_display_path,
    })),
  }))
}

async function loadChains() {
  const sequence = ++loadSequence
  loading.value = true
  chainsAuthoritative.value = false
  error.value = ''
  message.value = ''
  try {
    const [optionsResponse, chainsResponse] = await Promise.all([
      getQuotaResetApprovalChainOptions(),
      getQuotaResetApprovalChains(),
    ])
    if (sequence !== loadSequence) return

    const options = optionsResponse.data.data
    const items = chainsResponse.data.data?.items
    if (!options || !Array.isArray(options.groups) || !Array.isArray(options.departments) || !Array.isArray(items)) {
      throw new Error(t('quotaResetSettings.chainLoadFailed'))
    }

    groups.value = options.groups
    departments.value = options.departments
    chains.value = toInputChains(items)

    if (selectedGroupKey.value && !selectedChain.value) {
      selectedGroupKey.value = ''
    }
    chainsAuthoritative.value = true
  } catch (err) {
    if (sequence !== loadSequence) return
    error.value = errorMessage(err, t('quotaResetSettings.chainLoadFailed'))
  } finally {
    if (sequence === loadSequence) {
      loading.value = false
    }
  }
}

function toggleGroupDropdown() {
  groupDropdownOpen.value = !groupDropdownOpen.value
  if (groupDropdownOpen.value) {
    departmentDropdownOpen.value = false
  } else {
    groupSearch.value = ''
  }
}

function selectGroup(group: QuotaResetApprovalChainGroupOption) {
  let chain = chains.value.find(item => (
    item.provider_id === group.provider_id && item.group_id === group.group_id
  ))
  if (!chain) {
    chain = {
      provider_id: group.provider_id,
      group_id: group.group_id,
      group_name: group.group_name,
      enabled: true,
      nodes: [],
    }
    chains.value = [...chains.value, chain]
  }
  selectedGroupKey.value = groupKey(group.provider_id, group.group_id)
  groupDropdownOpen.value = false
  groupSearch.value = ''
  departmentDropdownOpen.value = false
  departmentSearch.value = ''
  error.value = ''
  message.value = ''
}

function toggleDepartmentDropdown() {
  if (!selectedChain.value) return
  departmentDropdownOpen.value = !departmentDropdownOpen.value
  if (departmentDropdownOpen.value) {
    groupDropdownOpen.value = false
  } else {
    departmentSearch.value = ''
  }
}

function isDepartmentConfigured(department: QuotaResetApprovalChainDepartmentOption) {
  return selectedChain.value?.nodes.some(node => (
    node.department_external_id === department.department_external_id
  )) ?? false
}

function addDepartment(department: QuotaResetApprovalChainDepartmentOption) {
  const chain = selectedChain.value
  if (!chain || isDepartmentConfigured(department)) return
  chain.nodes = [
    ...chain.nodes,
    {
      directory_source_id: department.directory_source_id,
      department_external_id: department.department_external_id,
      department_display_path: department.department_display_path,
    },
  ]
  departmentDropdownOpen.value = false
  departmentSearch.value = ''
}

function moveNode(index: number, offset: -1 | 1) {
  const nodes = selectedChain.value?.nodes
  if (!nodes) return
  const target = index + offset
  if (target < 0 || target >= nodes.length) return
  const next = [...nodes]
  ;[next[index], next[target]] = [next[target], next[index]]
  selectedChain.value!.nodes = next
}

function removeNode(index: number) {
  const chain = selectedChain.value
  if (!chain) return
  chain.nodes = chain.nodes.filter((_, nodeIndex) => nodeIndex !== index)
}

function removeChain() {
  const chain = selectedChain.value
  if (!chain || saving.value) return
  chains.value = chains.value.filter(item => (
    item.provider_id !== chain.provider_id || item.group_id !== chain.group_id
  ))
  selectedGroupKey.value = ''
  groupDropdownOpen.value = false
  groupSearch.value = ''
  departmentDropdownOpen.value = false
  departmentSearch.value = ''
  error.value = ''
  message.value = ''
}

function nodeWarning(node: QuotaResetApprovalChainNodeInput) {
  const sameDepartment = departments.value.filter(department => (
    department.department_external_id === node.department_external_id
  ))
  if (sameDepartment.length === 0) {
    return t('quotaResetSettings.staleDepartment')
  }
  const current = sameDepartment.find(department => (
    department.directory_source_id === node.directory_source_id
  ))
  if (!current) {
    return t('quotaResetSettings.staleSource')
  }
  if (current.approver_count < 1) {
    return t('quotaResetSettings.staleApprover')
  }
  return ''
}

async function saveChains() {
  if (!chainsAuthoritative.value || loading.value || saving.value) return
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const payload = toInputChains(chains.value)
    const response = await saveQuotaResetApprovalChains(payload)
    chains.value = toInputChains(response.data.data?.items ?? payload)
    chainsAuthoritative.value = true
    message.value = t('quotaResetSettings.chainsSaved')
  } catch (err) {
    error.value = errorMessage(err, t('quotaResetSettings.chainsSaveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section
    class="rounded-md border border-gray-200 bg-white p-4 sm:p-5"
    data-testid="subscription-group-approval-chains"
  >
    <div class="flex min-w-0 items-center justify-between gap-3">
      <h4 class="min-w-0 text-sm font-semibold text-gray-900">{{ t('quotaResetSettings.chains') }}</h4>
      <button
        type="button"
        data-testid="quota-reset-reload-chains"
        class="flex h-9 w-9 shrink-0 items-center justify-center rounded border border-gray-300 text-gray-600 hover:bg-gray-50 disabled:opacity-40"
        :aria-label="t('quotaResetSettings.reloadChains')"
        :title="t('quotaResetSettings.reloadChains')"
        :disabled="loading || saving"
        @click="loadChains"
      >
        <RefreshCw aria-hidden="true" class="h-4 w-4" />
      </button>
    </div>

    <div v-if="error" class="mt-3 break-words rounded-md bg-red-50 p-3 text-sm text-red-700">
      {{ error }}
    </div>
    <div v-if="message" class="mt-3 rounded-md bg-emerald-50 p-3 text-sm text-emerald-700">
      {{ message }}
    </div>
    <div
      v-if="loading"
      data-testid="quota-reset-chain-loading"
      class="mt-3 text-sm text-gray-500"
    >
      {{ t('settings.loading') }}
    </div>

    <div v-else class="mt-3 min-w-0 space-y-4">
      <div class="max-w-xl min-w-0">
        <span class="text-sm font-medium text-gray-700">{{ t('quotaResetSettings.chainGroup') }}</span>
        <div class="relative mt-1">
          <button
            type="button"
            data-testid="quota-reset-chain-group-select"
            class="flex min-h-10 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm text-gray-900 hover:bg-gray-50"
            aria-haspopup="listbox"
            :aria-expanded="groupDropdownOpen ? 'true' : 'false'"
            @click="toggleGroupDropdown"
          >
            <span class="min-w-0 truncate">
              {{ selectedGroupLabel || t('quotaResetSettings.chainGroupPlaceholder') }}
            </span>
            <span aria-hidden="true" class="shrink-0 text-xs text-gray-400">▼</span>
          </button>
          <div
            v-if="groupDropdownOpen"
            class="absolute z-20 mt-1 w-full rounded-md border border-gray-200 bg-white p-2 shadow-lg"
          >
            <input
              v-model="groupSearch"
              data-testid="quota-reset-chain-group-filter"
              type="search"
              :placeholder="t('quotaResetSettings.chainGroupSearch')"
              :aria-label="t('quotaResetSettings.chainGroupSearch')"
              class="w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
            />
            <div class="mt-2 max-h-52 overflow-y-auto" role="listbox">
              <button
                v-for="group in filteredGroups"
                :key="groupKey(group.provider_id, group.group_id)"
                type="button"
                :data-testid="`quota-reset-chain-group-option-${group.provider_id}-${group.group_id}`"
                class="block w-full min-w-0 rounded px-3 py-2 text-left hover:bg-slate-50"
                role="option"
                @click="selectGroup(group)"
              >
                <span class="block break-words text-sm font-medium text-slate-800">{{ group.group_name }}</span>
                <span v-if="group.platform" class="block break-all text-xs text-slate-500">{{ group.platform }}</span>
              </button>
              <div v-if="filteredGroups.length === 0" class="px-3 py-3 text-sm text-gray-500">
                {{ t('quotaResetSettings.noChainGroups') }}
              </div>
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="selectedChain && !selectedGroupIsCurrent"
        class="flex min-w-0 items-center justify-between gap-3 rounded-md bg-amber-50 p-3 text-sm text-amber-800"
      >
        <span class="min-w-0 break-words">{{ t('quotaResetSettings.staleGroup') }}</span>
        <button
          type="button"
          data-testid="quota-reset-remove-chain"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded border border-amber-300 text-amber-800 hover:bg-amber-100 disabled:opacity-40"
          :aria-label="t('quotaResetSettings.removeChain', { group: selectedChain.group_name })"
          :title="t('quotaResetSettings.removeChain', { group: selectedChain.group_name })"
          :disabled="saving"
          @click="removeChain"
        >
          <Trash2 aria-hidden="true" class="h-4 w-4" />
        </button>
      </div>

      <div v-if="selectedChain" class="min-w-0 space-y-3">
        <div class="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <label class="flex items-center gap-2 text-sm text-gray-700">
            <input
              v-model="selectedChain.enabled"
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-indigo-600"
            />
            {{ t('settings.enabled') }}
          </label>

          <div class="relative w-full min-w-0 sm:max-w-sm">
            <button
              type="button"
              data-testid="quota-reset-chain-department-select"
              class="flex min-h-10 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm text-gray-900 hover:bg-gray-50"
              aria-haspopup="listbox"
              :aria-expanded="departmentDropdownOpen ? 'true' : 'false'"
              @click="toggleDepartmentDropdown"
            >
              <span class="min-w-0 truncate">{{ t('quotaResetSettings.addNode') }}</span>
              <span aria-hidden="true" class="shrink-0 text-xs text-gray-400">▼</span>
            </button>
            <div
              v-if="departmentDropdownOpen"
              class="absolute right-0 z-20 mt-1 w-full rounded-md border border-gray-200 bg-white p-2 shadow-lg"
            >
              <input
                v-model="departmentSearch"
                data-testid="quota-reset-chain-department-filter"
                type="search"
                :placeholder="t('quotaResetSettings.departmentSearchPlaceholder')"
                :aria-label="t('quotaResetSettings.departmentSearchPlaceholder')"
                class="w-full min-w-0 rounded-md border border-gray-300 px-3 py-2 text-sm"
              />
              <div class="mt-2 max-h-52 overflow-y-auto" role="listbox">
                <button
                  v-for="department in filteredDepartments"
                  :key="`${department.directory_source_id}:${department.department_external_id}`"
                  type="button"
                  :data-testid="`quota-reset-chain-department-option-${department.department_external_id}`"
                  class="block w-full min-w-0 rounded px-3 py-2 text-left hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-gray-50 disabled:text-gray-400"
                  role="option"
                  :disabled="isDepartmentConfigured(department)"
                  @click="addDepartment(department)"
                >
                  <span class="block break-words text-sm font-medium">{{ department.department_display_path }}</span>
                  <span class="block text-xs">{{ t('quotaResetSettings.approverCount', { count: department.approver_count }) }}</span>
                </button>
                <div v-if="filteredDepartments.length === 0" class="px-3 py-3 text-sm text-gray-500">
                  {{ t('quotaResetSettings.noDepartmentMatches') }}
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="selectedChain.nodes.length" class="divide-y divide-gray-100 rounded-md border border-gray-200">
          <div
            v-for="(node, index) in selectedChain.nodes"
            :key="`${node.directory_source_id}:${node.department_external_id}`"
            :data-testid="`quota-reset-chain-node-${node.department_external_id}`"
            class="flex min-w-0 items-center gap-3 px-3 py-2"
          >
            <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-gray-100 text-xs font-semibold text-gray-600">
              {{ index + 1 }}
            </span>
            <div class="min-w-0 flex-1">
              <div class="break-words text-sm font-medium text-slate-800">{{ node.department_display_path }}</div>
              <div v-if="nodeWarning(node)" class="mt-0.5 break-words text-xs text-amber-700">
                {{ nodeWarning(node) }}
              </div>
            </div>
            <div class="flex shrink-0 gap-1">
              <button
                type="button"
                :data-testid="`quota-reset-chain-move-up-${node.department_external_id}`"
                class="flex h-9 w-9 items-center justify-center rounded border border-gray-300 text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                :aria-label="t('quotaResetSettings.moveUp', { department: node.department_display_path })"
                :title="t('quotaResetSettings.moveUp', { department: node.department_display_path })"
                :disabled="index === 0"
                @click="moveNode(index, -1)"
              >
                <ArrowUp aria-hidden="true" class="h-4 w-4" />
              </button>
              <button
                type="button"
                :data-testid="`quota-reset-chain-move-down-${node.department_external_id}`"
                class="flex h-9 w-9 items-center justify-center rounded border border-gray-300 text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                :aria-label="t('quotaResetSettings.moveDown', { department: node.department_display_path })"
                :title="t('quotaResetSettings.moveDown', { department: node.department_display_path })"
                :disabled="index === selectedChain.nodes.length - 1"
                @click="moveNode(index, 1)"
              >
                <ArrowDown aria-hidden="true" class="h-4 w-4" />
              </button>
              <button
                type="button"
                :data-testid="`quota-reset-chain-remove-${node.department_external_id}`"
                class="flex h-9 w-9 items-center justify-center rounded border border-red-200 text-red-600 hover:bg-red-50"
                :aria-label="t('quotaResetSettings.removeNode', { department: node.department_display_path })"
                :title="t('quotaResetSettings.removeNode', { department: node.department_display_path })"
                @click="removeNode(index)"
              >
                <Trash2 aria-hidden="true" class="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>
        <div v-else class="rounded-md border border-dashed border-gray-300 px-3 py-4 text-sm text-gray-500">
          {{ t('quotaResetSettings.chainEmpty') }}
        </div>
      </div>

      <div class="flex justify-end">
        <button
          type="button"
          data-testid="quota-reset-save-chains"
          class="min-h-10 rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
          :disabled="saving || loading || !chainsAuthoritative"
          @click="saveChains"
        >
          {{ saving ? t('settings.saving') : t('quotaResetSettings.saveChains') }}
        </button>
      </div>
    </div>
  </section>
</template>
