<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { listAdminUsers } from '@/api/adminUsers'
import { listAdminTeamAccountUserOptions, listTeamAccountUserOptions } from '@/api/teamAccounts'
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
  mode?: 'admin' | 'account'
  accountId?: number | null
}>(), {
  placeholder: '',
  disabled: false,
  departmentId: '',
  mode: 'admin',
  accountId: null,
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
const focused = ref(false)
const touched = ref(false)
const suppressClearOnBlur = ref(false)
const cancelled = ref(false)
const labelCache = ref(new Map<number, AdminUser>())
const trigger = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const userInput = ref<HTMLInputElement | null>(null)
const composing = ref(false)
const { position, start, stop } = useComboPosition(trigger)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

// The list only exists while the operator is typing; there is no browse mode.
const open = computed(() => touched.value && query.value.trim().length > 0)
// The displayed name is derived from the model value, never from a local copy,
// so clearing the owner anywhere empties the field immediately.
const selectedLabel = computed(() => {
  if (props.modelValue === null) return ''
  return labelCache.value.get(props.modelValue)?.username ?? ''
})
// While the field is focused it shows exactly what was typed, so deleting the
// text never resurrects the previously selected user.
const displayValue = computed(() => (focused.value ? query.value : selectedLabel.value))

function label(user: AdminUser) {
  return user.department?.display_path || user.department?.name || ''
}

async function search(keyword: string) {
  loading.value = true
  try {
    if (props.mode === 'account' && props.accountId) {
      // Account members may search people through the account-scoped endpoint;
      // the admin directory is not available to them.
      const response = await listTeamAccountUserOptions(props.accountId, keyword.trim())
      users.value = (response.data.data?.items ?? []).map((item: { id: number; username: string; email: string }) => ({
        id: item.id,
        username: item.username,
        email: item.email,
        role: 'user',
        auth_source: '',
        relay_auth_password: '',
      })) as AdminUser[]
      activeIndex.value = 0
      return
    }
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

// The parent owns the value: when it clears the owner (for example because the
// department was cleared), the field must drop the stale name immediately.
watch(
  () => props.modelValue,
  (value) => {
    if (value === null) return
    if (!labelCache.value.has(value)) {
      void hydrateSelection()
    }
  },
)

async function hydrateSelection() {
  if (props.modelValue === null) return
  if (props.mode === 'account' && props.accountId) {
    try {
      const response = await listAdminTeamAccountUserOptions(props.accountId, '')
      const match = (response.data.data?.items ?? []).find((item: { id: number; username: string; email: string }) => item.id === props.modelValue)
      if (match) {
        rememberUser({ ...match, role: 'user', auth_source: '', relay_auth_password: '' } as AdminUser)
      }
    } catch {
      // The field falls back to the raw value.
    }
    return
  }
  try {
    for (let page = 1; page <= 10; page += 1) {
      const response = await listAdminUsers({ page, page_size: 100 })
      const items = response.data.data?.items ?? []
      const match = items.find((user) => user.id === props.modelValue)
      if (match) {
        rememberUser(match)
        return
      }
      if (items.length < 100) break
    }
  } catch {
    // The field falls back to the raw value when the directory cannot be read.
  }
}

function rememberUser(user: AdminUser) {
  labelCache.value = new Map(labelCache.value).set(user.id, user)
}

function onInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  touched.value = true
  query.value = value
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!value.trim()) {
    users.value = []
    // Deleting the whole text clears the chosen user.
    if (props.modelValue !== null) {
      emit('update:modelValue', null)
      emit('select', null)
    }
    return
  }
  start()
  debounceTimer = setTimeout(() => {
    void search(value)
  }, 200)
}

function onFocus() {
  focused.value = true
  touched.value = false
  cancelled.value = false
  // Keep the chosen user's name in the field so it can be edited or deleted.
  query.value = selectedLabel.value
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
  if (suppressClearOnBlur.value) {
    suppressClearOnBlur.value = false
  } else if (cancelled.value) {
    cancelled.value = false
  } else if (!query.value.trim() && props.modelValue !== null) {
    emit('update:modelValue', null)
    emit('select', null)
  }
  focused.value = false
  touched.value = false
  query.value = ''
  stop()
}

// Ends editing, drops any in-flight IME composition, and re-asserts the label
// once Vue has patched the input value.
function endEditing() {
  composing.value = false
  suppressClearOnBlur.value = true
  focused.value = false
  touched.value = false
  query.value = ''
  stop()
  userInput.value?.blur()
  void nextTick(() => {
    if (userInput.value) userInput.value.value = displayValue.value
  })
}

function select(user: AdminUser) {
  rememberUser(user)
  emit('update:modelValue', user.id)
  emit('select', user)
  endEditing()
}

function clear() {
  emit('update:modelValue', null)
  emit('select', null)
  focused.value = false
  query.value = ''
}

function onKeydown(event: KeyboardEvent) {
  if (event.isComposing || composing.value) return
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
        ref="userInput"
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
        @compositionstart="onCompositionStart"
        @compositionend="onCompositionEnd"
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
