<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { useRepoStore } from '@/stores/repo'
import { listProviders } from '@/api/scmProvider'
import { createRepoDirect } from '@/api/repo'
import type { RepoConfig, SCMProvider } from '@/types'

const router = useRouter()
const repoStore = useRepoStore()
const showDeleteConfirm = ref<number | null>(null)
const collapsedGroups = ref<Set<string>>(new Set())

interface RepoGroup {
  key: string
  scmName: string
  scmType: string
  org: string
  repos: RepoConfig[]
}

const groupedRepos = computed<RepoGroup[]>(() => {
  const map = new Map<string, RepoGroup>()
  for (const repo of repoStore.repos) {
    const scm = repo.edges?.scm_provider
    const scmName = scm?.name ?? 'Unknown'
    const scmType = scm?.type ?? ''
    const org = repo.full_name.split('/')[0] || repo.name
    const key = `${scmName}::${org}`
    if (!map.has(key)) {
      map.set(key, { key, scmName, scmType, org, repos: [] })
    }
    map.get(key)!.repos.push(repo)
  }
  // Sort: by SCM name, then org
  return Array.from(map.values()).sort((a, b) =>
    a.scmName.localeCompare(b.scmName) || a.org.localeCompare(b.org)
  )
})

function toggleGroup(key: string) {
  if (collapsedGroups.value.has(key)) {
    collapsedGroups.value.delete(key)
  } else {
    collapsedGroups.value.add(key)
  }
  // trigger reactivity
  collapsedGroups.value = new Set(collapsedGroups.value)
}

// Add repo dialog
const showAddDialog = ref(false)
const providers = ref<SCMProvider[]>([])
const repoUrl = ref('')
const addForm = ref({
  scm_provider_id: 0,
  name: '',
  full_name: '',
  clone_url: '',
  default_branch: 'main',
})
const cloneProtocol = ref<'http' | 'ssh'>('http')
const sshHost = ref('')
const parsedInfo = ref<{ origin: string; project: string; repo: string; type: 'github' | 'bitbucket' } | null>(null)
const addError = ref('')
const addLoading = ref(false)

onMounted(() => {
  repoStore.fetchRepos()
})

function goToDetail(repo: RepoConfig) {
  router.push(`/repos/${repo.id}`)
}

async function openAddDialog() {
  addError.value = ''
  repoUrl.value = ''
  cloneProtocol.value = 'http'
  sshHost.value = ''
  parsedInfo.value = null
  addForm.value = { scm_provider_id: 0, name: '', full_name: '', clone_url: '', default_branch: 'main' }
  try {
    const res = await listProviders()
    const data = res.data.data
    providers.value = Array.isArray(data) ? data : (data as any)?.items ?? []
    if (providers.value.length > 0) {
      addForm.value.scm_provider_id = providers.value[0].id
    }
  } catch {
    providers.value = []
  }
  showAddDialog.value = true
}

async function handleAddRepo() {
  addError.value = ''
  if (!addForm.value.full_name) {
    addError.value = 'Please enter a valid repo URL'
    return
  }
  addLoading.value = true
  try {
    await createRepoDirect(addForm.value)
    showAddDialog.value = false
    await repoStore.fetchRepos()
  } catch (e: any) {
    addError.value = e.response?.data?.message || 'Failed to add repo'
  } finally {
    addLoading.value = false
  }
}

function parseRepoUrl() {
  const url = repoUrl.value.trim()
  if (!url) {
    addForm.value.name = ''
    addForm.value.full_name = ''
    addForm.value.clone_url = ''
    parsedInfo.value = null
    return
  }

  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    return
  }

  // Try GitHub: https://github.com/org/repo
  const ghMatch = parsed.pathname.match(/^\/([^/]+)\/([^/]+?)(?:\.git)?$/)
  if (ghMatch) {
    const [, org, repo] = ghMatch
    parsedInfo.value = { origin: parsed.origin, project: org, repo, type: 'github' }
    addForm.value.full_name = `${org}/${repo}`
    addForm.value.name = repo
    cloneProtocol.value = 'http'
    updateCloneUrl()
    autoSelectProvider(parsed.origin)
    return
  }

  // Try Bitbucket Server: https://host/projects/PROJ/repos/repo-name/browse
  const bbMatch = parsed.pathname.match(/^\/projects\/([^/]+)\/repos\/([^/]+)/)
  if (bbMatch) {
    const [, project, repo] = bbMatch
    parsedInfo.value = { origin: parsed.origin, project, repo, type: 'bitbucket' }
    addForm.value.full_name = `${project}/${repo}`
    addForm.value.name = repo
    cloneProtocol.value = 'http'
    updateCloneUrl()
    autoSelectProvider(parsed.origin)
    return
  }
}

function updateCloneUrl() {
  const info = parsedInfo.value
  if (!info) return

  if (info.type === 'github') {
    if (cloneProtocol.value === 'http') {
      addForm.value.clone_url = `${info.origin}/${info.project}/${info.repo}.git`
    } else {
      addForm.value.clone_url = `git@github.com:${info.project}/${info.repo}.git`
    }
  } else {
    // Bitbucket Server
    if (cloneProtocol.value === 'http') {
      addForm.value.clone_url = `${info.origin}/scm/${info.project.toLowerCase()}/${info.repo}.git`
    } else {
      const host = sshHost.value || new URL(info.origin).hostname
      addForm.value.clone_url = `ssh://git@${host}/${info.project.toLowerCase()}/${info.repo}.git`
    }
  }
}

function onProtocolChange() {
  updateCloneUrl()
}

function onSshHostInput() {
  updateCloneUrl()
}

function autoSelectProvider(urlOrigin: string) {
  // Try to match a provider by base_url origin
  const match = providers.value.find(p => {
    try {
      return new URL(p.base_url).origin === urlOrigin
    } catch {
      return false
    }
  })
  if (match) {
    addForm.value.scm_provider_id = match.id
  }
}

async function confirmDelete(id: number) {
  await repoStore.deleteRepo(id)
  showDeleteConfirm.value = null
}

function formatDate(date: string | null) {
  if (!date) return '—'
  return new Date(date).toLocaleDateString()
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between">
        <h1 class="text-2xl font-bold text-gray-900">Repositories</h1>
        <button
          class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          @click="openAddDialog"
        >
          Add Repo
        </button>
      </div>

      <div v-if="repoStore.loading" class="text-center text-gray-500 py-12">Loading...</div>

      <div v-else-if="repoStore.repos.length === 0" class="rounded-lg bg-white p-12 shadow text-center text-sm text-gray-500">
        No repositories found. Click "Add Repo" to get started.
      </div>

      <div v-else class="space-y-4">
        <div v-for="group in groupedRepos" :key="group.key" class="rounded-lg bg-white shadow overflow-hidden">
          <!-- Group Header -->
          <button
            class="flex w-full items-center justify-between px-5 py-3 bg-gray-50 hover:bg-gray-100 text-left"
            @click="toggleGroup(group.key)"
          >
            <div class="flex items-center space-x-2">
              <span class="text-xs font-medium uppercase tracking-wide px-1.5 py-0.5 rounded"
                :class="group.scmType === 'github' ? 'bg-gray-900 text-white' : 'bg-blue-600 text-white'"
              >{{ group.scmType || 'scm' }}</span>
              <span class="text-sm font-semibold text-gray-900">{{ group.org }}</span>
              <span class="text-xs text-gray-400">{{ group.scmName }}</span>
              <span class="text-xs text-gray-400">({{ group.repos.length }})</span>
            </div>
            <svg
              class="h-4 w-4 text-gray-400 transition-transform" :class="{ 'rotate-180': !collapsedGroups.has(group.key) }"
              xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
            >
              <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
            </svg>
          </button>

          <!-- Repo Table -->
          <table v-if="!collapsedGroups.has(group.key)" class="min-w-full divide-y divide-gray-100">
            <thead>
              <tr class="text-xs text-gray-400 uppercase">
                <th class="px-5 py-2 text-left font-medium">Name</th>
                <th class="px-5 py-2 text-left font-medium">AI Score</th>
                <th class="px-5 py-2 text-left font-medium">Status</th>
                <th class="px-5 py-2 text-left font-medium">Last Scan</th>
                <th class="px-5 py-2 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr
                v-for="repo in group.repos"
                :key="repo.id"
                class="cursor-pointer hover:bg-gray-50"
                @click="goToDetail(repo)"
              >
                <td class="whitespace-nowrap px-5 py-3">
                  <div class="text-sm font-medium text-gray-900">{{ repo.name }}</div>
                </td>
                <td class="whitespace-nowrap px-5 py-3">
                  <span
                    class="inline-flex rounded-full px-2 text-xs font-semibold leading-5"
                    :class="repo.ai_score >= 70 ? 'bg-green-100 text-green-800' : repo.ai_score >= 40 ? 'bg-yellow-100 text-yellow-800' : 'bg-red-100 text-red-800'"
                  >{{ repo.ai_score }}</span>
                </td>
                <td class="whitespace-nowrap px-5 py-3 text-sm text-gray-500">{{ repo.status }}</td>
                <td class="whitespace-nowrap px-5 py-3 text-sm text-gray-500">{{ formatDate(repo.last_scan_at) }}</td>
                <td class="whitespace-nowrap px-5 py-3 text-right text-sm" @click.stop>
                  <button
                    v-if="showDeleteConfirm !== repo.id"
                    class="text-red-600 hover:text-red-800"
                    @click="showDeleteConfirm = repo.id"
                  >Delete</button>
                  <span v-else class="space-x-2">
                    <button class="text-red-700 font-medium" @click="confirmDelete(repo.id)">Confirm</button>
                    <button class="text-gray-500" @click="showDeleteConfirm = null">Cancel</button>
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Add Repo Dialog -->
    <div v-if="showAddDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="w-full max-w-md rounded-lg bg-white p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-gray-900 mb-4">Add Repository</h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">SCM Provider</label>
            <select v-model="addForm.scm_provider_id" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }} ({{ p.type }})</option>
            </select>
            <p v-if="providers.length === 0" class="mt-1 text-xs text-red-500">No SCM providers found. Create one first in Settings.</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Repo URL</label>
            <input v-model="repoUrl" @input="parseRepoUrl" type="text" placeholder="https://github.com/org/repo or https://bitbucket.host/projects/PROJ/repos/name/browse" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">Paste the repo browse URL, fields below are auto-derived</p>
          </div>

          <div v-if="addForm.full_name" class="rounded-md bg-gray-50 p-3 space-y-2 text-sm">
            <div class="flex justify-between">
              <span class="text-gray-500">Full Name</span>
              <span class="font-medium text-gray-900">{{ addForm.full_name }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-gray-500">Name</span>
              <span class="font-medium text-gray-900">{{ addForm.name }}</span>
            </div>
            <div>
              <div class="flex items-center justify-between">
                <span class="text-gray-500">Clone URL</span>
                <span class="inline-flex rounded-md shadow-sm">
                  <button type="button"
                    :class="cloneProtocol === 'http' ? 'bg-indigo-600 text-white' : 'bg-white text-gray-700 hover:bg-gray-50'"
                    class="rounded-l-md border border-gray-300 px-2.5 py-0.5 text-xs font-medium"
                    @click="cloneProtocol = 'http'; onProtocolChange()"
                  >HTTP</button>
                  <button type="button"
                    :class="cloneProtocol === 'ssh' ? 'bg-indigo-600 text-white' : 'bg-white text-gray-700 hover:bg-gray-50'"
                    class="-ml-px rounded-r-md border border-gray-300 px-2.5 py-0.5 text-xs font-medium"
                    @click="cloneProtocol = 'ssh'; onProtocolChange()"
                  >SSH</button>
                </span>
              </div>
              <div v-if="cloneProtocol === 'ssh' && parsedInfo?.type === 'bitbucket'" class="mt-1">
                <input v-model="sshHost" @input="onSshHostInput" type="text" placeholder="SSH host, e.g. git.agoralab.co" class="block w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs" />
                <p class="mt-0.5 text-xs text-gray-400">Bitbucket SSH host (often differs from web host)</p>
              </div>
              <input v-model="addForm.clone_url" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-xs font-mono" />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Default Branch</label>
            <input v-model="addForm.default_branch" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div v-if="addError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ addError }}</div>
        </div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="showAddDialog = false" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">Cancel</button>
          <button @click="handleAddRepo" :disabled="addLoading" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ addLoading ? 'Adding...' : 'Add' }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
