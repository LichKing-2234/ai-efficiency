<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import RepoChat from '@/components/RepoChat.vue'
import { getRepo, updateRepo } from '@/api/repo'
import { triggerScan, listScans } from '@/api/analysis'
import { listPRs, syncPRs, settlePR } from '@/api/pr'
import type { RepoConfig, ScanResult, PRRecord } from '@/types'

const route = useRoute()
const router = useRouter()
const repo = ref<RepoConfig | null>(null)
const scans = ref<ScanResult[]>([])
const prs = ref<PRRecord[]>([])
const prsTotal = ref(0)
const prsPage = ref(0)
const prsPageSize = 10
const prsMonths = ref(3)
const loading = ref(true)
const scanning = ref(false)
const syncing = ref(false)
const settlingPRId = ref<number | null>(null)
const chatRef = ref<InstanceType<typeof RepoChat> | null>(null)

// Scan Settings
const showScanSettings = ref(false)
const scanPrompt = ref({ system_prompt: '', user_prompt_template: '' })
const scanPromptSaving = ref(false)
const scanPromptSuccess = ref('')

const repoId = Number(route.params.id)

onMounted(async () => {
  try {
    const [repoRes, scansRes, prsRes] = await Promise.all([
      getRepo(repoId),
      listScans(repoId, 10).catch(() => ({ data: { data: [] } })),
      listPRs(repoId, { limit: 10, months: 3 }).catch(() => ({ data: { data: { items: [] } } })),
    ])
    repo.value = repoRes.data.data ?? null
    scans.value = scansRes.data.data ?? []
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0

    if (repo.value?.scan_prompt_override) {
      scanPrompt.value = {
        system_prompt: repo.value.scan_prompt_override.system_prompt || '',
        user_prompt_template: repo.value.scan_prompt_override.user_prompt_template || '',
      }
    }
  } catch {
    router.push('/repos')
  } finally {
    loading.value = false
  }
})

async function handleScan() {
  scanning.value = true
  try {
    const res = await triggerScan(repoId)
    if (res.data.data) scans.value.unshift(res.data.data)
    const repoRes = await getRepo(repoId)
    repo.value = repoRes.data.data ?? null
  } catch { /* scan failed */ } finally {
    scanning.value = false
  }
}

function formatDate(date: string | null) {
  if (!date) return '—'
  return new Date(date).toLocaleString()
}

function scoreColor(score: number) {
  if (score >= 70) return 'text-green-600'
  if (score >= 40) return 'text-yellow-600'
  return 'text-red-600'
}

function scoreBadgeColor(score: number) {
  if (score >= 70) return 'bg-green-100 text-green-800'
  if (score >= 40) return 'bg-yellow-100 text-yellow-800'
  return 'bg-red-100 text-red-800'
}

function scoreBarColor(score: number, max: number) {
  const pct = max > 0 ? score / max : 0
  if (pct >= 0.7) return 'bg-green-500'
  if (pct >= 0.4) return 'bg-yellow-500'
  return 'bg-red-500'
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

function handleOptimize() {
  chatRef.value?.startOptimizePreview()
}

function formatConfidence(value?: number) {
  if (value == null || Number.isNaN(value)) return '—'
  const normalized = value <= 1 ? value * 100 : value
  return `${Math.round(normalized)}%`
}

function formatCurrency(value?: number) {
  if (value == null || Number.isNaN(value)) return '—'
  return `$${value.toFixed(2)}`
}

async function handleSettlePR(prId: number) {
  settlingPRId.value = prId
  try {
    await settlePR(prId)
    await loadPRs()
  } catch { /* settle failed */ } finally {
    settlingPRId.value = null
  }
}

async function handleSaveScanPrompt() {
  scanPromptSaving.value = true
  scanPromptSuccess.value = ''
  try {
    const override: Record<string, string> = {}
    if (scanPrompt.value.system_prompt) override.system_prompt = scanPrompt.value.system_prompt
    if (scanPrompt.value.user_prompt_template) override.user_prompt_template = scanPrompt.value.user_prompt_template
    await updateRepo(repoId, { scan_prompt_override: Object.keys(override).length > 0 ? override : undefined } as any)
    scanPromptSuccess.value = 'Scan prompt override saved'
    setTimeout(() => { scanPromptSuccess.value = '' }, 3000)
  } catch { /* save failed */ } finally {
    scanPromptSaving.value = false
  }
}

async function handleClearScanPrompt() {
  scanPromptSaving.value = true
  scanPromptSuccess.value = ''
  try {
    await updateRepo(repoId, { clear_scan_prompt: true } as any)
    scanPrompt.value = { system_prompt: '', user_prompt_template: '' }
    scanPromptSuccess.value = 'Scan prompt override cleared (using global defaults)'
    setTimeout(() => { scanPromptSuccess.value = '' }, 3000)
  } catch { /* clear failed */ } finally {
    scanPromptSaving.value = false
  }
}
</script>

<template>
  <AppLayout>
    <div v-if="loading" class="text-center text-gray-500 py-12">Loading...</div>

    <div v-else-if="repo" class="space-y-5">
      <!-- Header -->
      <div>
        <button class="text-sm text-indigo-600 hover:text-indigo-800" @click="router.push('/repos')">
          &larr; Back to Repos
        </button>
        <div class="mt-2 flex items-start justify-between">
          <div>
            <h1 class="text-2xl font-bold text-gray-900">{{ repo.name }}</h1>
            <p class="text-sm text-gray-500">{{ repo.full_name }}</p>
            <p v-if="repo.clone_url" class="mt-0.5 text-xs text-gray-400 font-mono select-all">{{ repo.clone_url }}</p>
          </div>
          <div class="flex items-center space-x-2">
            <button
              class="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              :disabled="scanning" @click="handleScan"
            >{{ scanning ? 'Scanning...' : 'Run Scan' }}</button>
            <button
              class="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              :disabled="syncing" @click="handleSyncPRs"
            >{{ syncing ? 'Syncing...' : 'Sync PRs' }}</button>
            <button
              class="rounded-md bg-green-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-green-700 disabled:opacity-50"
              :disabled="scans.length === 0" @click="handleOptimize"
            >Auto-Optimize</button>
          </div>
        </div>
      </div>

      <!-- Overview: Score + Info + Dimensions in one row -->
      <div class="rounded-lg bg-white shadow">
        <div class="grid grid-cols-12 divide-x divide-gray-100">
          <!-- Score -->
          <div class="col-span-2 flex flex-col items-center justify-center p-6">
            <div class="text-4xl font-bold" :class="scoreColor(repo.ai_score)">{{ repo.ai_score }}</div>
            <div class="mt-1 text-xs text-gray-500 uppercase tracking-wide">AI Score</div>
          </div>

          <!-- Basic Info -->
          <div class="col-span-3 p-5">
            <table class="w-full text-sm">
              <tbody>
                <tr>
                  <td class="text-gray-400 py-1 pr-4 align-middle whitespace-nowrap">Branch</td>
                  <td class="text-gray-900 py-1 align-middle">{{ repo.default_branch }}</td>
                </tr>
                <tr>
                  <td class="text-gray-400 py-1 pr-4 align-middle whitespace-nowrap">Status</td>
                  <td class="text-gray-900 py-1 align-middle">{{ repo.status }}</td>
                </tr>
                <tr>
                  <td class="text-gray-400 py-1 pr-4 align-middle whitespace-nowrap">Last Scan</td>
                  <td class="text-gray-900 py-1 align-middle">{{ formatDate(repo.last_scan_at) }}</td>
                </tr>
                <tr>
                  <td class="text-gray-400 py-1 pr-4 align-middle whitespace-nowrap">Created</td>
                  <td class="text-gray-900 py-1 align-middle">{{ formatDate(repo.created_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Score Breakdown -->
          <div class="col-span-7 p-5">
            <div v-if="scans.length > 0" class="grid grid-cols-2 gap-x-6 gap-y-2">
              <div v-for="(dim, key) in scans[0].dimensions" :key="String(key)">
                <div class="flex items-center justify-between text-sm">
                  <span class="text-gray-600 truncate">{{ String(key) }}</span>
                  <span class="ml-2 font-medium text-gray-900 shrink-0">{{ dim.score }}/{{ dim.max_score }}</span>
                </div>
                <div class="mt-1 h-1.5 rounded-full bg-gray-100">
                  <div
                    class="h-1.5 rounded-full transition-all"
                    :class="scoreBarColor(dim.score, dim.max_score)"
                    :style="{ width: `${dim.max_score > 0 ? (dim.score / dim.max_score) * 100 : 0}%` }"
                  />
                </div>
                <p class="mt-0.5 text-xs text-gray-400 line-clamp-1" :title="dim.details">{{ dim.details }}</p>
              </div>
            </div>
            <p v-else class="text-sm text-gray-400">No scan results yet. Click "Run Scan" to analyze.</p>
          </div>
        </div>
      </div>

      <!-- Scan History (compact) -->
      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Scan History</h2>
        <div v-if="scans.length > 0" class="mt-3 flex flex-wrap gap-2">
          <router-link v-for="scan in scans" :key="scan.id"
            :to="`/repos/${repoId}/scans/${scan.id}`"
            class="inline-flex items-center space-x-2 rounded-md border border-gray-200 px-3 py-1.5 text-sm hover:bg-gray-50 transition-colors"
          >
            <span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5" :class="scoreBadgeColor(scan.score)">
              {{ scan.score }}
            </span>
            <span class="text-gray-500">{{ scan.scan_type }}</span>
            <span class="text-gray-400 text-xs">{{ formatDate(scan.created_at) }}</span>
          </router-link>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No scans yet.</p>
      </div>

      <!-- Scan Settings (collapsible) -->
      <div class="rounded-lg bg-white shadow">
        <button
          class="flex w-full items-center justify-between px-5 py-3 text-left"
          @click="showScanSettings = !showScanSettings"
        >
          <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Scan Settings</h2>
          <svg
            class="h-4 w-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showScanSettings }"
            xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
          >
            <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
          </svg>
        </button>
        <div v-if="showScanSettings" class="border-t border-gray-100 px-5 py-4 space-y-4">
          <p class="text-sm text-gray-500">Override the global scan prompts for this repo. Leave empty to use global defaults.</p>
          <div>
            <label class="block text-sm font-medium text-gray-700">System Prompt</label>
            <textarea v-model="scanPrompt.system_prompt" rows="3" placeholder="Leave empty to use global default" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">User Prompt Template</label>
            <textarea v-model="scanPrompt.user_prompt_template" rows="4" placeholder="Leave empty to use global default. Use {repo_context} as placeholder." class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
            <p class="mt-1 text-xs text-gray-400">Use <code class="bg-gray-100 px-1 rounded">{repo_context}</code> placeholder for repo content.</p>
          </div>
          <div v-if="scanPromptSuccess" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ scanPromptSuccess }}</div>
          <div class="flex justify-end space-x-3">
            <button @click="handleClearScanPrompt" :disabled="scanPromptSaving"
              class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
              Clear Override
            </button>
            <button @click="handleSaveScanPrompt" :disabled="scanPromptSaving"
              class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
              {{ scanPromptSaving ? 'Saving...' : 'Save Override' }}
            </button>
          </div>
        </div>
      </div>

      <!-- PR Records -->
      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Pull Requests</h2>
          <div class="flex items-center space-x-3">
            <select :value="prsMonths" @change="handleMonthsChange"
              class="rounded-md border border-gray-300 px-2 py-1 text-xs text-gray-600">
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
              <tr class="text-xs text-gray-400 uppercase">
                <th class="px-3 py-2 text-left font-medium">Title</th>
                <th class="px-3 py-2 text-left font-medium">Author</th>
                <th class="px-3 py-2 text-left font-medium">Status</th>
                <th class="px-3 py-2 text-left font-medium">AI Label</th>
                <th class="px-3 py-2 text-left font-medium">Attribution</th>
                <th class="px-3 py-2 text-left font-medium">Confidence</th>
                <th class="px-3 py-2 text-left font-medium">Primary Cost</th>
                <th class="px-3 py-2 text-left font-medium">Created</th>
                <th class="px-3 py-2 text-left font-medium">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="pr in prs" :key="pr.id" class="hover:bg-gray-50">
                <td class="px-3 py-2 max-w-xs truncate">
                  <a v-if="pr.scm_pr_url" :href="pr.scm_pr_url" target="_blank" class="text-indigo-600 hover:text-indigo-800">
                    {{ pr.title }}
                  </a>
                  <span v-else>{{ pr.title }}</span>
                </td>
                <td class="px-3 py-2 text-gray-500">{{ pr.author }}</td>
                <td class="px-3 py-2">
                  <span class="inline-flex rounded-full px-2 text-xs font-medium leading-5"
                    :class="pr.status === 'merged' ? 'bg-purple-50 text-purple-700' : pr.status === 'open' ? 'bg-green-50 text-green-700' : 'bg-gray-50 text-gray-500'"
                  >{{ pr.status }}</span>
                </td>
                <td class="px-3 py-2">
                  <span class="inline-flex rounded-full px-2 text-xs font-medium leading-5" :class="labelColor(pr.ai_label)">
                    {{ pr.ai_label }}
                  </span>
                </td>
                <td class="px-3 py-2 text-gray-600 text-xs">{{ pr.attribution_status || 'not_run' }}</td>
                <td class="px-3 py-2 text-gray-600 text-xs">{{ formatConfidence(pr.attribution_confidence) }}</td>
                <td class="px-3 py-2 text-gray-600 text-xs">{{ formatCurrency(pr.primary_token_cost) }}</td>
                <td class="px-3 py-2 text-gray-400 text-xs whitespace-nowrap">{{ formatDate(pr.created_at) }}</td>
                <td class="px-3 py-2">
                  <button
                    class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                    :disabled="settlingPRId === pr.id"
                    @click="handleSettlePR(pr.id)"
                  >{{ settlingPRId === pr.id ? 'Settling...' : 'Settle' }}</button>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-if="prsTotal > prsPageSize" class="mt-3 flex items-center justify-between border-t border-gray-100 pt-3">
            <span class="text-xs text-gray-400">
              {{ prsPage * prsPageSize + 1 }}–{{ Math.min((prsPage + 1) * prsPageSize, prsTotal) }} of {{ prsTotal }}
            </span>
            <div class="flex space-x-2">
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                :disabled="prsPage === 0" @click="prsPrevPage">Prev</button>
              <button class="rounded border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-40"
                :disabled="(prsPage + 1) * prsPageSize >= prsTotal" @click="prsNextPage">Next</button>
            </div>
          </div>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No pull requests recorded yet.</p>
      </div>
    </div>

    <!-- Chat -->
    <RepoChat ref="chatRef" :repo-id="repoId" />
  </AppLayout>
</template>
