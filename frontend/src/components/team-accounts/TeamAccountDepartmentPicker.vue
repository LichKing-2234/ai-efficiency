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
  parent_external_id?: string | null
  name: string
  display_path: string
}

interface Row {
  id: string
  name: string
  path: string
  depth: number
  hasChildren: boolean
  expanded: boolean
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
const open = ref(false)
const activeIndex = ref(0)
const expanded = ref<Set<string>>(new Set())
const trigger = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const { position, start, stop } = useComboPosition(trigger)

const byID = computed(() => {
  const map = new Map<string, Department>()
  for (const department of departments.value) map.set(department.external_id, department)
  return map
})

const childrenByParent = computed(() => {
  const map = new Map<string, Department[]>()
  for (const department of departments.value) {
    const parent = department.parent_external_id || ''
    if (!parent || !byID.value.has(parent)) {
      if (!map.has('')) map.set('', [])
      map.get('')!.push(department)
      continue
    }
    if (!map.has(parent)) map.set(parent, [])
    map.get(parent)!.push(department)
  }
  for (const list of map.values()) list.sort((a, b) => a.name.localeCompare(b.name))
  return map
})

function treeRows(parentID: string, depth: number, rows: Row[]) {
  for (const department of childrenByParent.value.get(parentID) ?? []) {
    const hasChildren = (childrenByParent.value.get(department.external_id)?.length ?? 0) > 0
    const isOpen = expanded.value.has(department.external_id)
    rows.push({
      id: department.external_id,
      name: department.name,
      path: department.display_path || department.name,
      depth,
      hasChildren,
      expanded: isOpen,
    })
    if (hasChildren && isOpen) treeRows(department.external_id, depth + 1, rows)
  }
}

const rows = computed<Row[]>(() => {
  const keyword = query.value.trim().toLowerCase()
  if (keyword) {
    return departments.value
      .filter((department) => (
        department.name.toLowerCase().includes(keyword)
        || (department.display_path || '').toLowerCase().includes(keyword)
      ))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((department) => ({
        id: department.external_id,
        name: department.name,
        path: department.display_path || department.name,
        depth: 0,
        hasChildren: false,
        expanded: false,
      }))
  }
  const out: Row[] = []
  treeRows('', 0, out)
  return out
})

const selected = computed(() => byID.value.get(props.modelValue) ?? null)

const displayValue = computed(() => {
  if (open.value) return query.value
  return selected.value ? (selected.value.display_path || selected.value.name) : ''
})

async function load() {
  if (loading.value || departments.value.length > 0) return
  loading.value = true
  try {
    const response = await listAdminUserDepartments()
    departments.value = (response.data.data?.items ?? []).map((item) => ({
      external_id: item.external_id,
      parent_external_id: item.parent_external_id ?? '',
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

function openList() {
  if (props.disabled) return
  open.value = true
  query.value = ''
  activeIndex.value = 0
  start()
  void nextTick(() => list.value?.focus())
}

function closeList() {
  open.value = false
  stop()
}

function toggle(event: Event, row: Row) {
  event.stopPropagation()
  const next = new Set(expanded.value)
  if (next.has(row.id)) next.delete(row.id)
  else next.add(row.id)
  expanded.value = next
}

function select(row: Row) {
  emit('update:modelValue', row.id)
  emit('change', row.id)
  closeList()
}

function clear() {
  emit('update:modelValue', '')
  emit('change', '')
  closeList()
}

function onInput(event: Event) {
  query.value = (event.target as HTMLInputElement).value
  open.value = true
  activeIndex.value = 0
}

function move(step: number) {
  const total = rows.value.length
  if (total === 0) return
  activeIndex.value = (activeIndex.value + step + total) % total
  const active = list.value?.querySelector<HTMLElement>('[data-active="true"]')
  active?.scrollIntoView({ block: 'nearest' })
}

function onKeydown(event: KeyboardEvent) {
  if (!open.value && (event.key === 'Enter' || event.key === 'ArrowDown')) {
    openList()
    event.preventDefault()
    return
  }
  if (event.key === 'ArrowDown') {
    move(1)
    event.preventDefault()
  } else if (event.key === 'ArrowUp') {
    move(-1)
    event.preventDefault()
  } else if (event.key === 'Enter') {
    const row = rows.value[activeIndex.value]
    if (row) {
      select(row)
      event.preventDefault()
    }
  } else if (event.key === 'Escape') {
    if (query.value) {
      query.value = ''
    } else {
      closeList()
    }
    event.preventDefault()
  }
}
</script>

<template>
  <div ref="trigger" class="relative w-full">
    <div
      class="flex h-8 w-full items-center gap-1 rounded border border-slate-300 bg-white px-2 text-sm"
      :class="disabled ? 'cursor-not-allowed bg-slate-50' : 'cursor-text'"
      @click="openList"
    >
      <input
        :value="displayValue"
        :placeholder="placeholder || t('teamAccounts.admin.departmentPlaceholder')"
        :disabled="disabled"
        class="min-w-0 flex-1 border-0 bg-transparent p-0 outline-none placeholder:text-slate-400"
        data-testid="team-account-department-input"
        role="combobox"
        :aria-expanded="open"
        @input="onInput"
        @keydown="onKeydown"
        @focus="openList"
        @blur="closeList"
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
      <span v-else class="shrink-0 text-slate-300">▾</span>
    </div>

    <Teleport to="body">
      <ul
        v-if="open"
        ref="list"
        tabindex="-1"
        class="fixed z-[3000] max-h-80 overflow-auto rounded border border-slate-200 bg-white py-1 text-sm shadow-lg outline-none"
        :style="{ left: `${position.left}px`, top: `${position.top}px`, width: `${Math.max(position.width, 260)}px` }"
        data-testid="team-account-department-list"
        @mousedown.prevent
      >
        <li v-if="loading" class="px-3 py-2 text-slate-400">…</li>
        <li v-else-if="rows.length === 0" class="px-3 py-2 text-slate-400">
          {{ t('teamAccounts.userPicker.empty') }}
        </li>
        <li
          v-for="(row, index) in rows"
          :key="row.id"
          :data-active="index === activeIndex"
          :data-testid="`department-option-${row.id}`"
          class="flex cursor-pointer items-center gap-1 px-2 py-1.5 hover:bg-blue-50"
          :class="index === activeIndex ? 'bg-blue-50' : ''"
          @mouseenter="activeIndex = index"
          @mousedown.prevent="select(row)"
        >
          <button
            v-if="row.hasChildren && !query"
            type="button"
            class="w-4 shrink-0 text-slate-400"
            @mousedown.prevent.stop="toggle($event, row)"
          >
            {{ row.expanded ? '▾' : '▸' }}
          </button>
          <span v-else class="w-4 shrink-0" />
          <span class="min-w-0 flex-1">
            <span :style="{ paddingLeft: `${row.depth * 12}px` }" class="block truncate">
              <HighlightedText :text="row.name" :query="query" />
            </span>
            <span v-if="query && row.path !== row.name" class="block truncate text-xs text-slate-400">
              <HighlightedText :text="row.path" :query="query" />
            </span>
          </span>
        </li>
      </ul>
    </Teleport>
  </div>
</template>
