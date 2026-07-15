<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getEventDetail, getEventSummary, listEvents, searchEventUsers } from '@/api/events'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import { useModalFocus } from '@/composables/useModalFocus'
import type {
  ToolUsageEventDetail,
  ToolUsageEventRow,
  ToolUsageEventSummary,
  ToolUsageEventUserOption,
} from '@/types'

const auth = useAuthStore()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const eventRowsMediaQuery = window.matchMedia('(min-width: 768px)')
const maxEventPageSize = 100

const loading = ref(true)
const summary = ref<ToolUsageEventSummary | null>(null)
const rows = ref<ToolUsageEventRow[]>([])
const total = ref(0)
const detailLoading = ref(false)
const selectedEvent = ref<ToolUsageEventDetail | null>(null)
const selectedEventId = ref<number | null>(null)
const eventDetailDialog = ref<HTMLElement | null>(null)
const mobileFiltersOpen = ref(false)
const desktopEventRows = ref(eventRowsMediaQuery.matches)
const advancedDetailsOpen = ref(false)
const userSearch = ref('')
const userOptions = ref<ToolUsageEventUserOption[]>([])
const selectedUser = ref<ToolUsageEventUserOption | null>(null)
const selectedUserId = ref<number | null>(queryNumber('user_id', 0) || null)
const userSearchLoading = ref(false)

const defaultFrom = toDateTimeLocal(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000))
const defaultTo = toDateTimeLocal(new Date())

const filters = reactive({
  from: queryString('from') || defaultFrom,
  to: queryString('to') || defaultTo,
  tool: queryString('tool'),
  binding_status: queryString('binding_status'),
  q: queryString('q'),
  limit: queryNumber('limit', 20),
  offset: queryNumber('offset', 0),
})

const isAdmin = computed(() => auth.isAdmin)
const detailOpen = computed(() => selectedEventId.value != null)
const currentPage = computed(() => Math.floor(filters.offset / filters.limit) + 1)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.limit)))
const canGoPrev = computed(() => filters.offset > 0)
const canGoNext = computed(() => filters.offset + filters.limit < total.value)
const showMobileEventRows = computed(() => rows.value.length > 0 && !desktopEventRows.value)
const showDesktopEventRows = computed(() => rows.value.length > 0 && desktopEventRows.value)
const formattedRawPayload = computed(() => JSON.stringify(selectedEvent.value?.raw_payload, null, 2))
const filterSummaryBadges = computed(() => {
  const badges = [timeFilterSummary()]
  badges.push(filters.tool ? t('events.toolSummary', { tool: filters.tool }) : t('events.allTools'))
  badges.push(filters.binding_status ? t('events.codeLinkSummary', { status: bindingStatusLabel(filters.binding_status) }) : t('events.allCodeLinks'))
  if (filters.q.trim()) badges.push(t('events.searchSummary', { query: filters.q.trim() }))
  if (isAdmin.value && selectedUserId.value) badges.push(t('events.userSummary', { id: selectedUserId.value }))
  return badges
})

const { handleKeydown: handleDetailKeydown } = useModalFocus(detailOpen, eventDetailDialog, {
  onClose: closeDetail,
})

function queryString(key: string) {
  const value = route.query[key]
  return typeof value === 'string' ? value : ''
}

function queryNumber(key: string, fallback: number) {
  const value = Number(queryString(key))
  if (key === 'limit') {
    return Number.isFinite(value) && value > 0 ? Math.min(value, maxEventPageSize) : fallback
  }
  return Number.isFinite(value) && value >= 0 ? value : fallback
}

function replaceEventQuery() {
  const next: Record<string, string> = {}
  if (filters.from) next.from = filters.from
  if (filters.to) next.to = filters.to
  if (filters.tool) next.tool = filters.tool
  if (filters.binding_status) next.binding_status = filters.binding_status
  if (filters.q) next.q = filters.q
  if (filters.limit !== 20) next.limit = String(filters.limit)
  if (filters.offset > 0) next.offset = String(filters.offset)
  if (isAdmin.value && selectedUserId.value) next.user_id = String(selectedUserId.value)
  void router.replace({ query: next })
}

function userLabel(user: ToolUsageEventUserOption) {
  return user.email || user.username || `User #${user.id}`
}

function userMeta(user: ToolUsageEventUserOption) {
  const parts = []
  if (user.username && user.username !== user.email) parts.push(user.username)
  parts.push(user.role, `${user.event_count} events`)
  return parts.join(' · ')
}

function toDateTimeLocal(date: Date) {
  const pad = (value: number) => String(value).padStart(2, '0')
  const yyyy = date.getFullYear()
  const mm = pad(date.getMonth() + 1)
  const dd = pad(date.getDate())
  const hh = pad(date.getHours())
  const min = pad(date.getMinutes())
  return `${yyyy}-${mm}-${dd}T${hh}:${min}`
}

function fromDateTimeLocal(value: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function timeFilterSummary() {
  if (!filters.from && !filters.to) return t('events.allTime')
  if (filters.from && filters.to) {
    const from = new Date(filters.from).getTime()
    const to = new Date(filters.to).getTime()
    if (Number.isFinite(from) && Number.isFinite(to)) {
      const days = Math.round((to - from) / (24 * 60 * 60 * 1000))
      if (days === 7) return t('events.last7Days')
    }
  }
  return t('events.customTimeRange')
}

function buildQuery(includePagination = true) {
  const params: Record<string, unknown> = {}
  const from = fromDateTimeLocal(filters.from)
  const to = fromDateTimeLocal(filters.to)
  if (from) params.from = from
  if (to) params.to = to
  if (includePagination) {
    params.limit = filters.limit
    params.offset = filters.offset
  }
  if (filters.tool) params.tool = filters.tool
  if (filters.binding_status) params.binding_status = filters.binding_status
  if (filters.q) params.q = filters.q
  const userId = selectedUser.value?.id ?? selectedUserId.value
  if (isAdmin.value && userId) params.user_id = userId
  return params
}

async function loadPage() {
  loading.value = true
  try {
    const summaryParams = buildQuery(false)
    const listParams = buildQuery(true)
    const [summaryRes, listRes] = await Promise.all([
      getEventSummary(summaryParams),
      listEvents(listParams),
    ])
    summary.value = summaryRes.data.data ?? null
    const listData = listRes.data.data
    rows.value = listData?.items ?? []
    total.value = listData?.total ?? 0
  } finally {
    loading.value = false
  }
}

async function searchUsers() {
  if (!isAdmin.value) return
  userSearchLoading.value = true
  try {
    const res = await searchEventUsers({ q: userSearch.value, limit: 20 })
    userOptions.value = res.data.data ?? []
  } finally {
    userSearchLoading.value = false
  }
}

async function selectUser(user: ToolUsageEventUserOption) {
  selectedUser.value = user
  selectedUserId.value = user.id
  userOptions.value = []
  filters.offset = 0
  replaceEventQuery()
  await loadPage()
}

async function clearSelectedUser() {
  selectedUser.value = null
  selectedUserId.value = null
  userSearch.value = ''
  userOptions.value = []
  filters.offset = 0
  replaceEventQuery()
  await loadPage()
}

async function applyFilters() {
  filters.offset = 0
  replaceEventQuery()
  await loadPage()
}

async function clearTimeRange() {
  filters.from = ''
  filters.to = ''
  filters.offset = 0
  replaceEventQuery()
  await loadPage()
}

async function nextPage() {
  if (!canGoNext.value) return
  filters.offset += filters.limit
  replaceEventQuery()
  await loadPage()
}

async function previousPage() {
  if (!canGoPrev.value) return
  filters.offset = Math.max(0, filters.offset - filters.limit)
  replaceEventQuery()
  await loadPage()
}

async function changePageSize() {
  filters.offset = 0
  replaceEventQuery()
  await loadPage()
}

async function openDetail(row: ToolUsageEventRow) {
  advancedDetailsOpen.value = false
  selectedEventId.value = row.id
  detailLoading.value = true
  try {
    const res = await getEventDetail(row.id)
    selectedEvent.value = res.data.data ?? null
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  advancedDetailsOpen.value = false
  selectedEventId.value = null
  selectedEvent.value = null
}

function handleAdvancedDetailsToggle(event: Event) {
  advancedDetailsOpen.value = (event.currentTarget as HTMLDetailsElement).open
}

function handleEventRowsMediaChange(event: MediaQueryListEvent) {
  desktopEventRows.value = event.matches
}

function formatDate(value?: string | null) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function formatCount(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toLocaleString()
}

function formatDecimal(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toFixed(2)
}

function formatTokenUsage(row: ToolUsageEventRow) {
  const input = row.input_tokens ?? 0
  const output = row.output_tokens ?? 0
  const totalTokens = input + output
  return totalTokens > 0 ? formatCount(totalTokens) : '—'
}

function bindingStatusLabel(value?: string | null) {
  if (value === 'bound') return t('events.bound')
  if (value === 'unbound') return t('events.unbound')
  return '—'
}

function shortSha(value?: string | null) {
  if (!value) return '—'
  return value.slice(0, 8)
}

onMounted(() => {
  eventRowsMediaQuery.addEventListener('change', handleEventRowsMediaChange)
  void loadPage()
})
onUnmounted(() => {
  eventRowsMediaQuery.removeEventListener('change', handleEventRowsMediaChange)
})
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex items-start justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">{{ t('events.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('events.subtitle') }}</p>
        </div>
        <button
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          :disabled="loading"
          @click="loadPage"
        >
          {{ loading ? t('events.loading') : t('events.refresh') }}
        </button>
      </div>

      <div class="grid gap-4 sm:grid-cols-4">
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.totalRecords') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.total_events) }}</div>
        </div>
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.linkedToCommit') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.bound_events) }}</div>
        </div>
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.needsLinking') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.unbound_events) }}</div>
        </div>
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.tools') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.tool_counts?.length ?? 0) }}</div>
        </div>
      </div>

      <div class="rounded-lg bg-white p-4 shadow">
        <div class="flex items-center justify-between gap-3 md:hidden">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('events.filters') }}</h2>
            <p class="mt-1 text-xs text-gray-500">{{ t('events.filtersHelp') }}</p>
          </div>
          <button
            class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700"
            type="button"
            :aria-expanded="mobileFiltersOpen"
            aria-controls="events-filter-panel"
            @click="mobileFiltersOpen = !mobileFiltersOpen"
          >
            {{ mobileFiltersOpen ? t('events.hideFilters') : t('events.showFilters') }}
          </button>
        </div>
        <div class="mt-3 flex flex-wrap gap-2 md:hidden" aria-label="Active filters">
          <span
            v-for="badge in filterSummaryBadges"
            :key="badge"
            class="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700"
          >
            {{ badge }}
          </span>
        </div>

        <div
          id="events-filter-panel"
          class="mt-3 md:mt-0"
          :class="mobileFiltersOpen ? 'block' : 'hidden md:block'"
        >
        <div class="grid gap-3 md:grid-cols-6">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.from') }}
            <input
              v-model="filters.from"
              type="datetime-local"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.to') }}
            <input
              v-model="filters.to"
              type="datetime-local"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.tool') }}
            <select v-model="filters.tool" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700">
              <option value="">{{ t('events.all') }}</option>
              <option value="claude">Claude</option>
              <option value="codex">Codex</option>
              <option value="kiro">Kiro</option>
            </select>
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.binding') }}
            <select v-model="filters.binding_status" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700">
              <option value="">{{ t('events.all') }}</option>
              <option value="bound">{{ t('events.bound') }}</option>
              <option value="unbound">{{ t('events.unbound') }}</option>
            </select>
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500 md:col-span-2">
            {{ t('events.search') }}
            <input
              v-model="filters.q"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              :placeholder="t('events.searchPlaceholder')"
            />
          </label>
        </div>

        <div v-if="isAdmin" class="mt-3 border-t border-gray-100 pt-3">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.user') }}
            <div class="mt-1 flex gap-2">
              <input
                v-model="userSearch"
                data-testid="event-user-search"
                class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
                :placeholder="t('events.userSearchPlaceholder')"
              />
              <button
                data-testid="event-user-search-button"
                class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                :disabled="userSearchLoading"
                @click="searchUsers"
              >
                {{ userSearchLoading ? t('events.searching') : t('events.search') }}
              </button>
            </div>
          </label>
          <div v-if="selectedUser || selectedUserId" class="mt-2 flex items-center justify-between gap-3 rounded-md border border-gray-200 px-3 py-2 text-sm text-gray-700">
            <span class="min-w-0 truncate">
              <template v-if="selectedUser">{{ userLabel(selectedUser) }} · {{ userMeta(selectedUser) }}</template>
              <template v-else>{{ t('events.selectedUserId') }} #{{ selectedUserId }}</template>
            </span>
            <button class="text-xs font-medium text-gray-500 hover:text-gray-900" @click="clearSelectedUser">{{ t('events.clear') }}</button>
          </div>
          <div v-if="userOptions.length > 0" class="mt-2 divide-y divide-gray-100 rounded-md border border-gray-200 bg-white">
            <button
              v-for="option in userOptions"
              :key="option.id"
              :data-testid="`event-user-option-${option.id}`"
              class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-50"
              @click="selectUser(option)"
            >
              <span class="font-medium text-gray-900">{{ userLabel(option) }}</span>
              <span class="ml-2 text-xs text-gray-500"> · {{ userMeta(option) }}</span>
            </button>
          </div>
        </div>

        <div class="mt-3 flex justify-end gap-2">
          <button class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50" @click="clearTimeRange">{{ t('events.clearTime') }}</button>
          <button class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white" @click="applyFilters">{{ t('events.applyFilters') }}</button>
        </div>
        </div>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('events.recentUsage') }}</h2>
          <div class="flex items-center gap-2 text-xs text-gray-500">
            <span>{{ total }} {{ t('events.totalSuffix') }}</span>
            <select
              v-model.number="filters.limit"
              data-testid="events-page-size"
              class="rounded-md border border-gray-300 px-2 py-1 text-xs"
              @change="changePageSize"
            >
              <option :value="20">20</option>
              <option :value="50">50</option>
              <option :value="100">100</option>
            </select>
            <button
              data-testid="events-prev-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoPrev || loading"
              @click="previousPage"
            >
              {{ t('events.prev') }}
            </button>
            <span>{{ t('events.page') }} {{ currentPage }} / {{ totalPages }}</span>
            <button
              data-testid="events-next-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoNext || loading"
              @click="nextPage"
            >
              {{ t('events.next') }}
            </button>
          </div>
        </div>

        <div v-if="showMobileEventRows" class="mt-3 space-y-3 md:hidden" data-event-list="mobile">
          <button
            v-for="row in rows"
            :key="row.id"
            data-event-row="mobile"
            class="block w-full rounded-lg border border-gray-100 bg-white p-4 text-left shadow-sm transition hover:border-gray-200 focus:outline-none focus:ring-2 focus:ring-blue-500"
            type="button"
            @click="openDetail(row)"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900">{{ row.tool || '—' }}</div>
                <div class="mt-1 truncate text-xs text-gray-500">{{ formatDate(row.observed_end_at) }}</div>
              </div>
              <span
                class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium"
                :class="row.binding_status === 'bound' ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
              >
                {{ bindingStatusLabel(row.binding_status) }}
              </span>
            </div>
            <dl class="mt-3 grid grid-cols-2 gap-3 text-xs">
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('events.repository') }}</dt>
                <dd class="mt-1 truncate text-gray-800">{{ row.repo_name || '—' }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('events.tokenUsage') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatTokenUsage(row) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('events.credits') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatDecimal(row.credit_usage) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('events.requests') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatCount(row.request_count) }}</dd>
              </div>
              <div class="col-span-2">
                <dt class="text-gray-400">{{ t('events.commit') }}</dt>
                <dd class="mt-1 break-all font-mono text-gray-800">{{ shortSha(row.commit_sha) }}</dd>
              </div>
              <div v-if="isAdmin" class="col-span-2">
                <dt class="text-gray-400">{{ t('events.user') }}</dt>
                <dd class="mt-1 truncate text-gray-800">{{ row.username || '—' }}</dd>
              </div>
            </dl>
            <div class="mt-3 border-t border-gray-100 pt-3 text-sm font-medium text-blue-700">
              {{ t('events.viewDetails') }}
            </div>
          </button>
        </div>

        <div v-if="showDesktopEventRows" class="mt-3 hidden overflow-x-auto md:block" data-event-list="desktop">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">{{ t('events.observed') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('events.tool') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('events.repository') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('events.codeLink') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('events.tokenUsage') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('events.credits') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('events.requests') }}</th>
                <th v-if="isAdmin" class="px-3 py-2 text-left font-medium">{{ t('events.user') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr
                v-for="row in rows"
                :key="row.id"
                data-event-row="desktop"
                class="cursor-pointer hover:bg-gray-50"
                role="button"
                tabindex="0"
                @click="openDetail(row)"
                @keydown.enter.prevent="openDetail(row)"
                @keydown.space.prevent="openDetail(row)"
              >
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ formatDate(row.observed_end_at) }}</td>
                <td class="px-3 py-2 text-gray-900">{{ row.tool }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.repo_name }}</td>
                <td class="px-3 py-2 text-gray-700">
                  <div class="font-medium" :class="row.binding_status === 'bound' ? 'text-emerald-700' : 'text-amber-700'">
                    {{ bindingStatusLabel(row.binding_status) }}
                  </div>
                  <div class="mt-1 font-mono text-xs text-gray-500">{{ shortSha(row.commit_sha) }}</div>
                </td>
                <td class="px-3 py-2 text-gray-700">{{ formatTokenUsage(row) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatDecimal(row.credit_usage) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatCount(row.request_count) }}</td>
                <td v-if="isAdmin" class="px-3 py-2 text-gray-700">{{ row.username || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="rows.length === 0" class="mt-3 text-sm text-gray-400">{{ t('events.empty') }}</div>
      </div>
    </div>

    <div v-if="selectedEventId != null" class="fixed inset-0 z-40">
      <button
        class="absolute inset-0 bg-slate-950/20"
        type="button"
        :aria-label="t('events.close')"
        @click="closeDetail"
      />
      <aside
        ref="eventDetailDialog"
        class="absolute inset-y-0 right-0 w-full max-w-xl border-l border-gray-200 bg-white shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="event-detail-title"
        tabindex="-1"
        @keydown="handleDetailKeydown"
      >
      <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4">
        <div>
          <h2 id="event-detail-title" class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('events.recordDetail') }}</h2>
          <p class="mt-1 text-xs text-gray-400">ID {{ selectedEventId }}</p>
        </div>
        <button class="rounded border border-gray-200 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50" type="button" @click="closeDetail">{{ t('events.close') }}</button>
      </div>

      <div v-if="detailLoading" class="p-5 text-sm text-gray-500">{{ t('events.loadingDetail') }}</div>
      <div v-else-if="selectedEvent" class="space-y-5 overflow-y-auto p-5 text-sm text-gray-700">
        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">{{ t('events.basic') }}</h3>
          <dl class="mt-2 space-y-2">
            <div class="flex justify-between gap-4"><dt>{{ t('events.tool') }}</dt><dd>{{ selectedEvent.tool }}</dd></div>
            <div class="flex justify-between gap-4"><dt>{{ t('events.repository') }}</dt><dd>{{ selectedEvent.repo_name || '—' }}</dd></div>
            <div class="flex justify-between gap-4"><dt>{{ t('events.observedAt') }}</dt><dd>{{ formatDate(selectedEvent.observed_end_at) }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>{{ t('events.user') }}</dt><dd>{{ selectedEvent.username || '—' }}</dd></div>
          </dl>
        </div>

        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">{{ t('events.usage') }}</h3>
          <dl class="mt-2 grid gap-2 sm:grid-cols-2">
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">{{ t('events.input') }}</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.input_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">{{ t('events.output') }}</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.output_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">{{ t('events.cache') }}</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.cached_input_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">{{ t('events.reasoning') }}</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.reasoning_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">{{ t('events.credits') }}</dt><dd class="mt-1 font-medium text-gray-900">{{ formatDecimal(selectedEvent.credit_usage) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">{{ t('events.requests') }}</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.request_count) }}</dd></div>
          </dl>
        </div>

        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">{{ t('events.codeLink') }}</h3>
          <dl class="mt-2 space-y-2">
            <div class="flex justify-between gap-4"><dt>{{ t('events.codeStatus') }}</dt><dd>{{ bindingStatusLabel(selectedEvent.binding_status) }}</dd></div>
            <div class="flex justify-between gap-4"><dt>{{ t('events.commit') }}</dt><dd class="font-mono">{{ selectedEvent.commit_sha || '—' }}</dd></div>
            <div class="flex justify-between gap-4"><dt>{{ t('events.capturedAt') }}</dt><dd>{{ formatDate(selectedEvent.checkpoint_captured_at || null) }}</dd></div>
          </dl>
          <div class="mt-3">
            <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.matchedPrs') }}</div>
            <ul v-if="selectedEvent.matched_prs.length > 0" class="mt-2 space-y-2">
              <li v-for="pr in selectedEvent.matched_prs" :key="pr.pr_record_id" class="rounded border border-gray-100 p-3">
                <div class="font-medium text-gray-900">#{{ pr.scm_pr_id }} {{ pr.title }}</div>
                <div class="mt-1 text-xs text-gray-500">{{ pr.status }}</div>
              </li>
            </ul>
            <div v-else class="mt-2 text-sm text-gray-500">{{ t('events.noMatchedPrs') }}</div>
          </div>
        </div>

        <details class="rounded-md border border-gray-200 p-4" @toggle="handleAdvancedDetailsToggle">
          <summary class="cursor-pointer text-xs font-semibold uppercase tracking-wide text-gray-500">{{ t('events.advancedData') }}</summary>
          <dl class="mt-3 space-y-2">
            <div class="flex justify-between gap-4"><dt>{{ t('events.workspace') }}</dt><dd class="font-mono">{{ selectedEvent.workspace_id }}</dd></div>
            <div class="flex justify-between gap-4"><dt>{{ t('events.toolSession') }}</dt><dd class="font-mono">{{ selectedEvent.tool_session_id }}</dd></div>
            <div class="flex justify-between gap-4"><dt>{{ t('events.toolEvent') }}</dt><dd class="font-mono">{{ selectedEvent.tool_event_id || '—' }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>{{ t('events.dedupeKey') }}</dt><dd class="font-mono">{{ selectedEvent.dedupe_key }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>{{ t('events.source') }}</dt><dd>{{ selectedEvent.source_basename }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>{{ t('events.rawPath') }}</dt><dd class="break-all text-right font-mono">{{ selectedEvent.raw_source_path || '—' }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>{{ t('events.rawLocator') }}</dt><dd class="font-mono">{{ selectedEvent.raw_source_locator || '—' }}</dd></div>
          </dl>

          <div v-if="isAdmin && selectedEvent.raw_payload" class="mt-4">
            <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">{{ t('events.rawPayload') }}</h3>
            <pre v-if="advancedDetailsOpen" class="mt-2 overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs text-gray-100">{{ formattedRawPayload }}</pre>
          </div>
        </details>
      </div>
      </aside>
    </div>
  </AppLayout>
</template>
