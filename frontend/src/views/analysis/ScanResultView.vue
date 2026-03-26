<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { listScans } from '@/api/analysis'
import { getRepo } from '@/api/repo'
import type { ScanResult, RepoConfig } from '@/types'

const route = useRoute()
const router = useRouter()
const repoId = Number(route.params.repoId)
const scanId = route.params.scanId ? Number(route.params.scanId) : null

const repo = ref<RepoConfig | null>(null)
const scan = ref<ScanResult | null>(null)
const allScans = ref<ScanResult[]>([])
const loading = ref(true)

onMounted(async () => {
  try {
    const [repoRes, scansRes] = await Promise.all([
      getRepo(repoId),
      listScans(repoId, 50),
    ])
    repo.value = repoRes.data.data ?? null
    allScans.value = scansRes.data.data ?? []

    if (scanId) {
      scan.value = allScans.value.find(s => s.id === scanId) ?? null
    } else {
      scan.value = allScans.value.length > 0 ? allScans.value[0] : null
    }
  } catch {
    router.push('/repos')
  } finally {
    loading.value = false
  }
})

function selectScan(s: ScanResult) {
  scan.value = s
}

function scoreColor(score: number) {
  if (score >= 70) return 'text-green-600'
  if (score >= 40) return 'text-yellow-600'
  return 'text-red-600'
}

function scoreBgColor(score: number) {
  if (score >= 70) return 'bg-green-50 border-green-200'
  if (score >= 40) return 'bg-yellow-50 border-yellow-200'
  return 'bg-red-50 border-red-200'
}

function scoreBarColor(score: number, max: number) {
  const pct = max > 0 ? score / max : 0
  if (pct >= 0.7) return 'bg-green-500'
  if (pct >= 0.4) return 'bg-yellow-500'
  return 'bg-red-500'
}

function priorityColor(priority: string) {
  if (priority === 'high') return 'bg-red-100 text-red-700'
  if (priority === 'medium') return 'bg-yellow-100 text-yellow-700'
  return 'bg-gray-100 text-gray-600'
}

function formatDate(date: string | null) {
  if (!date) return '—'
  return new Date(date).toLocaleString()
}

const dimensions = computed(() => {
  if (!scan.value?.dimensions) return []
  return Object.entries(scan.value.dimensions).map(([key, dim]) => ({
    name: key,
    ...dim,
  }))
})

const suggestions = computed(() => scan.value?.suggestions ?? [])

const autoFixable = computed(() => suggestions.value.filter(s => s.auto_fix))
const manualSuggestions = computed(() => suggestions.value.filter(s => !s.auto_fix))
</script>

<template>
  <AppLayout>
    <div v-if="loading" class="text-center text-gray-500 py-12">Loading...</div>

    <div v-else-if="scan" class="space-y-5">
      <!-- Header -->
      <div>
        <button class="text-sm text-indigo-600 hover:text-indigo-800" @click="router.push(`/repos/${repoId}`)">
          &larr; Back to {{ repo?.name || 'Repo' }}
        </button>
        <div class="mt-2 flex items-start justify-between">
          <div>
            <h1 class="text-2xl font-bold text-gray-900">Scan Result</h1>
            <p class="text-sm text-gray-500">
              {{ scan.scan_type }} scan &middot; {{ formatDate(scan.created_at) }}
              <span v-if="scan.commit_sha" class="ml-2 font-mono text-xs text-gray-400">{{ scan.commit_sha.slice(0, 8) }}</span>
            </p>
          </div>
          <div class="flex items-center space-x-3 rounded-lg border px-5 py-3" :class="scoreBgColor(scan.score)">
            <div class="text-3xl font-bold" :class="scoreColor(scan.score)">{{ scan.score }}</div>
            <div class="text-xs text-gray-500 leading-tight">/ 100<br>AI Score</div>
          </div>
        </div>
      </div>

      <!-- Dimensions -->
      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Score Breakdown</h2>
        <div class="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div v-for="dim in dimensions" :key="dim.name" class="rounded-lg border border-gray-100 p-4">
            <div class="flex items-center justify-between">
              <span class="text-sm font-medium text-gray-700">{{ dim.name }}</span>
              <span class="text-sm font-bold" :class="scoreColor(dim.score)">{{ dim.score }} / {{ dim.max_score }}</span>
            </div>
            <div class="mt-2 h-2 rounded-full bg-gray-100">
              <div
                class="h-2 rounded-full transition-all"
                :class="scoreBarColor(dim.score, dim.max_score)"
                :style="{ width: `${dim.max_score > 0 ? (dim.score / dim.max_score) * 100 : 0}%` }"
              />
            </div>
            <p class="mt-2 text-xs text-gray-500">{{ dim.details }}</p>
          </div>
        </div>
      </div>

      <!-- Suggestions -->
      <div v-if="suggestions.length > 0" class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Suggestions</h2>

        <!-- Auto-fixable -->
        <div v-if="autoFixable.length > 0" class="mt-4">
          <h3 class="text-xs font-medium text-gray-500 uppercase">Auto-fixable</h3>
          <ul class="mt-2 space-y-2">
            <li v-for="(s, i) in autoFixable" :key="'af-' + i"
              class="flex items-start space-x-3 rounded-md border border-green-100 bg-green-50 px-4 py-3">
              <svg class="mt-0.5 h-4 w-4 shrink-0 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <div>
                <span class="inline-flex rounded-full px-2 text-xs font-medium leading-5" :class="priorityColor(s.priority)">{{ s.priority }}</span>
                <span class="ml-1 text-xs text-gray-400">{{ s.category }}</span>
                <p class="mt-1 text-sm text-gray-700">{{ s.message }}</p>
              </div>
            </li>
          </ul>
        </div>

        <!-- Manual -->
        <div v-if="manualSuggestions.length > 0" class="mt-4">
          <h3 class="text-xs font-medium text-gray-500 uppercase">Manual improvements</h3>
          <ul class="mt-2 space-y-2">
            <li v-for="(s, i) in manualSuggestions" :key="'ms-' + i"
              class="flex items-start space-x-3 rounded-md border border-gray-100 px-4 py-3">
              <svg class="mt-0.5 h-4 w-4 shrink-0 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <div>
                <span class="inline-flex rounded-full px-2 text-xs font-medium leading-5" :class="priorityColor(s.priority)">{{ s.priority }}</span>
                <span class="ml-1 text-xs text-gray-400">{{ s.category }}</span>
                <p class="mt-1 text-sm text-gray-700">{{ s.message }}</p>
              </div>
            </li>
          </ul>
        </div>
      </div>

      <!-- Scan History sidebar -->
      <div v-if="allScans.length > 1" class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Other Scans</h2>
        <div class="mt-3 flex flex-wrap gap-2">
          <button v-for="s in allScans" :key="s.id"
            class="inline-flex items-center space-x-2 rounded-md border px-3 py-1.5 text-sm transition-colors"
            :class="s.id === scan.id ? 'border-indigo-300 bg-indigo-50' : 'border-gray-200 hover:bg-gray-50'"
            @click="selectScan(s)"
          >
            <span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5"
              :class="s.score >= 70 ? 'bg-green-100 text-green-800' : s.score >= 40 ? 'bg-yellow-100 text-yellow-800' : 'bg-red-100 text-red-800'">
              {{ s.score }}
            </span>
            <span class="text-gray-500">{{ s.scan_type }}</span>
            <span class="text-gray-400 text-xs">{{ formatDate(s.created_at) }}</span>
          </button>
        </div>
      </div>
    </div>

    <div v-else class="text-center text-gray-500 py-12">
      <p>No scan results found.</p>
      <button class="mt-3 text-sm text-indigo-600 hover:text-indigo-800" @click="router.push(`/repos/${repoId}`)">
        Back to Repo
      </button>
    </div>
  </AppLayout>
</template>
