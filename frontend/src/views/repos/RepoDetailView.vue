<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getRepo, updateRepo } from '@/api/repo'
import { getPR, listPRs, syncPRs, settlePR } from '@/api/pr'
import { listProviders } from '@/api/scmProvider'
import { useAuthStore } from '@/stores/auth'
import type { RepoConfig, PRRecord, SCMProvider } from '@/types'

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
const settlingPRId = ref<number | null>(null)
const detailsLoadingIds = ref<Record<number, boolean>>({})
const expandedPRId = ref<number | null>(null)
const prDetails = ref<Record<number, PRRecord>>({})
const prDetailRequests = new Map<number, Promise<boolean>>()
const providers = ref<SCMProvider[]>([])
const selectedProviderId = ref<number | null>(null)
const bindingSaving = ref(false)
const bindingMessage = ref('')
const isRepoUnbound = computed(() => repo.value?.binding_state === 'unbound')

const repoId = Number(route.params.id)

onMounted(async () => {
  try {
    const [repoRes, prsRes] = await Promise.all([
      getRepo(repoId),
      listPRs(repoId, { limit: 10, months: 3 }).catch(() => ({ data: { data: { items: [] } } })),
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

function formatDate(date: string | null) {
  if (!date) return '—'
  return new Date(date).toLocaleString()
}

function labelColor(label: string) {
  if (label === 'ai_via_sub2api') return 'bg-indigo-100 text-indigo-800'
  if (label === 'no_ai_detected') return 'bg-gray-100 text-gray-600'
  return 'bg-yellow-100 text-yellow-700'
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

async function loadPRs() {
  try {
    const prsRes = await listPRs(repoId, { limit: prsPageSize, offset: prsPage.value * prsPageSize, months: prsMonths.value })
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0
  } catch { /* load failed */ }
}

function handleMonthsChange(e: Event) {
  prsMonths.value = Number((e.target as HTMLSelectElement).value)
  prsPage.value = 0
  loadPRs()
}

function prsPrevPage() {
  if (prsPage.value > 0) { prsPage.value--; loadPRs() }
}

function prsNextPage() {
  if ((prsPage.value + 1) * prsPageSize < prsTotal.value) { prsPage.value++; loadPRs() }
}

function formatConfidence(value?: PRRecord['attribution_confidence']) {
  if (!value) return '—'
  return value
}

function formatCurrency(value?: number) {
  if (value == null || Number.isNaN(value)) return '—'
  return `$${value.toFixed(2)}`
}

function formatTokenCount(value?: number) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toLocaleString()
}

function resolvedPR(pr: PRRecord) {
  return prDetails.value[pr.id] ?? pr
}

function matchedCommitShas(pr: PRRecord) {
  return resolvedPR(pr).edges?.last_attribution_run?.matched_commit_shas ?? []
}

function validationReason(pr: PRRecord) {
  const detail = resolvedPR(pr)
  return detail.edges?.last_attribution_run?.validation_summary?.reason
    ?? detail.metadata_summary?.reason
    ?? '—'
}

function attributionIntervals(pr: PRRecord) {
  const detail = resolvedPR(pr)
  const intervals = detail.metadata_summary?.intervals
  return Array.isArray(intervals) ? intervals : []
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

async function togglePRDetails(prId: number) {
  if (expandedPRId.value === prId) {
    expandedPRId.value = null
    return
  }
  expandedPRId.value = prId
  const loaded = await ensurePRDetail(prId)
  if (!loaded && expandedPRId.value === prId && !prDetails.value[prId]) {
    expandedPRId.value = null
  }
}

async function handleSettlePR(prId: number) {
  settlingPRId.value = prId
  try {
    await settlePR(prId)
    await loadPRs()
    if (expandedPRId.value === prId || prDetails.value[prId]) {
      await ensurePRDetail(prId, { force: true })
    }
  } catch { /* settle failed */ } finally {
    settlingPRId.value = null
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
                <th class="px-3 py-2 text-left font-medium">AI Label</th>
                <th class="px-3 py-2 text-left font-medium">Attribution</th>
                <th class="px-3 py-2 text-left font-medium">Confidence</th>
                <th class="px-3 py-2 text-left font-medium">Primary Tokens</th>
                <th class="px-3 py-2 text-left font-medium">Primary Cost</th>
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
                  <td class="px-3 py-2">
                    <span class="inline-flex rounded-full px-2 text-xs font-medium leading-5" :class="labelColor(pr.ai_label)">
                      {{ pr.ai_label }}
                    </span>
                  </td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ pr.attribution_status || 'not_run' }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatConfidence(pr.attribution_confidence) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatTokenCount(pr.primary_token_count) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatCurrency(pr.primary_token_cost) }}</td>
                  <td class="whitespace-nowrap px-3 py-2 text-xs text-gray-400">{{ formatDate(pr.created_at) }}</td>
                  <td class="px-3 py-2">
                    <div class="flex items-center gap-2">
                      <button
                        class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                        :disabled="settlingPRId === pr.id"
                        @click="handleSettlePR(pr.id)"
                      >{{ settlingPRId === pr.id ? 'Settling...' : 'Settle' }}</button>
                      <button
                        class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                        :disabled="isPRDetailLoading(pr.id)"
                        @click="togglePRDetails(pr.id)"
                      >{{ isPRDetailLoading(pr.id) ? 'Loading...' : expandedPRId === pr.id ? 'Hide' : 'Details' }}</button>
                    </div>
                  </td>
                </tr>
                <tr v-if="expandedPRId === pr.id" class="bg-gray-50/70">
                  <td colspan="10" class="px-4 py-4">
                    <div class="space-y-4 text-xs text-gray-700">
                      <div class="grid gap-3 sm:grid-cols-4">
                        <div>
                          <div class="text-gray-400">Primary Tokens</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatTokenCount(resolvedPR(pr).primary_token_count) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">Primary Cost</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatCurrency(resolvedPR(pr).primary_token_cost) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">Validation Reason</div>
                          <div class="mt-1 font-medium text-gray-900">{{ validationReason(pr) }}</div>
                        </div>
                        <div>
                          <div class="text-gray-400">Last Attributed</div>
                          <div class="mt-1 font-medium text-gray-900">{{ formatDate(resolvedPR(pr).last_attributed_at || null) }}</div>
                        </div>
                      </div>

                      <div>
                        <div class="text-gray-400">Matched Commits</div>
                        <div v-if="matchedCommitShas(pr).length > 0" class="mt-1 flex flex-wrap gap-2">
                          <code v-for="sha in matchedCommitShas(pr)" :key="sha" class="rounded bg-white px-2 py-1 text-[11px] text-gray-800">{{ sha }}</code>
                        </div>
                        <div v-else class="mt-1 text-gray-500">No matched commits recorded.</div>
                      </div>

                      <div>
                        <div class="text-gray-400">Intervals</div>
                        <div v-if="attributionIntervals(pr).length > 0" class="mt-2 overflow-x-auto">
                          <table class="min-w-full divide-y divide-gray-200 text-[11px]">
                            <thead>
                              <tr class="uppercase text-gray-400">
                                <th class="px-2 py-1 text-left font-medium">Commit</th>
                                <th class="px-2 py-1 text-left font-medium">Tokens</th>
                                <th class="px-2 py-1 text-left font-medium">Cost</th>
                                <th class="px-2 py-1 text-left font-medium">Source</th>
                              </tr>
                            </thead>
                            <tbody class="divide-y divide-gray-100">
                              <tr v-for="(interval, idx) in attributionIntervals(pr)" :key="interval.checkpoint_id ?? interval.commit_sha ?? idx">
                                <td class="px-2 py-1 font-mono text-gray-800">{{ interval.commit_sha || '—' }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatTokenCount(interval.total_tokens) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ formatCurrency(interval.total_cost) }}</td>
                                <td class="px-2 py-1 text-gray-700">{{ interval.source || '—' }}</td>
                              </tr>
                            </tbody>
                          </table>
                        </div>
                        <div v-else class="mt-1 text-gray-500">No interval breakdown recorded.</div>
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
