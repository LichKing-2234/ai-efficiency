<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { listAdminUserDepartments } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import HighlightedText from './HighlightedText.vue'
import { useComboPosition } from './comboPosition'

interface Department {
  external_id: string
  name: string
  display_path: string
}

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  disabled?: boolean
}>(), {
  placeholder: '',
  disabled: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)

const departments = ref<Department[]>([])
const loading = ref(false)
const query = ref('')
const activeIndex = ref(0)
const focused = ref(false)
const touched = ref(false)
const trigger = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const departmentInput = ref<HTMLInputElement | null>(null)
const composing = ref(false)
const { position, start, stop } = useComboPosition(trigger)

const selected = computed(() => departments.value.find((item) => item.external_id === props.modelValue) ?? null)
// The list only exists while the operator is typing; there is no browse mode.
const open = computed(() => touched.value && query.value.trim().length > 0)
const rows = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  if (!keyword) return []
  return departments.value.filter((department) => (
    department.name.toLowerCase().includes(keyword)
    || department.display_path.toLowerCase().includes(keyword)
  ))
})
// While the field is focused it shows exactly what was typed, so deleting the
// text never resurrects the previously selected department.
const displayValue = computed(() => (focused.value ? query.value : selected.value?.display_path ?? ''))

async function load() {
  if (loading.value || departments.value.length > 0) return
  loading.value = true
  try {
    const response = await listAdminUserDepartments()
    departments.value = (response.data.data?.items ?? []).map((item) => ({
      external_id: item.external_id,
      name: item.name,
      display_path: item.display_path || item.name,
    }))
  } catch {
    departments.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
})

function onInput(event: Event) {
  // Composition (for example pinyin IME) fires input events before the final
  // characters exist; ignore them so a half-composed word never becomes the
  // query that survives a selection.
  if (composing.value || !focused.value) return
  touched.value = true
  query.value = (event.target as HTMLInputElement).value
  activeIndex.value = 0
  start()
  void nextTick(() => list.value?.querySelector<HTMLElement>('[data-active="true"]')?.scrollIntoView?.({ block: 'nearest' }))
}

function onFocus() {
  focused.value = true
  touched.value = false
  // Keep the current selection in the field so it can be edited or deleted
  // character by character instead of vanishing on click.
  query.value = selected.value ? selected.value.display_path || selected.value.name : ''
  start()
}

function onCompositionStart() {
  composing.value = true
}

function onCompositionEnd(event: Event) {
  composing.value = false
  // A blur during composition commits late; ignore anything that arrives after
  // the field already selected a value.
  if (!focused.value) {
    return
  }
  query.value = (event.target as HTMLInputElement).value
}

function onBlur() {
  composing.value = false
  focused.value = false
  touched.value = false
  query.value = ''
  stop()
}

// Ends editing, drops any in-flight IME composition, and re-asserts the label
// once Vue has patched the input value.
function endEditing() {
  composing.value = false
  focused.value = false
  touched.value = false
  query.value = ''
  stop()
  departmentInput.value?.blur()
  void nextTick(() => {
    if (departmentInput.value) departmentInput.value.value = displayValue.value
  })
}

function select(department: Department) {
  emit('update:modelValue', department.external_id)
  emit('change', department.external_id)
  endEditing()
}

function clear() {
  emit('update:modelValue', '')
  emit('change', '')
  endEditing()
}

function onKeydown(event: KeyboardEvent) {
  if (event.isComposing || composing.value) return
  if (!open.value) return
  if (event.key === 'ArrowDown') {
    activeIndex.value = Math.min(activeIndex.value + 1, rows.value.length - 1)
    event.preventDefault()
  } else if (event.key === 'ArrowUp') {
    activeIndex.value = Math.max(activeIndex.value - 1, 0)
    event.preventDefault()
  } else if (event.key === 'Enter') {
    const row = rows.value[activeIndex.value]
    if (row) {
      select(row)
      event.preventDefault()
    }
  } else if (event.key === 'Escape') {
    query.value = ''
    event.preventDefault()
  }
}
</script>

<template>
  <div ref="trigger" class="relative w-full">
    <div
      class="flex h-8 w-full items-center gap-1 rounded border border-slate-300 bg-white px-2 text-sm focus-within:border-blue-400"
      :class="disabled ? 'bg-slate-50' : ''"
    >
      <input
        ref="departmentInput"
        :value="displayValue"
        :placeholder="placeholder || t('teamAccounts.admin.departmentPlaceholder')"
        :disabled="disabled"
        class="min-w-0 flex-1 border-0 bg-transparent p-0 outline-none placeholder:text-slate-400"
        data-testid="team-account-department-input"
        role="combobox"
        :aria-expanded="open"
        @input="onInput"
        @keydown="onKeydown"
        @focus="onFocus"
        @blur="onBlur"
        @compositionstart="onCompositionStart"
        @compositionend="onCompositionEnd"
      >
      <button
        v-if="modelValue && !disabled"
        type="button"
        class="shrink-0 text-slate-400 hover:text-slate-600"
        aria-label="clear"
        @mousedown.prevent="clear"
      >
        ×
      </button>
    </div>

    <Teleport to="body">
      <ul
        v-if="open"
        ref="list"
        class="fixed z-[3000] max-h-72 overflow-auto rounded border border-slate-200 bg-white py-1 text-sm shadow-lg"
        :style="{ left: `${position.left}px`, top: `${position.top}px`, width: `${Math.max(position.width, 320)}px` }"
        data-testid="team-account-department-list"
        @mousedown.prevent
      >
        <li v-if="loading" class="px-3 py-2 text-slate-400">…</li>
        <li v-else-if="rows.length === 0" class="px-3 py-2 text-slate-400">
          {{ t('teamAccounts.userPicker.empty') }}
        </li>
        <li
          v-for="(row, index) in rows"
          :key="row.external_id"
          :data-testid="`department-option-${row.external_id}`"
          :data-active="index === activeIndex"
          class="cursor-pointer px-3 py-1.5"
          :class="index === activeIndex ? 'bg-blue-50' : 'hover:bg-slate-50'"
          @mouseenter="activeIndex = index"
          @mousedown.prevent="select(row)"
        >
          <span class="block truncate font-medium text-slate-800">
            <HighlightedText :text="row.name" :query="query" />
          </span>
          <span v-if="row.display_path !== row.name" class="block truncate text-xs text-slate-400">
            <HighlightedText :text="row.display_path" :query="query" />
          </span>
        </li>
      </ul>
    </Teleport>
  </div>
</template>
