<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { listAdminUsers, revealAdminUserRelayPassword } from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import type { AdminUser } from '@/types'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const error = ref('')
const rows = ref<AdminUser[]>([])
const total = ref(0)
const copiedState = reactive<Record<number, string>>({})
const plaintextConfirmUserId = ref<number | null>(null)
let searchTimer: number | undefined

const filters = reactive({
  q: queryString('q'),
  page: queryNumber('page', 1),
  page_size: queryNumber('page_size', 20),
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
    replaceAdminUsersQuery()
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || t('adminUsers.loadFailed')
    rows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function queryString(key: string) {
  const value = route.query[key]
  return typeof value === 'string' ? value : ''
}

function queryNumber(key: string, fallback: number) {
  const value = Number(queryString(key))
  return Number.isFinite(value) && value > 0 ? value : fallback
}

function replaceAdminUsersQuery() {
  const query: Record<string, string> = {}
  if (filters.q.trim()) query.q = filters.q.trim()
  if (filters.page > 1) query.page = String(filters.page)
  if (filters.page_size !== 20) query.page_size = String(filters.page_size)
  void router.replace({ query })
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
  return new Date(value).toLocaleString(locale.value)
}

function relayMappingLabel(user: AdminUser) {
  return user.relay_user_id == null ? t('adminUsers.notMapped') : `${t('adminUsers.mapped')} #${user.relay_user_id}`
}

function accessStatusLabel(user: AdminUser) {
  return user.relay_auth_password ? t('adminUsers.configured') : t('adminUsers.missingRelayCredential')
}

async function copyEncrypted(user: AdminUser) {
  if (!user.relay_auth_password) {
    copiedState[user.id] = t('adminUsers.noEncryptedPassword')
    return
  }
  try {
    await navigator.clipboard.writeText(user.relay_auth_password)
    copiedState[user.id] = t('adminUsers.copiedEncrypted')
  } catch (err: any) {
    copiedState[user.id] = err.message || t('adminUsers.copyFailed')
  }
}

function requestPlaintextCopy(user: AdminUser) {
  copiedState[user.id] = ''
  plaintextConfirmUserId.value = user.id
}

async function confirmCopyPlaintext(user: AdminUser) {
  copiedState[user.id] = ''
  try {
    const res = await revealAdminUserRelayPassword(user.id)
    const password = res.data.data?.password || ''
    if (!password) {
      copiedState[user.id] = t('adminUsers.noPlaintextReturned')
      return
    }
    await navigator.clipboard.writeText(password)
    copiedState[user.id] = t('adminUsers.copiedPlaintext')
    plaintextConfirmUserId.value = null
  } catch (err: any) {
    copiedState[user.id] = err.response?.data?.message || err.message || t('adminUsers.copyFailed')
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
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">{{ t('nav.userManagement') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('adminUsers.subtitle') }}</p>
        </div>
        <button
          class="shrink-0 self-start whitespace-nowrap rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 sm:self-auto"
          :disabled="loading"
          @click="loadUsers"
        >
          {{ loading ? t('adminUsers.loading') : t('adminUsers.refresh') }}
        </button>
      </div>

      <div class="rounded-lg bg-white p-4 shadow">
        <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_120px_auto]">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.search') }}
            <input
              v-model="filters.q"
              data-testid="admin-users-search"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              :placeholder="t('adminUsers.searchPlaceholder')"
              @keyup.enter="applySearch"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.pageSize') }}
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
              {{ t('adminUsers.search') }}
            </button>
          </div>
        </div>
        <p v-if="error" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('adminUsers.localUsers') }}</h2>
          <div class="flex items-center gap-2 text-xs text-gray-500">
            <span>{{ total }} {{ t('adminUsers.totalSuffix') }}</span>
            <button
              data-testid="admin-users-prev-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoPrev || loading"
              @click="previousPage"
            >
              {{ t('adminUsers.prev') }}
            </button>
            <span>{{ t('adminUsers.page') }} {{ filters.page }} / {{ totalPages }}</span>
            <button
              data-testid="admin-users-next-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoNext || loading"
              @click="nextPage"
            >
              {{ t('adminUsers.next') }}
            </button>
          </div>
        </div>

        <div v-if="rows.length > 0" class="mt-3 space-y-3 md:hidden">
          <div v-for="row in rows" :key="row.id" class="rounded-lg border border-gray-100 bg-white p-4 shadow-sm">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="truncate font-medium text-gray-900">{{ row.username }}</div>
                <div class="truncate text-xs text-gray-500">{{ row.email }}</div>
                <div class="mt-1 font-mono text-[11px] text-gray-400">{{ t('adminUsers.localId') }} #{{ row.id }}</div>
              </div>
              <span
                class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium"
                :class="row.relay_auth_password ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
              >
                {{ accessStatusLabel(row) }}
              </span>
            </div>
            <dl class="mt-3 grid grid-cols-2 gap-3 text-xs">
              <div>
                <dt class="text-gray-400">{{ t('adminUsers.role') }}</dt>
                <dd class="mt-1 text-gray-800">{{ row.role }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('adminUsers.authSource') }}</dt>
                <dd class="mt-1 text-gray-800">{{ row.auth_source }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('adminUsers.relayMapping') }}</dt>
                <dd class="mt-1 text-gray-800">{{ relayMappingLabel(row) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('adminUsers.updated') }}</dt>
                <dd class="mt-1 text-gray-800">{{ formatDate(row.updated_at) }}</dd>
              </div>
            </dl>
            <div class="mt-3 flex flex-wrap gap-2">
              <button
                class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                :disabled="!row.relay_auth_password"
                @click="copyEncrypted(row)"
              >
                {{ t('adminUsers.copyEncrypted') }}
              </button>
              <button
                class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                :disabled="!row.relay_auth_password"
                @click="requestPlaintextCopy(row)"
              >
                {{ t('adminUsers.copyPlaintext') }}
              </button>
            </div>
            <div v-if="plaintextConfirmUserId === row.id" class="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-900">
              <p>{{ t('adminUsers.plaintextWarning') }}</p>
              <button
                class="mt-2 rounded bg-amber-700 px-2 py-1 font-medium text-white hover:bg-amber-800"
                @click="confirmCopyPlaintext(row)"
              >
                {{ t('adminUsers.confirmRevealAndCopy') }}
              </button>
            </div>
            <span v-if="copiedState[row.id]" class="mt-2 block text-xs text-gray-500" aria-live="polite">{{ copiedState[row.id] }}</span>
          </div>
        </div>

        <div v-if="rows.length > 0" class="mt-3 hidden overflow-x-auto md:block">
          <table class="min-w-[980px] divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.user') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.role') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.authSource') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.relayMapping') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.accessStatus') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.created') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.updated') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="row in rows" :key="row.id">
                <td class="px-3 py-2">
                  <div class="font-medium text-gray-900">{{ row.username }}</div>
                  <div class="text-xs text-gray-500">{{ row.email }}</div>
                  <div class="mt-1 font-mono text-[11px] text-gray-400">{{ t('adminUsers.localId') }} #{{ row.id }}</div>
                </td>
                <td class="px-3 py-2 text-gray-700">{{ row.role }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.auth_source }}</td>
                <td class="px-3 py-2 text-gray-700">{{ relayMappingLabel(row) }}</td>
                <td class="px-3 py-2">
                  <span
                    class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="row.relay_auth_password ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
                  >
                    {{ accessStatusLabel(row) }}
                  </span>
                </td>
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
                      {{ t('adminUsers.copyEncrypted') }}
                    </button>
                    <button
                      :data-testid="`copy-plaintext-${row.id}`"
                      class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="!row.relay_auth_password"
                      @click="requestPlaintextCopy(row)"
                    >
                      {{ t('adminUsers.copyPlaintext') }}
                    </button>
                    <div v-if="plaintextConfirmUserId === row.id" class="max-w-64 rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-900">
                      <p>{{ t('adminUsers.plaintextWarning') }}</p>
                      <button
                        :data-testid="`confirm-copy-plaintext-${row.id}`"
                        class="mt-2 rounded bg-amber-700 px-2 py-1 font-medium text-white hover:bg-amber-800"
                        @click="confirmCopyPlaintext(row)"
                      >
                        {{ t('adminUsers.confirmRevealAndCopy') }}
                      </button>
                    </div>
                    <span v-if="copiedState[row.id]" class="text-xs text-gray-500" aria-live="polite">{{ copiedState[row.id] }}</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="mt-3 text-sm text-gray-400">{{ t('adminUsers.empty') }}</div>
      </div>
    </div>
  </AppLayout>
</template>
