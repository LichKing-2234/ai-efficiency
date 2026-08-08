<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getEventDetail, getEventSummary, listEvents, searchEventUsers } from '@/api/events'
import { useAuthStore } from '@/stores/auth'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'
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
const maxEventPageSize = 100

const loading = ref(true)
const pageError = ref('')
const summary = ref<ToolUsageEventSummary | null>(null)
const rows = ref<ToolUsageEventRow[]>([])
const total = ref(0)
const detailLoading = ref(false)
const detailError = ref('')
const selectedEvent = ref<ToolUsageEventDetail | null>(null)
const selectedEventId = ref<number | null>(null)
const mobileFiltersOpen = ref(false)
const desktopEventRows = useMediaQuery('(min-width: 768px)')
const advancedDetailSections = ref<string[]>([])
const userSearch = ref('')
const userOptions = ref<ToolUsageEventUserOption[]>([])
const selectedUser = ref<ToolUsageEventUserOption | null>(null)
const selectedUserId = ref<number | null>(queryNumber('user_id', 0) || null)
const userSearchLoading = ref(false)
const userSearchError = ref('')

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
const advancedDetailsOpen = computed(() => advancedDetailSections.value.includes('advanced'))
const formattedRawPayload = computed(() => JSON.stringify(selectedEvent.value?.raw_payload, null, 2))
const filterSummaryBadges = computed(() => {
  const badges = [timeFilterSummary()]
  badges.push(filters.tool ? t('events.toolSummary', { tool: filters.tool }) : t('events.allTools'))
  badges.push(filters.binding_status ? t('events.codeLinkSummary', { status: bindingStatusLabel(filters.binding_status) }) : t('events.allCodeLinks'))
  if (filters.q.trim()) badges.push(t('events.searchSummary', { query: filters.q.trim() }))
  if (isAdmin.value && selectedUserId.value) badges.push(t('events.userSummary', { id: selectedUserId.value }))
  return badges
})

function queryString(key: string) {
  const value = route.query[key]
  return typeof value === 'string' ? value : ''
}

function queryNumber(key: string, fallback: number) {
  const value = Number(queryString(key))
  if (!Number.isSafeInteger(value)) return fallback
  if (key === 'limit') {
    return value > 0 ? Math.min(value, maxEventPageSize) : fallback
  }
  return value >= 0 ? value : fallback
}

function normalizeRestoredPaginationQuery() {
  const next = { ...route.query }
  let changed = false
  const normalize = (key: 'limit' | 'offset', value: number, fallback: number) => {
    const raw = route.query[key]
    if (raw == null) return
    const normalized = value === fallback ? undefined : String(value)
    if (typeof raw === 'string' && raw === normalized) return
    changed = true
    if (normalized == null) delete next[key]
    else next[key] = normalized
  }

  normalize('limit', filters.limit, 20)
  normalize('offset', filters.offset, 0)
  if (changed) void router.replace({ query: next })
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
  pageError.value = ''
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
  } catch {
    pageError.value = t('events.loadFailed')
  } finally {
    loading.value = false
  }
}

async function searchUsers() {
  if (!isAdmin.value) return
  userSearchLoading.value = true
  userSearchError.value = ''
  userOptions.value = []
  try {
    const res = await searchEventUsers({ q: userSearch.value, limit: 20 })
    userOptions.value = res.data.data ?? []
  } catch {
    userSearchError.value = t('events.userSearchFailed')
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
  advancedDetailSections.value = []
  detailError.value = ''
  selectedEvent.value = null
  selectedEventId.value = row.id
  detailLoading.value = true
  try {
    const res = await getEventDetail(row.id)
    selectedEvent.value = res.data.data ?? null
  } catch {
    detailError.value = t('events.detailLoadFailed')
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  advancedDetailSections.value = []
  detailError.value = ''
  selectedEventId.value = null
  selectedEvent.value = null
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

function asEventRow(row: unknown) {
  return row as ToolUsageEventRow
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
  normalizeRestoredPaginationQuery()
  void loadPage()
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
        <ElButton :loading="loading" @click="loadPage">
          {{ t('events.refresh') }}
        </ElButton>
      </div>

      <ElAlert
        v-if="pageError"
        data-testid="events-load-error"
        type="error"
        :title="pageError"
        :closable="false"
        show-icon
      />

      <div class="grid gap-4 sm:grid-cols-4">
        <ElCard shadow="never">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.totalRecords') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.total_events) }}</div>
        </ElCard>
        <ElCard shadow="never">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.linkedToCommit') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.bound_events) }}</div>
        </ElCard>
        <ElCard shadow="never">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.needsLinking') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.unbound_events) }}</div>
        </ElCard>
        <ElCard shadow="never">
          <div class="text-xs uppercase tracking-wide text-gray-400">{{ t('events.tools') }}</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.tool_counts?.length ?? 0) }}</div>
        </ElCard>
      </div>

      <ElCard shadow="never">
        <div class="flex items-center justify-between gap-3 md:hidden">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('events.filters') }}</h2>
            <p class="mt-1 text-xs text-gray-500">{{ t('events.filtersHelp') }}</p>
          </div>
          <ElButton
            :aria-expanded="mobileFiltersOpen"
            aria-controls="events-filter-panel"
            @click="mobileFiltersOpen = !mobileFiltersOpen"
          >
            {{ mobileFiltersOpen ? t('events.hideFilters') : t('events.showFilters') }}
          </ElButton>
        </div>
        <div class="mt-3 flex flex-wrap gap-2 md:hidden" aria-label="Active filters">
          <ElTag
            v-for="badge in filterSummaryBadges"
            :key="badge"
            round
            type="info"
          >
            {{ badge }}
          </ElTag>
        </div>

        <div
          id="events-filter-panel"
          class="mt-3 md:mt-0"
          :class="mobileFiltersOpen ? 'block' : 'hidden md:block'"
        >
        <div class="grid gap-3 md:grid-cols-6">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.from') }}
            <ElDatePicker
              v-model="filters.from"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm"
              class="mt-1 !w-full"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.to') }}
            <ElDatePicker
              v-model="filters.to"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm"
              class="mt-1 !w-full"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.tool') }}
            <ElSelect v-model="filters.tool" class="mt-1 w-full" :teleported="false">
              <ElOption value="" :label="t('events.all')" />
              <ElOption value="claude" label="Claude" />
              <ElOption value="codex" label="Codex" />
              <ElOption value="kiro" label="Kiro" />
            </ElSelect>
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.binding') }}
            <ElSelect v-model="filters.binding_status" class="mt-1 w-full" :teleported="false">
              <ElOption value="" :label="t('events.all')" />
              <ElOption value="bound" :label="t('events.bound')" />
              <ElOption value="unbound" :label="t('events.unbound')" />
            </ElSelect>
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500 md:col-span-2">
            {{ t('events.search') }}
            <ElInput
              v-model="filters.q"
              class="mt-1"
              :placeholder="t('events.searchPlaceholder')"
              clearable
            />
          </label>
        </div>

        <div v-if="isAdmin" class="mt-3 border-t border-gray-100 pt-3">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('events.user') }}
            <div class="mt-1 flex gap-2">
              <ElInput
                v-model="userSearch"
                data-testid="event-user-search"
                :placeholder="t('events.userSearchPlaceholder')"
                clearable
              />
              <ElButton
                data-testid="event-user-search-button"
                :loading="userSearchLoading"
                @click="searchUsers"
              >
                {{ t('events.search') }}
              </ElButton>
            </div>
          </label>
          <div v-if="selectedUser || selectedUserId" class="mt-2 flex items-center justify-between gap-3 rounded-md border border-gray-200 px-3 py-2 text-sm text-gray-700">
            <span class="min-w-0 truncate">
              <template v-if="selectedUser">{{ userLabel(selectedUser) }} · {{ userMeta(selectedUser) }}</template>
              <template v-else>{{ t('events.selectedUserId') }} #{{ selectedUserId }}</template>
            </span>
            <ElButton link @click="clearSelectedUser">{{ t('events.clear') }}</ElButton>
          </div>
          <div v-if="userOptions.length > 0" class="mt-2 divide-y divide-gray-100 rounded-md border border-gray-200 bg-white">
            <ElButton
              v-for="option in userOptions"
              :key="option.id"
              :data-testid="`event-user-option-${option.id}`"
              text
              class="!m-0 !h-auto !w-full !justify-start !rounded-none !px-3 !py-2 !text-left"
              @click="selectUser(option)"
            >
              <span class="font-medium text-gray-900">{{ userLabel(option) }}</span>
              <span class="ml-2 text-xs text-gray-500"> · {{ userMeta(option) }}</span>
            </ElButton>
          </div>
          <ElAlert
            v-if="userSearchError"
            data-testid="event-user-search-error"
            class="mt-2"
            type="error"
            :title="userSearchError"
            :closable="false"
            show-icon
          />
        </div>

        <div class="mt-3 flex justify-end gap-2">
          <ElButton @click="clearTimeRange">{{ t('events.clearTime') }}</ElButton>
          <ElButton type="primary" @click="applyFilters">{{ t('events.applyFilters') }}</ElButton>
        </div>
        </div>
      </ElCard>

      <ElCard shadow="never">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('events.recentUsage') }}</h2>
          <div class="flex flex-wrap items-center justify-end gap-2 text-xs text-gray-500">
            <span>{{ total }} {{ t('events.totalSuffix') }}</span>
            <ElSelect
              v-model="filters.limit"
              data-testid="events-page-size"
              class="w-20"
              size="small"
              :teleported="false"
              @change="changePageSize"
            >
              <ElOption :value="20" label="20" />
              <ElOption :value="50" label="50" />
              <ElOption :value="100" label="100" />
            </ElSelect>
            <ElButton
              data-testid="events-prev-page"
              size="small"
              :disabled="!canGoPrev || loading"
              @click="previousPage"
            >
              {{ t('events.prev') }}
            </ElButton>
            <span>{{ t('events.page') }} {{ currentPage }} / {{ totalPages }}</span>
            <ElButton
              data-testid="events-next-page"
              size="small"
              :disabled="!canGoNext || loading"
              @click="nextPage"
            >
              {{ t('events.next') }}
            </ElButton>
          </div>
        </div>

        <div v-if="showMobileEventRows" class="mt-3 space-y-3 md:hidden" data-event-list="mobile">
          <ElButton
            v-for="row in rows"
            :key="row.id"
            data-event-row="mobile"
            class="!m-0 !h-auto !w-full !whitespace-normal !rounded-lg !border-gray-100 !bg-white !p-4 !text-left !shadow-sm"
            @click="openDetail(row)"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900">{{ row.tool || '—' }}</div>
                <div class="mt-1 truncate text-xs text-gray-500">{{ formatDate(row.observed_end_at) }}</div>
              </div>
              <ElTag :type="row.binding_status === 'bound' ? 'success' : 'warning'" round>
                {{ bindingStatusLabel(row.binding_status) }}
              </ElTag>
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
          </ElButton>
        </div>

        <div v-if="showDesktopEventRows" class="mt-3 hidden md:block" data-event-list="desktop">
          <ElTable :data="rows" stripe @row-click="openDetail">
            <ElTableColumn :label="t('events.observed')" min-width="180">
              <template #default="{ row }">{{ formatDate(row.observed_end_at) }}</template>
            </ElTableColumn>
            <ElTableColumn prop="tool" :label="t('events.tool')" min-width="90" />
            <ElTableColumn prop="repo_name" :label="t('events.repository')" min-width="180" />
            <ElTableColumn :label="t('events.codeLink')" min-width="120">
              <template #default="{ row }">
                <ElTag :type="row.binding_status === 'bound' ? 'success' : 'warning'" size="small">
                  {{ bindingStatusLabel(row.binding_status) }}
                </ElTag>
                <div class="mt-1 font-mono text-xs text-gray-500">{{ shortSha(row.commit_sha) }}</div>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('events.tokenUsage')" min-width="110">
              <template #default="{ row }">{{ formatTokenUsage(asEventRow(row)) }}</template>
            </ElTableColumn>
            <ElTableColumn :label="t('events.credits')" min-width="90">
              <template #default="{ row }">{{ formatDecimal(row.credit_usage) }}</template>
            </ElTableColumn>
            <ElTableColumn :label="t('events.requests')" min-width="90">
              <template #default="{ row }">{{ formatCount(row.request_count) }}</template>
            </ElTableColumn>
            <ElTableColumn v-if="isAdmin" prop="username" :label="t('events.user')" min-width="120" />
            <ElTableColumn fixed="right" width="105">
              <template #default="{ row }">
                <ElButton link type="primary" @click.stop="openDetail(asEventRow(row))">{{ t('events.viewDetails') }}</ElButton>
              </template>
            </ElTableColumn>
          </ElTable>
        </div>
        <ElEmpty v-if="!loading && !pageError && rows.length === 0" :description="t('events.empty')" />
      </ElCard>
    </div>

    <ElDrawer
      :model-value="detailOpen"
      data-testid="event-detail-drawer"
      :teleported="false"
      :size="'min(100vw, 36rem)'"
      :show-close="false"
      destroy-on-close
      @update:model-value="(value: boolean) => { if (!value) closeDetail() }"
    >
      <template #header>
        <div>
          <h2 id="event-detail-title" class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('events.recordDetail') }}</h2>
          <p class="mt-1 text-xs text-gray-400">ID {{ selectedEventId }}</p>
        </div>
        <ElButton size="small" @click="closeDetail">{{ t('events.close') }}</ElButton>
      </template>

      <div v-if="detailLoading" class="p-5 text-sm text-gray-500">{{ t('events.loadingDetail') }}</div>
      <ElAlert
        v-else-if="detailError"
        data-testid="event-detail-error"
        type="error"
        :title="detailError"
        :closable="false"
        show-icon
      />
      <div v-else-if="selectedEvent" class="space-y-5 text-sm text-gray-700">
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

        <ElCollapse
          v-model="advancedDetailSections"
          data-testid="event-advanced-data"
          class="rounded-md border border-gray-200 px-4"
        >
          <ElCollapseItem name="advanced">
            <template #title>
              <span class="text-xs font-semibold uppercase tracking-wide text-gray-500">{{ t('events.advancedData') }}</span>
            </template>
            <dl class="space-y-2">
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
          </ElCollapseItem>
        </ElCollapse>
      </div>
    </ElDrawer>
  </AppLayout>
</template>
