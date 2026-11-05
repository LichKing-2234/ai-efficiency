<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { listAdminUsers } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import HighlightedText from './HighlightedText.vue'
import { useComboPosition } from './comboPosition'
import type { AdminUser } from '@/types'

const PAGE_SIZE = 100

const props = withDefaults(defineProps<{
  modelValue: number | null
  placeholder?: string
  disabled?: boolean
  departmentId?: string
}>(), {
  placeholder: '',
  disabled: false,
  departmentId: '',
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  select: [user: AdminUser | null]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)

const users = ref<AdminUser[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const query = ref('')
const open = ref(false)
const activeIndex = ref(0)
const trigger = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const { position, start, stop } = useComboPosition(trigger)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

const selected = computed(() => users.value.find((user) => user.id === props.modelValue) ?? null)
const displayValue = computed(() => (open.value ? query.value : selected.value?.username ?? ''))
const hasMore = computed(() => users.value.length < total.value)

function label(user: AdminUser) {
  return user.department?.display_path || user.department?.name || ''
}

async function fetchPage(nextPage: number, keyword: string, append: boolean) {
  if (loading.value) return
  loading.value = true
  try {
    const response = await listAdminUsers({
      q: keyword.trim() || undefined,
      department_id: props.departmentId.trim() || undefined,
      page: nextPage,
      page_size: PAGE_SIZE,
    })
    const data = response.data.data
    const items = data?.items ?? []
    total.value = data?.total ?? items.length
    page.value = nextPage
    users.value = append ? [...users.value, ...items] : items
    activeIndex.value = 0
  } catch {
    if (!append) {
      users.value = []
      total.value = 0
    }
  } finally {
    loading.value = false
  }
}

async function reload(keyword = query.value) {
  await fetchPage(1, keyword, false)
  if (props.modelValue !== null && !users.value.some((user) => user.id === props.modelValue)) {
    emitSelected(null)
  }
}

function emitSelected(user: AdminUser | null) {
  emit('update:modelValue', user ? user.id : null)
  emit('select', user)
}

onMounted(() => {
  void reload('')
})

watch(
  () => props.departmentId,
  () => {
    void reload(query.value)
  },
)

function openList() {
  if (props.disabled) return
  open.value = true
  start()
  if (users.value.length === 0) void reload('')
}

function closeList() {
  open.value = false
  query.value = ''
  stop()
}

function onInput(event: Event) {
  query.value = (event.target as HTMLInputElement).value
  open.value = true
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    void reload(query.value)
  }, 250)
}

function select(user: AdminUser) {
  emitSelected(user)
  closeList()
}

function clear() {
  emitSelected(null)
  closeList()
}

function move(step: number) {
  const totalRows = users.value.length
  if (totalRows === 0) return
  activeIndex.value = (activeIndex.value + step + totalRows) % totalRows
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
    const user = users.value[activeIndex.value]
    if (user) {
      select(user)
      event.preventDefault()
    }
  } else if (event.key === 'Escape') {
    closeList()
    event.preventDefault()
  }
}

function onScroll() {
  const element = list.value
  if (!element || loading.value || !hasMore.value) return
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 16) {
    void fetchPage(page.value + 1, query.value, true)
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
        :placeholder="placeholder || t('teamAccounts.userPicker.placeholder')"
        :disabled="disabled"
        class="min-w-0 flex-1 border-0 bg-transparent p-0 outline-none placeholder:text-slate-400"
        data-testid="team-account-user-input"
        role="combobox"
        :aria-expanded="open"
        @input="onInput"
        @keydown="onKeydown"
        @focus="openList"
        @blur="closeList"
      >
      <button
        v-if="modelValue !== null && !disabled"
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
        class="fixed z-[3000] max-h-72 overflow-auto rounded border border-slate-200 bg-white py-1 text-sm shadow-lg outline-none"
        :style="{ left: `${position.left}px`, top: `${position.top}px`, width: `${Math.max(position.width, 280)}px` }"
        data-testid="team-account-user-list"
        @mousedown.prevent
        @scroll="onScroll"
      >
        <li v-if="loading && users.length === 0" class="px-3 py-2 text-slate-400">…</li>
        <li v-else-if="users.length === 0" class="px-3 py-2 text-slate-400">
          {{ t('teamAccounts.userPicker.empty') }}
        </li>
        <li
          v-for="(user, index) in users"
          :key="user.id"
          :data-active="index === activeIndex"
          :data-testid="`user-option-${user.id}`"
          class="cursor-pointer px-3 py-1.5 hover:bg-blue-50"
          :class="index === activeIndex ? 'bg-blue-50' : ''"
          @mouseenter="activeIndex = index"
          @mousedown.prevent="select(user)"
        >
          <span class="block truncate">
            <HighlightedText :text="user.username" :query="query" />
          </span>
          <span class="block truncate text-xs text-slate-400">
            <HighlightedText :text="user.email" :query="query" />
            <template v-if="label(user)"> · {{ label(user) }}</template>
          </span>
        </li>
        <li v-if="loading && users.length > 0" class="px-3 py-1 text-center text-xs text-slate-400">…</li>
        <li v-else-if="hasMore" class="px-3 py-1 text-center text-xs text-slate-400">
          {{ t('teamAccounts.userPicker.scrollMore') }}
        </li>
      </ul>
    </Teleport>
  </div>
</template>
