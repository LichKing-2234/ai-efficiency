<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { getEventDetail, getEventSummary, listEvents, searchEventUsers } from '@/api/events'
import { useAuthStore } from '@/stores/auth'
import type {
  ToolUsageEventDetail,
  ToolUsageEventRow,
  ToolUsageEventSummary,
  ToolUsageEventUserOption,
} from '@/types'

const auth = useAuthStore()

const loading = ref(true)
const summary = ref<ToolUsageEventSummary | null>(null)
const rows = ref<ToolUsageEventRow[]>([])
const total = ref(0)
const detailLoading = ref(false)
const selectedEvent = ref<ToolUsageEventDetail | null>(null)
const selectedEventId = ref<number | null>(null)
const userSearch = ref('')
const userOptions = ref<ToolUsageEventUserOption[]>([])
const selectedUser = ref<ToolUsageEventUserOption | null>(null)
const userSearchLoading = ref(false)

const filters = reactive({
  from: toDateTimeLocal(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000)),
  to: toDateTimeLocal(new Date()),
  tool: '',
  binding_status: '',
  q: '',
  limit: 20,
  offset: 0,
})

const isAdmin = computed(() => auth.isAdmin)
const currentPage = computed(() => Math.floor(filters.offset / filters.limit) + 1)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.limit)))
const canGoPrev = computed(() => filters.offset > 0)
const canGoNext = computed(() => filters.offset + filters.limit < total.value)

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
  if (isAdmin.value && selectedUser.value) params.user_id = selectedUser.value.id
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
  userOptions.value = []
  filters.offset = 0
  await loadPage()
}

async function clearSelectedUser() {
  selectedUser.value = null
  userSearch.value = ''
  userOptions.value = []
  filters.offset = 0
  await loadPage()
}

async function applyFilters() {
  filters.offset = 0
  await loadPage()
}

async function clearTimeRange() {
  filters.from = ''
  filters.to = ''
  filters.offset = 0
  await loadPage()
}

async function nextPage() {
  if (!canGoNext.value) return
  filters.offset += filters.limit
  await loadPage()
}

async function previousPage() {
  if (!canGoPrev.value) return
  filters.offset = Math.max(0, filters.offset - filters.limit)
  await loadPage()
}

async function changePageSize() {
  filters.offset = 0
  await loadPage()
}

async function openDetail(row: ToolUsageEventRow) {
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

function shortSha(value?: string | null) {
  if (!value) return '—'
  return value.slice(0, 8)
}

onMounted(loadPage)
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex items-start justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">Events</h1>
          <p class="mt-1 text-sm text-gray-500">Browse backend-ingested tool usage events across repos.</p>
        </div>
        <button
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          :disabled="loading"
          @click="loadPage"
        >
          {{ loading ? 'Loading...' : 'Refresh' }}
        </button>
      </div>

      <div class="grid gap-4 sm:grid-cols-4">
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">Total Events</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.total_events) }}</div>
        </div>
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">Bound to Commit</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.bound_events) }}</div>
        </div>
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">Unbound</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.unbound_events) }}</div>
        </div>
        <div class="rounded-lg bg-white p-4 shadow">
          <div class="text-xs uppercase tracking-wide text-gray-400">Tools</div>
          <div class="mt-2 text-2xl font-semibold text-gray-900">{{ formatCount(summary?.tool_counts?.length ?? 0) }}</div>
        </div>
      </div>

      <div class="rounded-lg bg-white p-4 shadow">
        <div class="grid gap-3 md:grid-cols-6">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            From
            <input
              v-model="filters.from"
              type="datetime-local"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            To
            <input
              v-model="filters.to"
              type="datetime-local"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            Tool
            <select v-model="filters.tool" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700">
              <option value="">All</option>
              <option value="claude">Claude</option>
              <option value="codex">Codex</option>
              <option value="kiro">Kiro</option>
            </select>
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            Binding
            <select v-model="filters.binding_status" class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700">
              <option value="">All</option>
              <option value="bound">Bound</option>
              <option value="unbound">Unbound</option>
            </select>
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500 md:col-span-2">
            Search
            <input
              v-model="filters.q"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              placeholder="tool session, event id, dedupe key, commit, source"
            />
          </label>
        </div>

        <div v-if="isAdmin" class="mt-3 border-t border-gray-100 pt-3">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            User
            <div class="mt-1 flex gap-2">
              <input
                v-model="userSearch"
                data-testid="event-user-search"
                class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
                placeholder="Search email or username"
              />
              <button
                data-testid="event-user-search-button"
                class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                :disabled="userSearchLoading"
                @click="searchUsers"
              >
                {{ userSearchLoading ? 'Searching...' : 'Search' }}
              </button>
            </div>
          </label>
          <div v-if="selectedUser" class="mt-2 flex items-center justify-between rounded-md border border-gray-200 px-3 py-2 text-sm text-gray-700">
            <span>{{ userLabel(selectedUser) }} · {{ userMeta(selectedUser) }}</span>
            <button class="text-xs font-medium text-gray-500 hover:text-gray-900" @click="clearSelectedUser">Clear</button>
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
          <button class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50" @click="clearTimeRange">Clear Time</button>
          <button class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white" @click="applyFilters">Apply Filters</button>
        </div>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Event Records</h2>
          <div class="flex items-center gap-2 text-xs text-gray-500">
            <span>{{ total }} total</span>
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
              Prev
            </button>
            <span>Page {{ currentPage }} / {{ totalPages }}</span>
            <button
              data-testid="events-next-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoNext || loading"
              @click="nextPage"
            >
              Next
            </button>
          </div>
        </div>

        <div v-if="rows.length > 0" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Observed</th>
                <th class="px-3 py-2 text-left font-medium">Tool</th>
                <th class="px-3 py-2 text-left font-medium">Source</th>
                <th class="px-3 py-2 text-left font-medium">Repo</th>
                <th class="px-3 py-2 text-left font-medium">Binding</th>
                <th class="px-3 py-2 text-left font-medium">Commit</th>
                <th class="px-3 py-2 text-left font-medium">Input</th>
                <th class="px-3 py-2 text-left font-medium">Output</th>
                <th class="px-3 py-2 text-left font-medium">Cache</th>
                <th class="px-3 py-2 text-left font-medium">Credits</th>
                <th class="px-3 py-2 text-left font-medium">Requests</th>
                <th v-if="isAdmin" class="px-3 py-2 text-left font-medium">User</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr
                v-for="row in rows"
                :key="row.id"
                class="cursor-pointer hover:bg-gray-50"
                @click="openDetail(row)"
              >
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ formatDate(row.observed_end_at) }}</td>
                <td class="px-3 py-2 text-gray-900">{{ row.tool }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.source_basename }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.repo_name }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.binding_status }}</td>
                <td class="px-3 py-2 font-mono text-gray-700">{{ shortSha(row.commit_sha) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatCount(row.input_tokens) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatCount(row.output_tokens) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatCount(row.cached_input_tokens) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatDecimal(row.credit_usage) }}</td>
                <td class="px-3 py-2 text-gray-700">{{ formatCount(row.request_count) }}</td>
                <td v-if="isAdmin" class="px-3 py-2 text-gray-700">{{ row.username || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="mt-3 text-sm text-gray-400">No events match current filters.</div>
      </div>
    </div>

    <div v-if="selectedEventId != null" class="fixed inset-y-0 right-0 z-40 w-full max-w-xl border-l border-gray-200 bg-white shadow-xl">
      <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4">
        <div>
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Event Detail</h2>
          <p class="mt-1 text-xs text-gray-400">ID {{ selectedEventId }}</p>
        </div>
        <button class="rounded border border-gray-200 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50" @click="closeDetail">Close</button>
      </div>

      <div v-if="detailLoading" class="p-5 text-sm text-gray-500">Loading detail...</div>
      <div v-else-if="selectedEvent" class="space-y-5 overflow-y-auto p-5 text-sm text-gray-700">
        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">Basic</h3>
          <dl class="mt-2 space-y-2">
            <div class="flex justify-between gap-4"><dt>Tool</dt><dd>{{ selectedEvent.tool }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Workspace</dt><dd class="font-mono">{{ selectedEvent.workspace_id }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Tool Session</dt><dd class="font-mono">{{ selectedEvent.tool_session_id }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Tool Event</dt><dd class="font-mono">{{ selectedEvent.tool_event_id || '—' }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Dedupe Key</dt><dd class="font-mono">{{ selectedEvent.dedupe_key }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>User</dt><dd>{{ selectedEvent.username || '—' }}</dd></div>
          </dl>
        </div>

        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">Usage</h3>
          <dl class="mt-2 grid gap-2 sm:grid-cols-2">
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">Input</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.input_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">Output</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.output_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">Cache</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.cached_input_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">Reasoning</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.reasoning_tokens) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">Credits</dt><dd class="mt-1 font-medium text-gray-900">{{ formatDecimal(selectedEvent.credit_usage) }}</dd></div>
            <div class="rounded border border-gray-100 p-3"><dt class="text-gray-400">Requests</dt><dd class="mt-1 font-medium text-gray-900">{{ formatCount(selectedEvent.request_count) }}</dd></div>
          </dl>
        </div>

        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">Binding</h3>
          <dl class="mt-2 space-y-2">
            <div class="flex justify-between gap-4"><dt>Status</dt><dd>{{ selectedEvent.binding_status }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Commit Checkpoint</dt><dd>{{ selectedEvent.commit_checkpoint_id ?? '—' }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Commit</dt><dd class="font-mono">{{ selectedEvent.commit_sha || '—' }}</dd></div>
            <div class="flex justify-between gap-4"><dt>Captured At</dt><dd>{{ formatDate(selectedEvent.checkpoint_captured_at || null) }}</dd></div>
          </dl>
          <div class="mt-3">
            <div class="text-xs uppercase tracking-wide text-gray-400">Matched PRs</div>
            <ul v-if="selectedEvent.matched_prs.length > 0" class="mt-2 space-y-2">
              <li v-for="pr in selectedEvent.matched_prs" :key="pr.pr_record_id" class="rounded border border-gray-100 p-3">
                <div class="font-medium text-gray-900">#{{ pr.scm_pr_id }} {{ pr.title }}</div>
                <div class="mt-1 text-xs text-gray-500">{{ pr.status }}</div>
              </li>
            </ul>
            <div v-else class="mt-2 text-sm text-gray-500">No matched PRs.</div>
          </div>
        </div>

        <div>
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">Source</h3>
          <dl class="mt-2 space-y-2">
            <div class="flex justify-between gap-4"><dt>Basename</dt><dd>{{ selectedEvent.source_basename }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>Path</dt><dd class="break-all text-right font-mono">{{ selectedEvent.raw_source_path || '—' }}</dd></div>
            <div v-if="isAdmin" class="flex justify-between gap-4"><dt>Locator</dt><dd class="font-mono">{{ selectedEvent.raw_source_locator || '—' }}</dd></div>
          </dl>
        </div>

        <div v-if="isAdmin && selectedEvent.raw_payload">
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-400">Raw Payload</h3>
          <pre class="mt-2 overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs text-gray-100">{{ JSON.stringify(selectedEvent.raw_payload, null, 2) }}</pre>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
