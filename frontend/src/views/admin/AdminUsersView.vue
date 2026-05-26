<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { listAdminUsers, revealAdminUserRelayPassword } from '@/api/adminUsers'
import type { AdminUser } from '@/types'

const loading = ref(false)
const error = ref('')
const rows = ref<AdminUser[]>([])
const total = ref(0)
const copiedState = reactive<Record<number, string>>({})
let searchTimer: ReturnType<typeof window.setTimeout> | undefined

const filters = reactive({
  q: '',
  page: 1,
  page_size: 20,
})

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.page_size)))
const canGoPrev = computed(() => filters.page > 1)
const canGoNext = computed(() => filters.page < totalPages.value)

async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const res = await listAdminUsers({
      q: filters.q,
      page: filters.page,
      page_size: filters.page_size,
    })
    const data = res.data.data
    rows.value = data?.items ?? []
    total.value = data?.total ?? 0
    filters.page = data?.page ?? filters.page
    filters.page_size = data?.page_size ?? filters.page_size
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || 'Failed to load users.'
    rows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function clearSearchTimer() {
  if (searchTimer) {
    window.clearTimeout(searchTimer)
    searchTimer = undefined
  }
}

async function applySearch() {
  clearSearchTimer()
  filters.page = 1
  await loadUsers()
}

async function changePageSize() {
  filters.page = 1
  await loadUsers()
}

async function previousPage() {
  if (!canGoPrev.value) return
  filters.page -= 1
  await loadUsers()
}

async function nextPage() {
  if (!canGoNext.value) return
  filters.page += 1
  await loadUsers()
}

function formatDate(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function displayRelayUserID(user: AdminUser) {
  return user.relay_user_id == null ? '-' : String(user.relay_user_id)
}

async function copyEncrypted(user: AdminUser) {
  if (!user.relay_auth_password) {
    copiedState[user.id] = 'No encrypted password'
    return
  }
  try {
    await navigator.clipboard.writeText(user.relay_auth_password)
    copiedState[user.id] = 'Copied encrypted'
  } catch (err: any) {
    copiedState[user.id] = err.message || 'Copy failed'
  }
}

async function copyPlaintext(user: AdminUser) {
  copiedState[user.id] = ''
  try {
    const res = await revealAdminUserRelayPassword(user.id)
    const password = res.data.data?.password || ''
    if (!password) {
      copiedState[user.id] = 'No plaintext returned'
      return
    }
    await navigator.clipboard.writeText(password)
    copiedState[user.id] = 'Copied plaintext'
  } catch (err: any) {
    copiedState[user.id] = err.response?.data?.message || err.message || 'Copy failed'
  }
}

watch(
  () => filters.q,
  () => {
    clearSearchTimer()
    searchTimer = window.setTimeout(() => {
      void applySearch()
    }, 300)
  }
)

onMounted(loadUsers)
onBeforeUnmount(clearSearchTimer)
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">Admin Users</h1>
          <p class="mt-1 text-sm text-gray-500">Inspect local users and copy stored relay credentials when needed.</p>
        </div>
        <button
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          :disabled="loading"
          @click="loadUsers"
        >
          {{ loading ? 'Loading...' : 'Refresh' }}
        </button>
      </div>

      <div class="rounded-lg bg-white p-4 shadow">
        <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_120px_auto]">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            Search
            <input
              v-model="filters.q"
              data-testid="admin-users-search"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              placeholder="username, email, local id, relay user id"
              @keyup.enter="applySearch"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            Page Size
            <select
              v-model.number="filters.page_size"
              data-testid="admin-users-page-size"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              @change="changePageSize"
            >
              <option :value="10">10</option>
              <option :value="20">20</option>
              <option :value="50">50</option>
              <option :value="100">100</option>
            </select>
          </label>
          <div class="flex items-end">
            <button
              data-testid="admin-users-search-button"
              class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
              :disabled="loading"
              @click="applySearch"
            >
              Search
            </button>
          </div>
        </div>
        <p v-if="error" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Local Users</h2>
          <div class="flex items-center gap-2 text-xs text-gray-500">
            <span>{{ total }} total</span>
            <button
              data-testid="admin-users-prev-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoPrev || loading"
              @click="previousPage"
            >
              Prev
            </button>
            <span>Page {{ filters.page }} / {{ totalPages }}</span>
            <button
              data-testid="admin-users-next-page"
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
                <th class="px-3 py-2 text-left font-medium">ID</th>
                <th class="px-3 py-2 text-left font-medium">Username</th>
                <th class="px-3 py-2 text-left font-medium">Email</th>
                <th class="px-3 py-2 text-left font-medium">Role</th>
                <th class="px-3 py-2 text-left font-medium">Auth Source</th>
                <th class="px-3 py-2 text-left font-medium">Relay User ID</th>
                <th class="px-3 py-2 text-left font-medium">Relay Auth Password</th>
                <th class="px-3 py-2 text-left font-medium">Created</th>
                <th class="px-3 py-2 text-left font-medium">Updated</th>
                <th class="px-3 py-2 text-left font-medium">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="row in rows" :key="row.id">
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ row.id }}</td>
                <td class="px-3 py-2 font-medium text-gray-900">{{ row.username }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.email }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.role }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.auth_source }}</td>
                <td class="px-3 py-2 text-gray-700">{{ displayRelayUserID(row) }}</td>
                <td class="max-w-sm break-all px-3 py-2 font-mono text-xs text-gray-700">{{ row.relay_auth_password || '-' }}</td>
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ formatDate(row.created_at) }}</td>
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ formatDate(row.updated_at) }}</td>
                <td class="whitespace-nowrap px-3 py-2">
                  <div class="flex flex-col gap-1">
                    <button
                      :data-testid="`copy-encrypted-${row.id}`"
                      class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="!row.relay_auth_password"
                      @click="copyEncrypted(row)"
                    >
                      Copy encrypted
                    </button>
                    <button
                      :data-testid="`copy-plaintext-${row.id}`"
                      class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="!row.relay_auth_password"
                      @click="copyPlaintext(row)"
                    >
                      Copy plaintext
                    </button>
                    <span v-if="copiedState[row.id]" class="text-xs text-gray-500">{{ copiedState[row.id] }}</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="mt-3 text-sm text-gray-400">No users match current filters.</div>
      </div>
    </div>
  </AppLayout>
</template>
