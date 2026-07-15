<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { listAdminUserDepartmentOptions } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import type { AdminDepartmentOption } from '@/types'

const props = defineProps<{
  modelValue: string
  labelledBy?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const { t } = useI18n()
const pickerID = useId()
const listboxID = `${pickerID}-listbox`
const valueID = `${pickerID}-value`
const allOptionID = `${pickerID}-option-all`
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const open = ref(false)
const loading = ref(false)
const error = ref('')
const searchQuery = ref('')
const items = ref<AdminDepartmentOption[]>([])
const selectedOption = ref<AdminDepartmentOption | null>(null)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const hasLoadedOptions = ref(false)
const activeOptionIndex = ref(0)
const selectionCache = new Map<string, AdminDepartmentOption | null>()
let optionsRequested = false
let requestGeneration = 0
let selectionRequestGeneration = 0
let selectionPendingID = ''
let searchTimer: number | undefined
let committedQuery = ''

const selectedLabel = computed(() => {
  if (!props.modelValue) return t('adminUsers.allDepartments')
  const option = selectedOption.value
  if (option?.external_id !== props.modelValue) return props.modelValue
  return option.display_path || option.name || props.modelValue
})
const canGoPrevious = computed(() => page.value > 1)
const canGoNext = computed(() => page.value * pageSize.value < total.value)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const triggerLabelledBy = computed(() => {
  const externalLabel = props.labelledBy?.trim()
  return externalLabel ? `${externalLabel} ${valueID}` : valueID
})
const activeOptionID = computed(() => {
  if (activeOptionIndex.value <= 0) return allOptionID
  return items.value[activeOptionIndex.value - 1]
    ? optionID(activeOptionIndex.value)
    : allOptionID
})

function clearSearchTimer() {
  if (searchTimer !== undefined) {
    window.clearTimeout(searchTimer)
    searchTimer = undefined
  }
}

function cacheOptions(nextItems: AdminDepartmentOption[]) {
  for (const item of nextItems) selectionCache.set(item.external_id, item)
}

function optionID(index: number) {
  return `${pickerID}-option-${index}`
}

function resetActiveOption() {
  const selectedIndex = items.value.findIndex((item) => item.external_id === props.modelValue)
  activeOptionIndex.value = selectedIndex >= 0 ? selectedIndex + 1 : 0
}

async function loadOptions(targetPage: number, selectedID = '', query = searchQuery.value.trim()) {
  const generation = ++requestGeneration
  const selectionGeneration = selectedID ? ++selectionRequestGeneration : 0
  if (selectedID) selectionPendingID = selectedID
  optionsRequested = true
  loading.value = true
  error.value = ''
  try {
    const params = {
      ...(query ? { q: query } : {}),
      ...(selectedID ? { selected_id: selectedID } : {}),
      page: targetPage,
      page_size: 20,
    }
    const response = await listAdminUserDepartmentOptions(params)
    const data = response.data.data
    if (selectedID && selectionGeneration === selectionRequestGeneration) {
      const resolved = data?.selected ?? null
      selectionCache.set(selectedID, resolved)
      if (props.modelValue === selectedID) selectedOption.value = resolved
    }
    if (generation !== requestGeneration) return
    const nextItems = data?.items ?? []
    items.value = nextItems
    cacheOptions(nextItems)
    page.value = data?.page ?? targetPage
    pageSize.value = data?.page_size ?? 20
    total.value = data?.total ?? 0
    hasLoadedOptions.value = true
    committedQuery = query
    resetActiveOption()
  } catch (err: any) {
    if (generation !== requestGeneration) return
    items.value = []
    page.value = 1
    pageSize.value = 20
    total.value = 0
    hasLoadedOptions.value = false
    activeOptionIndex.value = 0
    error.value = err.response?.data?.message || err.message || t('adminUsers.departmentsLoadFailed')
    optionsRequested = false
  } finally {
    if (selectedID && selectionGeneration === selectionRequestGeneration && selectionPendingID === selectedID) {
      selectionPendingID = ''
    }
    if (generation === requestGeneration) loading.value = false
  }
}

async function resolveSelection(value: string) {
  const selectedID = value.trim()
  if (!selectedID) {
    selectedOption.value = null
    return
  }
  if (selectionCache.has(selectedID)) {
    selectedOption.value = selectionCache.get(selectedID) ?? null
    return
  }
  if (selectionPendingID === selectedID) return
  await loadOptions(1, selectedID, '')
}

async function openPicker() {
  if (open.value) return
  open.value = true
  const selectedID = props.modelValue.trim()
  const selectionUnresolved = selectedID
    && selectedOption.value?.external_id !== selectedID
    && !selectionCache.has(selectedID)
  if (selectionUnresolved && selectionPendingID !== selectedID) {
    await resolveSelection(selectedID)
  } else if (!hasLoadedOptions.value && !loading.value && !optionsRequested) {
    await loadOptions(1)
  }
  await nextTick()
  searchInput.value?.focus()
}

async function toggleOpen() {
  if (open.value) {
    close()
    return
  }
  await openPicker()
}

function close(restoreTriggerFocus = false) {
  if (!open.value) {
    if (restoreTriggerFocus) trigger.value?.focus()
    return
  }
  open.value = false
  clearSearchTimer()
  requestGeneration += 1
  loading.value = false
  optionsRequested = hasLoadedOptions.value
  if (!error.value) searchQuery.value = committedQuery
  if (restoreTriggerFocus) void nextTick(() => trigger.value?.focus())
}

function choose(value: string, option: AdminDepartmentOption | null, restoreTriggerFocus = false) {
  selectionRequestGeneration += 1
  selectionPendingID = ''
  selectedOption.value = option
  if (option) selectionCache.set(option.external_id, option)
  emit('update:modelValue', value)
  emit('change', value)
  close(restoreTriggerFocus)
}

function scheduleSearch() {
  clearSearchTimer()
  requestGeneration += 1
  activeOptionIndex.value = 0
  searchTimer = window.setTimeout(() => {
    searchTimer = undefined
    void loadOptions(1)
  }, 300)
}

async function previousPage() {
  if (loading.value || !canGoPrevious.value) return
  await loadOptions(page.value - 1)
}

async function nextPage() {
  if (loading.value || !canGoNext.value) return
  await loadOptions(page.value + 1)
}

function handleDocumentPointerDown(event: Event) {
  if (root.value && event.target instanceof Node && !root.value.contains(event.target)) close()
}

function handleFocusOut(event: FocusEvent) {
  if (!root.value) return
  if (event.relatedTarget instanceof Node && root.value.contains(event.relatedTarget)) return
  close()
}

function moveActiveOption(nextIndex: number) {
  activeOptionIndex.value = Math.max(0, Math.min(nextIndex, items.value.length))
}

function activateActiveOption() {
  if (activeOptionIndex.value === 0) {
    choose('', null, true)
    return
  }
  const option = items.value[activeOptionIndex.value - 1]
  if (option) choose(option.external_id, option, true)
}

function handleSearchKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    moveActiveOption(activeOptionIndex.value + 1)
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    moveActiveOption(activeOptionIndex.value - 1)
  } else if (event.key === 'Home') {
    event.preventDefault()
    moveActiveOption(0)
  } else if (event.key === 'End') {
    event.preventDefault()
    moveActiveOption(items.value.length)
  } else if (event.key === 'Enter') {
    event.preventDefault()
    activateActiveOption()
  } else if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close(true)
  }
}

watch(
  () => props.modelValue,
  (value) => {
    selectionRequestGeneration += 1
    selectionPendingID = ''
    if (!value) {
      selectedOption.value = null
      return
    }
    if (selectedOption.value?.external_id !== value) {
      selectedOption.value = null
      void resolveSelection(value)
    }
  },
)

onMounted(() => {
  document.addEventListener('pointerdown', handleDocumentPointerDown)
  if (props.modelValue.trim()) void resolveSelection(props.modelValue)
})

onBeforeUnmount(() => {
  requestGeneration += 1
  selectionRequestGeneration += 1
  selectionPendingID = ''
  clearSearchTimer()
  document.removeEventListener('pointerdown', handleDocumentPointerDown)
})
</script>

<template>
  <div ref="root" data-testid="admin-department-picker" class="relative mt-1" @focusout="handleFocusOut">
    <button
      ref="trigger"
      type="button"
      data-testid="admin-department-picker-trigger"
      class="flex h-[38px] w-full items-center justify-between gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-left text-sm text-gray-700 hover:border-gray-400 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
      :aria-expanded="open"
      :aria-labelledby="triggerLabelledBy"
      aria-haspopup="listbox"
      :aria-controls="listboxID"
      @click="toggleOpen"
      @keydown.esc.stop.prevent="close(true)"
    >
      <span :id="valueID" class="min-w-0 truncate">{{ selectedLabel }}</span>
      <span class="shrink-0 text-xs text-gray-400" aria-hidden="true">{{ open ? '-' : '+' }}</span>
    </button>

    <div
      v-if="open"
      data-testid="admin-department-picker-menu"
      class="absolute left-0 z-30 mt-1 w-full min-w-[16rem] overflow-hidden rounded-md border border-gray-200 bg-white shadow-lg"
      @keydown.esc.stop.prevent="close(true)"
    >
      <div class="border-b border-gray-100 p-2">
        <input
          ref="searchInput"
          v-model="searchQuery"
          data-testid="admin-department-picker-search"
          type="search"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded="true"
          :aria-controls="listboxID"
          :aria-activedescendant="activeOptionID"
          class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 placeholder:text-gray-400 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
          :placeholder="t('adminUsers.search')"
          @input="scheduleSearch"
          @keydown="handleSearchKeydown"
        />
      </div>

      <div :id="listboxID" class="max-h-64 overflow-y-auto py-1" role="listbox" :aria-label="t('adminUsers.department')">
        <button
          :id="allOptionID"
          type="button"
          data-testid="admin-department-picker-all"
          class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-50"
          :class="!modelValue || activeOptionIndex === 0 ? 'bg-gray-50 font-medium text-gray-900' : 'text-gray-700'"
          role="option"
          tabindex="-1"
          :aria-selected="!modelValue"
          @mouseenter="activeOptionIndex = 0"
          @click="choose('', null)"
        >
          {{ t('adminUsers.allDepartments') }}
        </button>
        <p v-if="loading" class="px-3 py-3 text-sm text-gray-500">{{ t('adminUsers.loading') }}</p>
        <button
          v-for="(option, index) in items"
          v-else
          :key="option.external_id"
          :id="optionID(index + 1)"
          type="button"
          :data-testid="`admin-department-picker-option-${option.external_id}`"
          class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-50"
          :class="modelValue === option.external_id || activeOptionIndex === index + 1 ? 'bg-gray-50 font-medium text-gray-900' : 'text-gray-700'"
          role="option"
          tabindex="-1"
          :aria-selected="modelValue === option.external_id"
          @mouseenter="activeOptionIndex = index + 1"
          @click="choose(option.external_id, option)"
        >
          <span class="block truncate">{{ option.display_path || option.name }}</span>
        </button>
        <p
          v-if="!loading && !error && items.length === 0"
          data-testid="admin-department-picker-empty"
          class="px-3 py-3 text-sm text-gray-400"
        >
          {{ t('adminUsers.noDepartments') }}
        </p>
        <p v-if="error" data-testid="admin-department-picker-error" class="px-3 py-3 text-sm text-red-700">
          {{ error }}
        </p>
      </div>

      <div class="flex items-center justify-between gap-2 border-t border-gray-100 px-2 py-2 text-xs text-gray-500">
        <button
          type="button"
          data-testid="admin-department-picker-prev"
          class="rounded border border-gray-200 px-2 py-1 text-gray-700 disabled:opacity-40"
          :disabled="loading || !canGoPrevious"
          @click="previousPage"
        >
          {{ t('adminUsers.prev') }}
        </button>
        <span data-testid="admin-department-picker-page">{{ t('adminUsers.page') }} {{ page }} / {{ totalPages }}</span>
        <button
          type="button"
          data-testid="admin-department-picker-next"
          class="rounded border border-gray-200 px-2 py-1 text-gray-700 disabled:opacity-40"
          :disabled="loading || !canGoNext"
          @click="nextPage"
        >
          {{ t('adminUsers.next') }}
        </button>
      </div>
    </div>
  </div>
</template>
