<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { listAdminUsers } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import HighlightedText from './HighlightedText.vue'
import { useComboPosition } from './comboPosition'
import type { AdminUser } from '@/types'

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
const loading = ref(false)
const query = ref('')
const activeIndex = ref(0)
const selectedUser = ref<AdminUser | null>(null)
const trigger = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const { position, start, stop } = useComboPosition(trigger)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

// The list only exists while the operator is typing; there is no browse mode.
const open = computed(() => query.value.trim().length > 0)
const displayValue = computed(() => (open.value ? query.value : selectedUser.value?.username ?? ''))

function label(user: AdminUser) {
  return user.department?.display_path || user.department?.name || ''
}

async function search(keyword: string) {
  loading.value = true
  try {
    const response = await listAdminUsers({
      q: keyword.trim() || undefined,
      department_id: props.departmentId.trim() || undefined,
      page: 1,
      page_size: 50,
    })
    users.value = response.data.data?.items ?? []
    activeIndex.value = 0
  } catch {
    users.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (props.modelValue !== null) void hydrateSelection()
})

async function hydrateSelection() {
  if (props.modelValue === null) {
    selectedUser.value = null
    return
  }
  try {
    const response = await listAdminUsers({ page: 1, page_size: 100 })
    const items = response.data.data?.items ?? []
    selectedUser.value = items.find((user) => user.id === props.modelValue) ?? null
  } catch {
    selectedUser.value = null
  }
}

function onInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  query.value = value
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!value.trim()) {
    users.value = []
    return
  }
  start()
  debounceTimer = setTimeout(() => {
    void search(value)
  }, 200)
}

function onFocus() {
  start()
}

function onBlur() {
  query.value = ''
  stop()
}

function select(user: AdminUser) {
  selectedUser.value = user
  emit('update:modelValue', user.id)
  emit('select', user)
  query.value = ''
  stop()
}

function clear() {
  selectedUser.value = null
  emit('update:modelValue', null)
  emit('select', null)
  query.value = ''
}

function onKeydown(event: KeyboardEvent) {
  if (!open.value) return
  if (event.key === 'ArrowDown') {
    activeIndex.value = Math.min(activeIndex.value + 1, users.value.length - 1)
    event.preventDefault()
  } else if (event.key === 'ArrowUp') {
    activeIndex.value = Math.max(activeIndex.value - 1, 0)
    event.preventDefault()
  } else if (event.key === 'Enter') {
    const user = users.value[activeIndex.value]
    if (user) select(user)
    event.preventDefault()
  } else if (event.key === 'Escape') {
    query.value = ''
    event.preventDefault()
  }
}
</script>

<template>
  <div ref="trigger" class="relative w-full">
    <div class="flex h-8 w-full items-center gap-1 rounded border border-slate-300 bg-white px-2 text-sm focus-within:border-blue-400">
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
        @focus="onFocus"
        @blur="onBlur"
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
    </div>

    <Teleport to="body">
      <ul
        v-if="open"
        ref="list"
        class="fixed z-[3000] max-h-72 overflow-auto rounded border border-slate-200 bg-white py-1 text-sm shadow-lg"
        :style="{ left: `${position.left}px`, top: `${position.top}px`, width: `${Math.max(position.width, 320)}px` }"
        data-testid="team-account-user-list"
        @mousedown.prevent
      >
        <li v-if="loading" class="px-3 py-2 text-slate-400">…</li>
        <li v-else-if="users.length === 0" class="px-3 py-2 text-slate-400">
          {{ t('teamAccounts.userPicker.empty') }}
        </li>
        <li
          v-for="(user, index) in users"
          :key="user.id"
          :data-testid="`user-option-${user.id}`"
          :data-active="index === activeIndex"
          class="cursor-pointer px-3 py-1.5"
          :class="index === activeIndex ? 'bg-blue-50' : 'hover:bg-slate-50'"
          @mouseenter="activeIndex = index"
          @mousedown.prevent="select(user)"
        >
          <span class="block truncate font-medium text-slate-800">
            <HighlightedText :text="user.username" :query="query" />
          </span>
          <span class="block truncate text-xs text-slate-400">
            <HighlightedText :text="user.email" :query="query" />
            <template v-if="label(user)"> · {{ label(user) }}</template>
          </span>
        </li>
      </ul>
    </Teleport>
  </div>
</template>
