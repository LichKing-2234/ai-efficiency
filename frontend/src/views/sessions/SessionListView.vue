<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { listSessions } from '@/api/session'
import { useAuthStore } from '@/stores/auth'
import type { Session, SessionListParams } from '@/types'

const router = useRouter()
const auth = useAuthStore()
const sessions = ref<Session[]>([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = 20
const statusFilter = ref('')
const repoQuery = ref('')
const branchFilter = ref('')
const ownerScope = ref<'all' | 'mine' | 'unowned'>('all')
const isAdmin = computed(() => auth.isAdmin)

async function fetchSessions() {
  loading.value = true
  try {
    const params: SessionListParams = { page: page.value, page_size: pageSize }
    if (statusFilter.value) params.status = statusFilter.value
    if (repoQuery.value.trim()) params.repo_query = repoQuery.value.trim()
    if (branchFilter.value.trim()) params.branch = branchFilter.value.trim()
    params.owner_scope = isAdmin.value ? ownerScope.value : 'mine'
    const res = await listSessions(params)
    sessions.value = res.data.data?.items ?? []
    total.value = res.data.data?.total ?? 0
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 1
  fetchSessions()
}

function resetFilters() {
  statusFilter.value = ''
  repoQuery.value = ''
  branchFilter.value = ''
  ownerScope.value = 'all'
  page.value = 1
  fetchSessions()
}

function formatDate(d: string | null) {
  if (!d) return '—'
  return new Date(d).toLocaleString()
}

function duration(s: Session) {
  if (!s.started_at) return '—'
  const start = new Date(s.started_at).getTime()
  const end = s.ended_at ? new Date(s.ended_at).getTime() : Date.now()
  const mins = Math.round((end - start) / 60000)
  if (mins < 60) return `${mins}m`
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return `${h}h ${m}m`
}

function statusClass(status: string) {
  switch (status) {
    case 'active': return 'bg-green-100 text-green-800'
    case 'completed': return 'bg-gray-100 text-gray-800'
    case 'abandoned': return 'bg-yellow-100 text-yellow-800'
    default: return 'bg-gray-100 text-gray-600'
  }
}

function invocationCount(s: Session) {
  return Array.isArray(s.tool_invocations) ? s.tool_invocations.length : 0
}

function toolSummary(s: Session) {
  if (!Array.isArray(s.tool_invocations) || s.tool_invocations.length === 0) return '—'
  const counts: Record<string, number> = {}
  for (const inv of s.tool_invocations) {
    counts[inv.tool] = (counts[inv.tool] || 0) + 1
  }
  return Object.entries(counts).map(([t, c]) => `${t}×${c}`).join(', ')
}

const totalPages = () => Math.ceil(total.value / pageSize)

onMounted(fetchSessions)
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <div class="flex items-center justify-between mb-6">
        <h1 class="text-xl font-semibold text-gray-900">Sessions</h1>
        <div class="flex items-center gap-3">
          <select
            v-model="statusFilter"
            name="status"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
            @change="page = 1; fetchSessions()"
          >
            <option value="">All Status</option>
            <option value="active">Active</option>
            <option value="completed">Completed</option>
            <option value="abandoned">Abandoned</option>
          </select>
          <input
            v-model="repoQuery"
            data-test="repo-query"
            name="repo_query"
            type="text"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
            placeholder="Filter by repo"
            @keyup.enter="applyFilters"
          />
          <input
            v-model="branchFilter"
            data-test="branch-filter"
            name="branch"
            type="text"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
            placeholder="Filter by branch"
            @keyup.enter="applyFilters"
          />
          <select
            v-if="isAdmin"
            v-model="ownerScope"
            data-test="owner-scope"
            name="owner_scope"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm"
          >
            <option value="all">All Owners</option>
            <option value="mine">My Sessions</option>
            <option value="unowned">Unowned</option>
          </select>
          <button
            type="button"
            data-testid="apply-session-filters"
            data-test="apply-filters"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
            @click="applyFilters"
          >
            Apply
          </button>
          <button
            type="button"
            data-test="reset-filters"
            class="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
            @click="resetFilters"
          >
            Reset
          </button>
        </div>
      </div>

      <div v-if="loading" class="text-center text-gray-500 py-12">Loading...</div>

      <div v-else-if="sessions.length === 0" class="rounded-lg bg-white p-12 shadow text-center">
        <p class="text-sm text-gray-500">No sessions found.</p>
        <p class="mt-2 text-xs text-gray-400">Sessions are created by ae-cli when developers start coding.</p>
      </div>

      <div v-else class="overflow-hidden rounded-lg bg-white shadow">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">ID</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Repo</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Branch</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Duration</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Tools</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Started</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <tr
              v-for="s in sessions"
              :key="s.id"
              class="hover:bg-gray-50 cursor-pointer"
              @click="router.push({ name: 'SessionDetail', params: { id: s.id } })"
            >
              <td class="px-4 py-3 text-sm font-mono text-gray-600">{{ String(s.id).slice(0, 8) }}</td>
              <td class="px-4 py-3 text-sm text-gray-900">{{ s.edges?.repo_config?.full_name ?? '—' }}</td>
              <td class="px-4 py-3 text-sm text-gray-600">{{ s.branch }}</td>
              <td class="px-4 py-3">
                <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(s.status)">
                  {{ s.status }}
                </span>
              </td>
              <td class="px-4 py-3 text-sm text-gray-600">{{ duration(s) }}</td>
              <td class="px-4 py-3 text-sm text-gray-600">{{ toolSummary(s) }}</td>
              <td class="px-4 py-3 text-sm text-gray-500">{{ formatDate(s.started_at) }}</td>
              <td class="px-4 py-3 text-sm">
                <button
                  class="text-indigo-600 hover:text-indigo-800"
                  @click.stop="router.push({ name: 'SessionDetail', params: { id: s.id } })"
                >View</button>
              </td>
            </tr>
          </tbody>
        </table>

        <div v-if="totalPages() > 1" class="flex items-center justify-between border-t border-gray-200 px-4 py-3">
          <p class="text-sm text-gray-500">{{ total }} sessions total</p>
          <div class="flex gap-2">
            <button
              :disabled="page <= 1"
              class="rounded px-3 py-1 text-sm border disabled:opacity-50"
              @click="page--; fetchSessions()"
            >Prev</button>
            <span class="px-2 py-1 text-sm text-gray-600">{{ page }} / {{ totalPages() }}</span>
            <button
              :disabled="page >= totalPages()"
              class="rounded px-3 py-1 text-sm border disabled:opacity-50"
              @click="page++; fetchSessions()"
            >Next</button>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
