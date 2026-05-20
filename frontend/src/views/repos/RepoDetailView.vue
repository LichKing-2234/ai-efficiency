<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getRepo, updateRepo } from '@/api/repo'
import { getPR, listPRs, refreshPRUsage, syncPRs } from '@/api/pr'
import { listProviders } from '@/api/scmProvider'
import { useAuthStore } from '@/stores/auth'
import type { PRCommitUsageSnapshot, PRRecord, RepoConfig, SCMProvider } from '@/types'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const repo = ref<RepoConfig | null>(null)
const prs = ref<PRRecord[]>([])
const prsTotal = ref(0)
const prsPage = ref(0)
const prsPageSize = 10
const prsMonths = ref(3)
const loading = ref(true)
const syncing = ref(false)
const detailsLoadingIds = ref<Record<number, boolean>>({})
const expandedPRId = ref<number | null>(null)
const prDetails = ref<Record<number, PRRecord>>({})
const prDetailRequests = new Map<number, Promise<boolean>>()
const providers = ref<SCMProvider[]>([])
const selectedProviderId = ref<number | null>(null)
const bindingSaving = ref(false)
const bindingMessage = ref('')

const repoId = Number(route.params.id)
const isRepoUnbound = computed(() => repo.value?.binding_state === 'unbound')

onMounted(async () => {
  try {
    const [repoRes, prsRes] = await Promise.all([
      getRepo(repoId),
      listPRs(repoId, { limit: prsPageSize, months: prsMonths.value }).catch(() => ({ data: { data: { items: [] } } })),
    ])
    repo.value = repoRes.data.data ?? null
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0

    selectedProviderId.value = repo.value?.edges?.scm_provider?.id ?? repo.value?.scm_provider_id ?? null
    if (auth.isAdmin) {
      const providersRes = await listProviders().catch(() => ({ data: { data: [] } }))
      const providerData = providersRes.data.data
      providers.value = Array.isArray(providerData) ? providerData : (providerData as any)?.items ?? []
    }
  } catch {
    router.push('/repos')
  } finally {
    loading.value = false
  }
})

async function refreshRepo() {
  const repoRes = await getRepo(repoId)
  repo.value = repoRes.data.data ?? null
  selectedProviderId.value = repo.value?.edges?.scm_provider?.id ?? repo.value?.scm_provider_id ?? null
}

async function loadPRs() {
  try {
    const prsRes = await listPRs(repoId, { limit: prsPageSize, offset: prsPage.value * prsPageSize, months: prsMonths.value })
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0
  } catch { /* load failed */ }
}

async function handleSyncPRs() {
  syncing.value = true
  try {
    await syncPRs(repoId)
    prsPage.value = 0
    await loadPRs()
  } catch { /* sync failed */ } finally {
    syncing.value = false
  }
}

async function saveBinding() {
  bindingSaving.value = true
  bindingMessage.value = ''
  try {
    await updateRepo(repoId, { scm_provider_id: selectedProviderId.value ?? undefined, clear_scm_provider: selectedProviderId.value == null } as any)
    await refreshRepo()
    bindingMessage.value = 'SCM provider binding saved'
  } catch (error: any) {
    bindingMessage.value = error?.response?.data?.message || 'Failed to save SCM provider binding'
  } finally {
    bindingSaving.value = false
  }
}

async function clearBinding() {
  selectedProviderId.value = null
  await saveBinding()
}

function handleMonthsChange(e: Event) {
  prsMonths.value = Number((e.target as HTMLSelectElement).value)
  prsPage.value = 0
  loadPRs()
}

function prsPrevPage() {
  if (prsPage.value > 0) {
    prsPage.value--
    loadPRs()
  }
}

function prsNextPage() {
  if ((prsPage.value + 1) * prsPageSize < prsTotal.value) {
    prsPage.value++
    loadPRs()
  }
}

function formatDate(date: string | null | undefined) {
  if (!date) return '—'
  return new Date(date).toLocaleString()
}

function formatCount(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toLocaleString()
}

function formatDecimal(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toFixed(2)
}

function resolvedPR(pr: PRRecord) {
  return prDetails.value[pr.id] ?? pr
}

function commitSnapshots(pr: PRRecord): PRCommitUsageSnapshot[] {
  const detail = resolvedPR(pr)
  const snapshots = detail.edges?.pr_commit_usage_snapshots
  return Array.isArray(snapshots) ? [...snapshots].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0)) : []
}

function hasUsageSnapshot(pr: PRRecord) {
  return commitSnapshots(pr).length > 0
}

function usageSummaryNeedsRefresh(pr: PRRecord) {
  const detail = resolvedPR(pr)
  return !hasUsageSnapshot(detail) && !detail.usage_refreshed_at
}

function isPRDetailLoading(prId: number) {
  return Boolean(detailsLoadingIds.value[prId])
}

function setPRDetailLoading(prId: number, loading: boolean) {
  if (loading) {
    detailsLoadingIds.value = { ...detailsLoadingIds.value, [prId]: true }
    return
  }
  const { [prId]: _removed, ...rest } = detailsLoadingIds.value
  detailsLoadingIds.value = rest
}

async function ensurePRDetail(prId: number, options?: { force?: boolean }) {
  const forceRefresh = options?.force === true
  if (!forceRefresh && prDetails.value[prId]) return true

  const existingRequest = prDetailRequests.get(prId)
  if (existingRequest) {
    if (!forceRefresh) return existingRequest
    await existingRequest
  }

  setPRDetailLoading(prId, true)
  const request = (async () => {
    try {
      const res = await getPR(prId)
      const detail = res.data.data
      if (detail) {
        prDetails.value = { ...prDetails.value, [prId]: detail }
        return true
      }
      return false
    } catch {
      return false
    } finally {
      prDetailRequests.delete(prId)
      setPRDetailLoading(prId, false)
    }
  })()

  prDetailRequests.set(prId, request)
  return request
}

async function refreshUsageDetail(prId: number) {
  setPRDetailLoading(prId, true)
  try {
    const res = await refreshPRUsage(prId)
    const detail = res.data.data
    if (detail) {
      prDetails.value = { ...prDetails.value, [prId]: detail }
      prs.value = prs.value.map((pr) => (pr.id === prId ? { ...pr, ...detail } : pr))
      return true
    }
    return false
  } catch {
    return false
  } finally {
    setPRDetailLoading(prId, false)
  }
}

async function togglePRDetails(prId: number) {
  if (expandedPRId.value === prId) {
    expandedPRId.value = null
    return
  }

  expandedPRId.value = prId
  const loaded = await ensurePRDetail(prId)
  if (!loaded && expandedPRId.value === prId && !prDetails.value[prId]) {
    expandedPRId.value = null
    return
  }

  const row = prDetails.value[prId] ?? prs.value.find((item) => item.id === prId)
  if (row && usageSummaryNeedsRefresh(row)) {
    const refreshed = await refreshUsageDetail(prId)
    if (!refreshed && expandedPRId.value === prId && !prDetails.value[prId]) {
      expandedPRId.value = null
    }
  }
}
</script>

<template>
  <AppLayout>
    <div v-if="loading" class="py-12 text-center text-gray-500">Loading...</div>

    <div v-else-if="repo" class="space-y-5">
      <div>
        <button class="text-sm text-indigo-600 hover:text-indigo-800" @click="router.push('/repos')">
          &larr; Back to Repos
        </button>
        <div class="mt-2 flex items-start justify-between">
          <div>
            <h1 class="text-2xl font-bold text-gray-900">{{ repo.name }}</h1>
            <p class="text-sm text-gray-500">{{ repo.full_name }}</p>
            <p v-if="repo.clone_url" class="mt-0.5 select-all font-mono text-xs text-gray-400">{{ repo.clone_url }}</p>
          </div>
          <div class="flex items-center space-x-2">
            <button
              class="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              :disabled="syncing || isRepoUnbound"
              @click="handleSyncPRs"
            >{{ syncing ? 'Syncing...' : 'Sync PRs' }}</button>
          </div>
        </div>
      </div>

      <div v-if="auth.isAdmin" class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">SCM Provider Binding</h2>
          <span
            class="rounded px-2 py-0.5 text-xs font-medium"
            :class="isRepoUnbound ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800'"
          >
            {{ isRepoUnbound ? 'Unbound' : 'Bound' }}
          </span>
        </div>
        <p class="mt-3 text-sm text-gray-500">
          {{ isRepoUnbound ? 'This repo was auto-discovered by ae-cli attribution sync and still needs an SCM provider binding.' : 'This repo is currently bound to an SCM provider.' }}
        </p>
        <div class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
          <select
            v-model="selectedProviderId"
            class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 sm:max-w-sm"
          >
            <option :value="null">Unbound</option>
            <option v-for="provider in providers" :key="provider.id" :value="provider.id">
              {{ provider.name }}
            </option>
          </select>
          <div class="flex gap-2">
            <button
              class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
              :disabled="bindingSaving"
              @click="saveBinding"
            >
              {{ bindingSaving ? 'Saving...' : 'Save Binding' }}
            </button>
            <button
              class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 disabled:opacity-50"
              :disabled="bindingSaving || selectedProviderId == null"
              @click="clearBinding"
            >
              Clear Binding
            </button>
          </div>
        </div>
        <div v-if="bindingMessage" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-700">{{ bindingMessage }}</div>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <table class="w-full text-sm">
          <tbody>
            <tr>
              <td class="whitespace-nowrap py-1 pr-4 align-middle text-gray-400">Branch</td>
              <td class="py-1 align-middle text-gray-900">{{ repo.default_branch }}</td>
            </tr>
            <tr>
              <td class="whitespace-nowrap py-1 pr-4 align-middle text-gray-400">Status</td>
              <td class="py-1 align-middle text-gray-900">{{ repo.status }}</td>
            </tr>
            <tr>
              <td class="whitespace-nowrap py-1 pr-4 align-middle text-gray-400">Binding</td>
              <td class="py-1 align-middle text-gray-900">{{ repo.binding_state }}</td>
            </tr>
            <tr>
              <td class="whitespace-nowrap py-1 pr-4 align-middle text-gray-400">Created</td>
              <td class="py-1 align-middle text-gray-900">{{ formatDate(repo.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Pull Requests</h2>
          <div class="flex items-center space-x-3">
            <select :value="prsMonths" @change="handleMonthsChange" class="rounded-md border border-gray-300 px-2 py-1 text-xs text-gray-600">
              <option :value="1">Last 1 month</option>
              <option :value="3">Last 3 months</option>
              <option :value="6">Last 6 months</option>
              <option :value="12">Last 12 months</option>
              <option :value="0">All time</option>
            </select>
            <span v-if="prsTotal > 0" class="text-xs text-gray-400">{{ prsTotal }} total</span>
          </div>
        </div>

        <div v-if="prs.length > 0" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Title</th>
                <th class="px-3 py-2 text-left font-medium">Author</th>
                <th class="px-3 py-2 text-left font-medium">Status</th>
                <th class="px-3 py-2 text-left font-medium">Input</th>
                <th class="px-3 py-2 text-left font-medium">Output</th>
                <th class="px-3 py-2 text-left font-medium">Cache</th>
                <th class="px-3 py-2 text-left font-medium">Reasoning</th>
                <th class="px-3 py-2 text-left font-medium">Credits</th>
                <th class="px-3 py-2 text-left font-medium">Requests</th>
                <th class="px-3 py-2 text-left font-medium">Created</th>
                <th class="px-3 py-2 text-left font-medium">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <template v-for="pr in prs" :key="pr.id">
                <tr class="hover:bg-gray-50">
                  <td class="max-w-xs truncate px-3 py-2">
                    <a v-if="pr.scm_pr_url" :href="pr.scm_pr_url" target="_blank" rel="noopener noreferrer" class="text-indigo-600 hover:text-indigo-800">
                      {{ pr.title }}
                    </a>
                    <span v-else>{{ pr.title }}</span>
                  </td>
                  <td class="px-3 py-2 text-gray-500">{{ pr.author }}</td>
                  <td class="px-3 py-2">
                    <span
                      class="inline-flex rounded-full px-2 text-xs font-medium leading-5"
                      :class="pr.status === 'merged' ? 'bg-purple-50 text-purple-700' : pr.status === 'open' ? 'bg-green-50 text-green-700' : 'bg-gray-50 text-gray-500'"
                    >{{ pr.status }}</span>
                  </td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatCount(pr.usage_input_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatCount(pr.usage_output_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatCount(pr.usage_cached_input_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatCount(pr.usage_reasoning_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatDecimal(pr.usage_credit_usage) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatCount(pr.usage_request_count) }}</td>
                  <td class="whitespace-nowrap px-3 py-2 text-xs text-gray-400">{{ formatDate(pr.created_at) }}</td>
                  <td class="px-3 py-2">
                    <button
                      class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="isPRDetailLoading(pr.id)"
                      @click="togglePRDetails(pr.id)"
                    >{{ isPRDetailLoading(pr.id) ? 'Loading...' : expandedPRId === pr.id ? 'Hide' : 'Details' }}</button>
                  </td>
                </tr>
                <tr v-if="expandedPRId === pr.id" class="bg-gray-50/70">
                  <td colspan="11" class="px-4 py-4">
                    <div v-if="isPRDetailLoading(pr.id) && !prDetails[pr.id]" class="py-6 text-center text-xs text-gray-500">
                      Loading PR details...
                    </div>
                    <div v-else class="space-y-4 text-xs text-gray-700">
                      <div class="grid gap-3 sm:grid-cols-4">
                        <div>
                          <div class="text-gray-400">Input</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_input_tokens) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">Output</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatCount(resolvedPR(pr).usage_output_tokens) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">Credits</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatDecimal(resolvedPR(pr).usage_credit_usage) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">Last Refreshed</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatDate(resolvedPR(pr).usage_refreshed_at || null) }}</div>
                        </div>
                      </div>

                      <div>
                        <div class="text-gray-400">Commits</div>
                        <div v-if="commitSnapshots(pr).length > 0" class="mt-2 overflow-x-auto">
                          <table class="min-w-full divide-y divide-gray-200 text-[11px]">
                            <thead>
                              <tr class="uppercase text-gray-400">
                                <th class="px-2 py-1 text-left font-medium">Commit SHA</th>
                                <th class="px-2 py-1 text-left font-medium">Captured At</th>
                                <th class="px-2 py-1 text-left font-medium">Input</th>
                                <th class="px-2 py-1 text-left font-medium">Output</th>
                                <th class="px-2 py-1 text-left font-medium">Cache</th>
                                <th class="px-2 py-1 text-left font-medium">Reasoning</th>
                                <th class="px-2 py-1 text-left font-medium">Credits</th>
                                <th class="px-2 py-1 text-left font-medium">Requests</th>
                              </tr>
                            </thead>
                            <tbody class="divide-y divide-gray-100">
                              <tr v-for="snapshot in commitSnapshots(pr)" :key="snapshot.commit_sha">
                                <td class="px-2 py-1 font-mono text-gray-800">{{ snapshot.commit_sha }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatDate(snapshot.captured_at || null) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.input_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.output_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.cached_input_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.reasoning_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatDecimal(snapshot.credit_usage) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCount(snapshot.request_count) }}</td>
                              </tr>
                            </tbody>
                          </table>
                        </div>
                        <div v-else class="mt-1 text-gray-500">No commit usage snapshot yet.</div>
                      </div>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
          <div v-if="prsTotal > prsPageSize" class="mt-3 flex items-center justify-between border-t border-gray-100 pt-3">
            <span class="text-xs text-gray-400">
              {{ prsPage * prsPageSize + 1 }}–{{ Math.min((prsPage + 1) * prsPageSize, prsTotal) }} of {{ prsTotal }}
            </span>
            <div class="flex space-x-2">
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40" :disabled="prsPage === 0" @click="prsPrevPage">Prev</button>
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40" :disabled="(prsPage + 1) * prsPageSize >= prsTotal" @click="prsNextPage">Next</button>
            </div>
          </div>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No pull requests recorded yet.</p>
      </div>
    </div>
  </AppLayout>
</template>
