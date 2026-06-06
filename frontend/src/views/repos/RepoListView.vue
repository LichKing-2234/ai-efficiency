<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { useRepoStore } from '@/stores/repo'
import { listProviders } from '@/api/scmProvider'
import { autoBindUnboundRepos, createRepoDirect, repairFailedWebhooks } from '@/api/repo'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import { useModalFocus } from '@/composables/useModalFocus'
import type { RepoConfig, SCMProvider } from '@/types'

const route = useRoute()
const router = useRouter()
const repoStore = useRepoStore()
const { t } = useI18n()
const auth = useAuthStore()
const showDeleteConfirm = ref<number | null>(null)
const collapsedGroups = ref<Set<string>>(new Set())
const bindingFilter = ref<'all' | 'bound' | 'unbound'>(initialBindingFilter())
const addDialog = ref<HTMLElement | null>(null)
const repoUrlInput = ref<HTMLInputElement | null>(null)
const autoBindLoading = ref(false)
const autoBindMessage = ref('')
const autoBindError = ref('')
const webhookRepairLoading = ref(false)
const webhookRepairMessage = ref('')
const webhookRepairError = ref('')

interface RepoGroup {
  key: string
  scmName: string
  scmType: string
  org: string
  repos: RepoConfig[]
}

const filteredRepos = computed(() => {
  if (bindingFilter.value === 'all') {
    return repoStore.repos
  }
  return repoStore.repos.filter((repo) => repo.binding_state === bindingFilter.value)
})

const healthSummary = computed(() => {
  const total = repoStore.repos.length
  const bound = repoStore.repos.filter((repo) => repo.binding_state === 'bound').length
  const unbound = repoStore.repos.filter((repo) => repo.binding_state === 'unbound').length
  const active = repoStore.repos.filter((repo) => repo.status === 'active').length
  return { total, bound, unbound, active }
})

const groupedRepos = computed<RepoGroup[]>(() => {
  const map = new Map<string, RepoGroup>()
  for (const repo of filteredRepos.value) {
    const scm = repo.edges?.scm_provider
    const scmName = scm?.name ?? t('repos.unboundProvider')
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

const { handleKeydown: handleAddDialogKeydown } = useModalFocus(showAddDialog, addDialog, {
  initialFocus: repoUrlInput,
  onClose: closeAddDialog,
})

watch(bindingFilter, replaceRepoQuery)

function initialBindingFilter(): 'all' | 'bound' | 'unbound' {
  const value = route.query.binding
  return value === 'bound' || value === 'unbound' ? value : 'all'
}

function replaceRepoQuery() {
  const query = bindingFilter.value === 'all' ? {} : { binding: bindingFilter.value }
  void router.replace({ query })
}

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

function closeAddDialog() {
  showAddDialog.value = false
}

async function handleAddRepo() {
  addError.value = ''
  if (!addForm.value.full_name) {
    addError.value = t('repos.invalidUrl')
    return
  }
  addLoading.value = true
  try {
    await createRepoDirect(addForm.value)
    showAddDialog.value = false
    await repoStore.fetchRepos()
  } catch (e: any) {
    addError.value = e.response?.data?.message || t('repos.addFailed')
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

async function handleAutoBindUnbound() {
  autoBindLoading.value = true
  autoBindMessage.value = ''
  autoBindError.value = ''
  try {
    const res = await autoBindUnboundRepos()
    const summary = res.data.data?.summary
    if (summary) {
      autoBindMessage.value = `${t('repos.autoBindComplete')}: ${t('repos.autoBindSummary', {
        bound: summary.bound,
        noMatch: summary.skipped_no_match,
        ambiguous: summary.skipped_ambiguous,
        webhookFailed: summary.webhook_failed,
        errors: summary.errors,
      })}`
    } else {
      autoBindMessage.value = t('repos.autoBindComplete')
    }
    await repoStore.fetchRepos()
  } catch (error: any) {
    autoBindError.value = error?.response?.data?.message || t('repos.autoBindFailed')
  } finally {
    autoBindLoading.value = false
  }
}

async function handleRepairFailedWebhooks() {
  webhookRepairLoading.value = true
  webhookRepairMessage.value = ''
  webhookRepairError.value = ''
  try {
    const res = await repairFailedWebhooks({ force: false })
    const summary = res.data.data?.summary
    if (summary) {
      webhookRepairMessage.value = `${t('repos.webhookRepairComplete')}: ${t('repos.webhookRepairSummary', {
        repaired: summary.repaired,
        alreadyRegistered: summary.already_registered,
        failed: summary.failed,
      })}`
    } else {
      webhookRepairMessage.value = t('repos.webhookRepairComplete')
    }
    await repoStore.fetchRepos()
  } catch (error: any) {
    webhookRepairError.value = error?.response?.data?.message || t('repos.webhookRepairFailed')
  } finally {
    webhookRepairLoading.value = false
  }
}

function formatDate(date: string | null) {
  if (!date) return '—'
  return new Date(date).toLocaleDateString()
}

function repoPrimaryAction(repo: RepoConfig) {
  return repo.binding_state === 'unbound' ? t('repos.bindProvider') : t('repos.viewPRUsage')
}

function applyBindingFilter(next: 'all' | 'bound' | 'unbound') {
  bindingFilter.value = next
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p class="text-xs font-semibold uppercase tracking-wide text-blue-700">{{ t('nav.codeSection') }}</p>
          <h1 class="mt-1 text-2xl font-bold text-gray-900">{{ t('repos.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('repos.subtitle') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <select
            v-model="bindingFilter"
            data-testid="repo-binding-filter"
            class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
          >
            <option value="all">{{ t('repos.allBindings') }}</option>
            <option value="bound">{{ t('repos.bound') }}</option>
            <option value="unbound">{{ t('repos.unbound') }}</option>
          </select>
          <button
            class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
            @click="openAddDialog"
          >
            {{ t('repos.addRepo') }}
          </button>
        </div>
      </div>

      <section class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.health') }}</h2>
            <p class="mt-1 text-sm text-slate-600">
              {{ t('repos.healthHelp') }}
            </p>
          </div>
          <div class="flex flex-wrap gap-3">
            <button
              v-if="auth.isAdmin"
              data-testid="repo-auto-bind-button"
              class="text-sm font-medium text-emerald-700 hover:text-emerald-900 disabled:opacity-50"
              :disabled="autoBindLoading"
              @click="handleAutoBindUnbound"
            >
              {{ autoBindLoading ? t('repos.autoBinding') : t('repos.autoBind') }}
            </button>
            <button
              v-if="auth.isAdmin"
              data-testid="repo-repair-webhooks-button"
              class="text-sm font-medium text-blue-700 hover:text-blue-900 disabled:opacity-50"
              :disabled="webhookRepairLoading"
              @click="handleRepairFailedWebhooks"
            >
              {{ webhookRepairLoading ? t('repos.webhookRepairing') : t('repos.repairWebhooks') }}
            </button>
            <button class="text-sm font-medium text-blue-700 hover:text-blue-900" @click="applyBindingFilter('unbound')">
              {{ t('repos.reviewNeedsBinding') }}
            </button>
          </div>
        </div>
        <div class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-md border border-slate-200 bg-slate-50 p-3">
            <div class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('repos.totalRepositories') }}</div>
            <div class="mt-2 text-2xl font-semibold text-slate-900">{{ healthSummary.total }}</div>
          </div>
          <div class="rounded-md border border-emerald-200 bg-emerald-50 p-3">
            <div class="text-xs font-medium uppercase tracking-wide text-emerald-700">{{ t('repos.boundRepositories') }}</div>
            <div class="mt-2 text-2xl font-semibold text-emerald-900">{{ healthSummary.bound }}</div>
          </div>
          <div class="rounded-md border border-amber-200 bg-amber-50 p-3">
            <div class="text-xs font-medium uppercase tracking-wide text-amber-700">{{ t('repos.needsBinding') }}</div>
            <div class="mt-2 text-2xl font-semibold text-amber-900">{{ healthSummary.unbound }}</div>
          </div>
          <div class="rounded-md border border-blue-200 bg-blue-50 p-3">
            <div class="text-xs font-medium uppercase tracking-wide text-blue-700">{{ t('repos.activeConfigs') }}</div>
            <div class="mt-2 text-2xl font-semibold text-blue-900">{{ healthSummary.active }}</div>
          </div>
        </div>
        <div v-if="autoBindMessage" class="mt-4 rounded-md bg-emerald-50 p-3 text-sm text-emerald-800">
          {{ autoBindMessage }}
        </div>
        <div v-if="autoBindError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
          {{ autoBindError }}
        </div>
        <div v-if="webhookRepairMessage" class="mt-4 rounded-md bg-emerald-50 p-3 text-sm text-emerald-800">
          {{ webhookRepairMessage }}
        </div>
        <div v-if="webhookRepairError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
          {{ webhookRepairError }}
        </div>
      </section>

      <div v-if="repoStore.loading" class="text-center text-gray-500 py-12">{{ t('repos.loading') }}</div>

      <div v-else-if="repoStore.repos.length === 0" class="rounded-lg bg-white p-12 shadow text-center text-sm text-gray-500">
        {{ t('repos.empty') }}
      </div>

      <div v-else class="space-y-4">
        <div v-for="group in groupedRepos" :key="group.key" class="rounded-lg bg-white shadow overflow-hidden">
          <!-- Group Header -->
          <button
            class="flex w-full items-center justify-between px-5 py-3 bg-gray-50 hover:bg-gray-100 text-left"
            :aria-expanded="!collapsedGroups.has(group.key)"
            @click="toggleGroup(group.key)"
          >
            <div class="flex min-w-0 items-center gap-2">
              <span class="text-xs font-medium uppercase tracking-wide px-1.5 py-0.5 rounded"
                :class="group.scmType === 'github' ? 'bg-gray-900 text-white' : 'bg-blue-600 text-white'"
              >{{ group.scmType || t('repos.unboundProviderShort') }}</span>
              <span class="truncate text-sm font-semibold text-gray-900">{{ group.org }}</span>
              <span class="hidden text-xs text-gray-400 sm:inline">{{ group.scmName }}</span>
              <span class="text-xs text-gray-400">({{ group.repos.length }})</span>
            </div>
            <svg
              class="h-4 w-4 text-gray-400 transition-transform" :class="{ 'rotate-180': !collapsedGroups.has(group.key) }"
              xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
            >
              <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
            </svg>
          </button>

          <div v-if="!collapsedGroups.has(group.key)" class="space-y-3 p-4 md:hidden">
            <article v-for="repo in group.repos" :key="repo.id" class="rounded-lg border border-gray-100 bg-white p-4 shadow-sm">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <button class="truncate text-left text-sm font-semibold text-indigo-700 hover:text-indigo-900" type="button" @click="goToDetail(repo)">
                    {{ repo.name }}
                  </button>
                  <div class="mt-1 truncate text-xs text-gray-500">{{ repo.full_name }}</div>
                </div>
                <span
                  class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium"
                  :class="repo.binding_state === 'bound' ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
                >
                  {{ repo.binding_state === 'bound' ? t('repos.bound') : t('repos.needsBinding') }}
                </span>
              </div>
              <dl class="mt-3 grid grid-cols-2 gap-3 text-xs">
                <div>
                  <dt class="text-gray-400">{{ t('repos.status') }}</dt>
                  <dd class="mt-1 text-gray-800">{{ repo.status }}</dd>
                </div>
                <div>
                  <dt class="text-gray-400">{{ t('repos.binding') }}</dt>
                  <dd class="mt-1 text-gray-800">{{ repo.binding_state === 'bound' ? t('repos.bound') : t('repos.needsBinding') }}</dd>
                </div>
              </dl>
              <div class="mt-3 flex flex-wrap items-center gap-3 text-sm">
                <button class="font-medium text-indigo-600 hover:text-indigo-800" type="button" @click="goToDetail(repo)">
                  {{ repoPrimaryAction(repo) }}
                </button>
                <button
                  v-if="showDeleteConfirm !== repo.id"
                  class="text-red-600 hover:text-red-800"
                  type="button"
                  @click="showDeleteConfirm = repo.id"
                >
                  {{ t('repos.delete') }}
                </button>
                <template v-else>
                  <button class="font-medium text-red-700" type="button" @click="confirmDelete(repo.id)">{{ t('repos.confirm') }}</button>
                  <button class="text-gray-500" type="button" @click="showDeleteConfirm = null">{{ t('repos.cancel') }}</button>
                </template>
              </div>
            </article>
          </div>

          <!-- Repo Table -->
          <table v-if="!collapsedGroups.has(group.key)" class="hidden min-w-full divide-y divide-gray-100 md:table">
            <thead>
              <tr class="text-xs text-gray-400 uppercase">
                <th class="px-5 py-2 text-left font-medium">{{ t('repos.name') }}</th>
                <th class="px-5 py-2 text-left font-medium">{{ t('repos.binding') }}</th>
                <th class="px-5 py-2 text-left font-medium">{{ t('repos.status') }}</th>
                <th class="px-5 py-2 text-right font-medium">{{ t('repos.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr
                v-for="repo in group.repos"
                :key="repo.id"
                class="cursor-pointer hover:bg-gray-50"
                role="button"
                tabindex="0"
                @click="goToDetail(repo)"
                @keydown.enter.prevent="goToDetail(repo)"
                @keydown.space.prevent="goToDetail(repo)"
              >
                <td class="whitespace-nowrap px-5 py-3">
                  <div class="flex items-center gap-2">
                    <div class="text-sm font-medium text-gray-900">{{ repo.name }}</div>
                    <span
                      v-if="repo.binding_state === 'unbound'"
                      class="rounded bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800"
                    >
                      {{ t('repos.unbound') }}
                    </span>
                  </div>
                </td>
                <td class="whitespace-nowrap px-5 py-3">
                  <span
                    class="rounded px-2 py-0.5 text-xs font-medium"
                    :class="repo.binding_state === 'bound' ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
                  >
                    {{ repo.binding_state === 'bound' ? t('repos.bound') : t('repos.needsBinding') }}
                  </span>
                </td>
                <td class="whitespace-nowrap px-5 py-3 text-sm text-gray-500">{{ repo.status }}</td>
                <td class="whitespace-nowrap px-5 py-3 text-right text-sm" @click.stop>
                  <button class="mr-3 text-indigo-600 hover:text-indigo-800" @click="goToDetail(repo)">
                    {{ repoPrimaryAction(repo) }}
                  </button>
                  <button
                    v-if="showDeleteConfirm !== repo.id"
                    class="text-red-600 hover:text-red-800"
                    @click="showDeleteConfirm = repo.id"
                  >{{ t('repos.delete') }}</button>
                  <span v-else class="space-x-2">
                    <button class="text-red-700 font-medium" @click="confirmDelete(repo.id)">{{ t('repos.confirm') }}</button>
                    <button class="text-gray-500" @click="showDeleteConfirm = null">{{ t('repos.cancel') }}</button>
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Add Repo Dialog -->
    <div v-if="showAddDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <button class="absolute inset-0" type="button" :aria-label="t('repos.cancel')" @click="closeAddDialog" />
      <div
        ref="addDialog"
        class="relative max-h-[90vh] w-full max-w-md overflow-y-auto rounded-lg bg-white p-6 shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="add-repository-title"
        tabindex="-1"
        @keydown="handleAddDialogKeydown"
      >
        <h2 id="add-repository-title" class="mb-4 text-lg font-semibold text-gray-900">{{ t('repos.addRepository') }}</h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('repos.scmProvider') }}</label>
            <select v-model="addForm.scm_provider_id" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }} ({{ p.type }})</option>
            </select>
            <p v-if="providers.length === 0" class="mt-1 text-xs text-red-500">{{ t('repos.noScmProviders') }}</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('repos.repoUrl') }}</label>
            <input ref="repoUrlInput" v-model="repoUrl" @input="parseRepoUrl" type="text" placeholder="https://github.com/org/repo or https://bitbucket.host/projects/PROJ/repos/name/browse" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">{{ t('repos.repoUrlHelp') }}</p>
          </div>

          <div v-if="addForm.full_name" class="rounded-md bg-gray-50 p-3 space-y-2 text-sm">
            <div class="flex justify-between">
              <span class="text-gray-500">{{ t('repos.fullName') }}</span>
              <span class="font-medium text-gray-900">{{ addForm.full_name }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-gray-500">{{ t('repos.name') }}</span>
              <span class="font-medium text-gray-900">{{ addForm.name }}</span>
            </div>
            <div>
              <div class="flex items-center justify-between">
                <span class="text-gray-500">{{ t('repos.cloneUrl') }}</span>
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
                <input v-model="sshHost" @input="onSshHostInput" type="text" placeholder="SSH host, e.g. git.example.com" class="block w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs" />
                <p class="mt-0.5 text-xs text-gray-400">{{ t('repos.bitbucketSshHelp') }}</p>
              </div>
              <input v-model="addForm.clone_url" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-xs font-mono" />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('repos.defaultBranch') }}</label>
            <input v-model="addForm.default_branch" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div v-if="addError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ addError }}</div>
        </div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="closeAddDialog" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">{{ t('repos.cancel') }}</button>
          <button @click="handleAddRepo" :disabled="addLoading" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ addLoading ? t('repos.adding') : t('repos.add') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
