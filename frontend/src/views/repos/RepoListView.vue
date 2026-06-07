<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { useRepoStore } from '@/stores/repo'
import { listProviders } from '@/api/scmProvider'
import { autoBindUnboundRepos, createRepoDirect, repairFailedWebhooks } from '@/api/repo'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import { useModalFocus } from '@/composables/useModalFocus'
import type { RepoConfig, RepoInventoryProviderSummary, RepoInventoryScopeSummary, RepoListParams, SCMProvider } from '@/types'

type BindingFilter = 'all' | 'bound' | 'unbound'

const route = useRoute()
const router = useRouter()
const repoStore = useRepoStore()
const { t } = useI18n()
const auth = useAuthStore()

const showDeleteConfirm = ref<number | null>(null)
const bindingFilter = ref<BindingFilter>(initialBindingFilter())
const selectedProviderKey = ref(queryString(route.query.provider))
const selectedScope = ref(queryString(route.query.scope))
const currentPage = ref(readPositiveInt(route.query.page, 1))
const currentPageSize = ref(readPositiveInt(route.query.page_size, 20))
const scopeSearch = ref('')

const addDialog = ref<HTMLElement | null>(null)
const repoUrlInput = ref<HTMLInputElement | null>(null)
const autoBindLoading = ref(false)
const autoBindMessage = ref('')
const autoBindError = ref('')
const webhookRepairLoading = ref(false)
const webhookRepairMessage = ref('')
const webhookRepairError = ref('')

const platformInventory = computed(() => [...repoStore.inventory].sort(compareProviders))
const selectedProvider = computed(() =>
  platformInventory.value.find((provider) => provider.provider_key === selectedProviderKey.value) ?? null
)
const selectedPlatformScopes = computed(() => selectedProvider.value?.scopes ?? [])
const filteredScopes = computed(() => {
  const keyword = scopeSearch.value.trim().toLowerCase()
  if (!keyword) return selectedPlatformScopes.value
  return selectedPlatformScopes.value.filter((scope) => scope.scope.toLowerCase().includes(keyword))
})
const hasInventory = computed(() => platformInventory.value.some((provider) => provider.total_repos > 0))
const pageCount = computed(() => Math.max(1, Math.ceil(repoStore.total / currentPageSize.value)))
const canGoPrevious = computed(() => currentPage.value > 1)
const canGoNext = computed(() => currentPage.value < pageCount.value)

const healthSummary = computed(() => {
  return platformInventory.value.reduce(
    (summary, provider) => {
      summary.total += provider.total_repos
      summary.bound += provider.bound_repos
      summary.unbound += provider.unbound_repos
      summary.active += provider.active_repos
      return summary
    },
    { total: 0, bound: 0, unbound: 0, active: 0 }
  )
})

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

onMounted(async () => {
  await refreshInventoryOnly()
  ensureSelectionFromInventory()
  await fetchSelectedRepos()
})

const { handleKeydown: handleAddDialogKeydown } = useModalFocus(showAddDialog, addDialog, {
  initialFocus: repoUrlInput,
  onClose: closeAddDialog,
})

function queryString(value: unknown) {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function readPositiveInt(value: unknown, fallback: number) {
  const raw = queryString(value)
  const parsed = Number.parseInt(raw, 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

function initialBindingFilter(): BindingFilter {
  const value = route.query.binding
  return value === 'bound' || value === 'unbound' ? value : 'all'
}

function compareProviders(a: RepoInventoryProviderSummary, b: RepoInventoryProviderSummary) {
  if (a.provider_key === 'unbound') return 1
  if (b.provider_key === 'unbound') return -1
  const priority = (provider: RepoInventoryProviderSummary) => {
    if (provider.type === 'github') return 0
    if (provider.type === 'bitbucket_server') return 1
    return 2
  }
  return priority(a) - priority(b) || a.name.localeCompare(b.name)
}

function firstScope(provider: RepoInventoryProviderSummary | null) {
  return provider?.scopes[0]?.scope ?? ''
}

function ensureSelectionFromInventory() {
  const providers = platformInventory.value
  if (providers.length === 0) {
    selectedProviderKey.value = ''
    selectedScope.value = ''
    return
  }

  const selectedExists = providers.some((provider) => provider.provider_key === selectedProviderKey.value)
  if (!selectedExists) {
    const unbound = providers.find((provider) => provider.provider_key === 'unbound')
    const firstBound = providers.find((provider) => provider.provider_key !== 'unbound')
    selectedProviderKey.value = bindingFilter.value === 'unbound' && unbound ? unbound.provider_key : (firstBound ?? providers[0]).provider_key
  }

  const provider = selectedProvider.value
  const scopeExists = provider?.scopes.some((scope) => scope.scope === selectedScope.value)
  if (!scopeExists) {
    selectedScope.value = firstScope(provider)
  }
}

function buildListParams(): RepoListParams {
  const params: RepoListParams = {
    page: currentPage.value,
    pageSize: currentPageSize.value,
  }
  const provider = selectedProvider.value
  if (!provider) return params

  if (provider.provider_key === 'unbound') {
    params.bindingState = 'unbound'
  } else {
    if (provider.provider_id) {
      params.scmProviderId = provider.provider_id
    }
    if (bindingFilter.value !== 'all') {
      params.bindingState = bindingFilter.value
    }
  }
  if (selectedScope.value) {
    params.scope = selectedScope.value
  }
  return params
}

async function refreshInventoryOnly() {
  await repoStore.fetchInventory()
}

async function refreshWorkbench() {
  await refreshInventoryOnly()
  ensureSelectionFromInventory()
  await fetchSelectedRepos()
}

async function fetchSelectedRepos() {
  if (!selectedProvider.value || !selectedScope.value) {
    return
  }
  await repoStore.fetchRepos(buildListParams())
  replaceRepoQuery()
}

function replaceRepoQuery() {
  const query: Record<string, string> = {}
  if (bindingFilter.value !== 'all') {
    query.binding = bindingFilter.value
  }
  if (selectedProviderKey.value) {
    query.provider = selectedProviderKey.value
  }
  if (selectedScope.value) {
    query.scope = selectedScope.value
  }
  if (currentPage.value > 1) {
    query.page = String(currentPage.value)
  }
  if (currentPageSize.value !== 20) {
    query.page_size = String(currentPageSize.value)
  }
  void router.replace({ query })
}

async function selectProvider(provider: RepoInventoryProviderSummary) {
  selectedProviderKey.value = provider.provider_key
  selectedScope.value = firstScope(provider)
  scopeSearch.value = ''
  currentPage.value = 1
  await fetchSelectedRepos()
}

async function selectScope(scope: RepoInventoryScopeSummary) {
  selectedScope.value = scope.scope
  currentPage.value = 1
  await fetchSelectedRepos()
}

function onBindingFilterChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value as BindingFilter
  void applyBindingFilter(value)
}

async function applyBindingFilter(next: BindingFilter) {
  bindingFilter.value = next
  const unbound = platformInventory.value.find((provider) => provider.provider_key === 'unbound')
  const firstBound = platformInventory.value.find((provider) => provider.provider_key !== 'unbound')

  if (next === 'unbound' && unbound) {
    selectedProviderKey.value = unbound.provider_key
    selectedScope.value = firstScope(unbound)
  } else if (next !== 'unbound' && selectedProviderKey.value === 'unbound' && firstBound) {
    selectedProviderKey.value = firstBound.provider_key
    selectedScope.value = firstScope(firstBound)
  }

  currentPage.value = 1
  await fetchSelectedRepos()
}

async function goToPage(page: number) {
  if (page < 1 || page > pageCount.value || page === currentPage.value) return
  currentPage.value = page
  await fetchSelectedRepos()
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
    await refreshWorkbench()
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

  const bbMatch = parsed.pathname.match(/^\/projects\/([^/]+)\/repos\/([^/]+)/)
  if (bbMatch) {
    const [, project, repo] = bbMatch
    parsedInfo.value = { origin: parsed.origin, project, repo, type: 'bitbucket' }
    addForm.value.full_name = `${project}/${repo}`
    addForm.value.name = repo
    cloneProtocol.value = 'http'
    updateCloneUrl()
    autoSelectProvider(parsed.origin)
  }
}

function updateCloneUrl() {
  const info = parsedInfo.value
  if (!info) return

  if (info.type === 'github') {
    addForm.value.clone_url =
      cloneProtocol.value === 'http'
        ? `${info.origin}/${info.project}/${info.repo}.git`
        : `git@github.com:${info.project}/${info.repo}.git`
  } else if (cloneProtocol.value === 'http') {
    addForm.value.clone_url = `${info.origin}/scm/${info.project.toLowerCase()}/${info.repo}.git`
  } else {
    const host = sshHost.value || new URL(info.origin).hostname
    addForm.value.clone_url = `ssh://git@${host}/${info.project.toLowerCase()}/${info.repo}.git`
  }
}

function onProtocolChange() {
  updateCloneUrl()
}

function onSshHostInput() {
  updateCloneUrl()
}

function autoSelectProvider(urlOrigin: string) {
  const match = providers.value.find((p) => {
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
  await refreshWorkbench()
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
    await refreshWorkbench()
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
    await refreshWorkbench()
  } catch (error: any) {
    webhookRepairError.value = error?.response?.data?.message || t('repos.webhookRepairFailed')
  } finally {
    webhookRepairLoading.value = false
  }
}

function repoPrimaryAction(repo: RepoConfig) {
  return repo.binding_state === 'unbound' ? t('repos.bindProvider') : t('repos.viewPRUsage')
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p class="text-xs font-semibold uppercase tracking-wide text-teal-700">{{ t('nav.codeSection') }}</p>
          <h1 class="mt-1 text-2xl font-bold text-gray-900">{{ t('repos.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('repos.subtitle') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <select
            :value="bindingFilter"
            data-testid="repo-binding-filter"
            class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 focus:border-teal-500 focus:outline-none focus:ring-2 focus:ring-teal-500/20"
            @change="onBindingFilterChange"
          >
            <option value="all">{{ t('repos.allBindings') }}</option>
            <option value="bound">{{ t('repos.bound') }}</option>
            <option value="unbound">{{ t('repos.unbound') }}</option>
          </select>
          <button
            class="rounded-md bg-teal-700 px-4 py-2 text-sm font-medium text-white hover:bg-teal-800 focus:outline-none focus:ring-2 focus:ring-teal-500 focus:ring-offset-2"
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
            <p class="mt-1 text-sm text-slate-600">{{ t('repos.healthHelp') }}</p>
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
            <button class="text-sm font-medium text-teal-700 hover:text-teal-900" @click="applyBindingFilter('unbound')">
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

      <section class="overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="border-b border-slate-200 px-5 py-4">
          <div class="flex items-center justify-between gap-3">
            <div>
              <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.platformSection') }}</h2>
              <p class="mt-1 text-sm text-slate-500">{{ t('repos.repositoriesInScope') }}</p>
            </div>
          </div>
          <div class="mt-4 flex gap-2 overflow-x-auto pb-1">
            <button
              v-for="provider in platformInventory"
              :key="provider.provider_key"
              data-testid="repo-platform-tab"
              class="flex min-w-[180px] items-center justify-between gap-3 rounded-md border px-3 py-2 text-left transition hover:border-teal-300 hover:bg-teal-50"
              :class="provider.provider_key === selectedProviderKey ? 'border-teal-500 bg-teal-50 text-teal-950' : 'border-slate-200 bg-white text-slate-700'"
              @click="selectProvider(provider)"
            >
              <span class="min-w-0">
                <span class="block truncate text-sm font-semibold">{{ provider.name }}</span>
                <span class="mt-0.5 block truncate text-xs text-slate-500">{{ provider.type }}</span>
              </span>
              <span class="rounded bg-slate-100 px-2 py-1 text-xs font-semibold text-slate-700">{{ provider.total_repos }}</span>
            </button>
          </div>
        </div>

        <div v-if="repoStore.inventoryLoading && platformInventory.length === 0" class="py-12 text-center text-sm text-gray-500">
          {{ t('repos.loading') }}
        </div>

        <div v-else-if="!hasInventory" class="py-12 text-center text-sm text-gray-500">
          {{ t('repos.empty') }}
        </div>

        <div v-else class="grid min-h-[520px] lg:grid-cols-[280px_minmax(0,1fr)]">
          <aside class="border-b border-slate-200 bg-slate-50 p-4 lg:border-b-0 lg:border-r">
            <div class="flex items-center justify-between gap-3">
              <h3 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.scopeSection') }}</h3>
              <span class="rounded bg-white px-2 py-1 text-xs font-medium text-slate-500">{{ selectedPlatformScopes.length }}</span>
            </div>
            <input
              v-model="scopeSearch"
              type="search"
              :placeholder="t('repos.scopeSearch')"
              class="mt-3 w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-teal-500 focus:outline-none focus:ring-2 focus:ring-teal-500/20"
            />
            <div class="mt-3 max-h-[420px] space-y-1 overflow-y-auto pr-1">
              <button
                v-for="scope in filteredScopes"
                :key="scope.scope"
                data-testid="repo-scope-option"
                class="w-full rounded-md border px-3 py-2 text-left transition hover:border-teal-300 hover:bg-white"
                :class="scope.scope === selectedScope ? 'border-teal-500 bg-white shadow-sm' : 'border-transparent bg-transparent'"
                @click="selectScope(scope)"
              >
                <span class="flex items-center justify-between gap-3">
                  <span class="min-w-0 truncate text-sm font-semibold text-slate-900">{{ scope.scope }}</span>
                  <span class="shrink-0 text-xs text-slate-500">{{ t('repos.scopeCount', { count: scope.total_repos }) }}</span>
                </span>
                <span class="mt-1 flex items-center gap-2 text-xs text-slate-500">
                  <span>{{ t('repos.bound') }} {{ scope.bound_repos }}</span>
                  <span v-if="scope.unbound_repos > 0" class="text-amber-700">{{ t('repos.unbound') }} {{ scope.unbound_repos }}</span>
                  <span v-if="scope.webhook_failed_repos > 0" class="text-red-700">Webhook {{ scope.webhook_failed_repos }}</span>
                </span>
              </button>
            </div>
          </aside>

          <div class="min-w-0">
            <div class="flex flex-col gap-2 border-b border-slate-200 px-5 py-4 md:flex-row md:items-center md:justify-between">
              <div class="min-w-0">
                <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ selectedProvider?.name }}</p>
                <h3 class="mt-1 truncate text-lg font-semibold text-slate-900">{{ selectedScope || t('repos.selectedScope') }}</h3>
              </div>
              <div class="text-sm text-slate-500">
                {{ repoStore.total }} {{ t('repos.repositoriesInScope') }}
              </div>
            </div>

            <div v-if="repoStore.loading" class="py-12 text-center text-sm text-gray-500">
              {{ t('repos.loading') }}
            </div>

            <div v-else-if="repoStore.repos.length === 0" class="py-12 text-center text-sm text-gray-500">
              {{ t('repos.scopedEmpty') }}
            </div>

            <template v-else>
              <div class="space-y-3 p-4 md:hidden">
                <article v-for="repo in repoStore.repos" :key="repo.id" class="rounded-lg border border-gray-100 bg-white p-4 shadow-sm">
                  <div class="flex items-start justify-between gap-3">
                    <div class="min-w-0">
                      <button class="truncate text-left text-sm font-semibold text-teal-700 hover:text-teal-900" type="button" @click="goToDetail(repo)">
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
                    <button class="font-medium text-teal-700 hover:text-teal-900" type="button" @click="goToDetail(repo)">
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

              <table class="hidden min-w-full divide-y divide-gray-100 md:table">
                <thead>
                  <tr class="text-xs uppercase text-gray-400">
                    <th class="px-5 py-2 text-left font-medium">{{ t('repos.name') }}</th>
                    <th class="px-5 py-2 text-left font-medium">{{ t('repos.binding') }}</th>
                    <th class="px-5 py-2 text-left font-medium">{{ t('repos.status') }}</th>
                    <th class="px-5 py-2 text-right font-medium">{{ t('repos.actions') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-50">
                  <tr
                    v-for="repo in repoStore.repos"
                    :key="repo.id"
                    class="cursor-pointer hover:bg-gray-50"
                    role="button"
                    tabindex="0"
                    @click="goToDetail(repo)"
                    @keydown.enter.prevent="goToDetail(repo)"
                    @keydown.space.prevent="goToDetail(repo)"
                  >
                    <td class="whitespace-nowrap px-5 py-3">
                      <div class="flex min-w-0 items-center gap-2">
                        <div class="truncate text-sm font-medium text-gray-900">{{ repo.name }}</div>
                        <span
                          v-if="repo.binding_state === 'unbound'"
                          class="rounded bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800"
                        >
                          {{ t('repos.unbound') }}
                        </span>
                      </div>
                      <div class="mt-0.5 truncate text-xs text-gray-400">{{ repo.full_name }}</div>
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
                      <button class="mr-3 text-teal-700 hover:text-teal-900" @click="goToDetail(repo)">
                        {{ repoPrimaryAction(repo) }}
                      </button>
                      <button
                        v-if="showDeleteConfirm !== repo.id"
                        class="text-red-600 hover:text-red-800"
                        @click="showDeleteConfirm = repo.id"
                      >
                        {{ t('repos.delete') }}
                      </button>
                      <span v-else class="space-x-2">
                        <button class="font-medium text-red-700" @click="confirmDelete(repo.id)">{{ t('repos.confirm') }}</button>
                        <button class="text-gray-500" @click="showDeleteConfirm = null">{{ t('repos.cancel') }}</button>
                      </span>
                    </td>
                  </tr>
                </tbody>
              </table>

              <div class="flex flex-col gap-3 border-t border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
                <div class="text-sm text-slate-500">
                  {{ t('repos.pageOf', { page: currentPage, pages: pageCount }) }}
                </div>
                <div class="flex items-center gap-2">
                  <button
                    data-testid="repo-prev-page"
                    class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
                    :disabled="!canGoPrevious"
                    @click="goToPage(currentPage - 1)"
                  >
                    {{ t('repos.previousPage') }}
                  </button>
                  <button
                    data-testid="repo-next-page"
                    class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
                    :disabled="!canGoNext"
                    @click="goToPage(currentPage + 1)"
                  >
                    {{ t('repos.nextPage') }}
                  </button>
                </div>
              </div>
            </template>
          </div>
        </div>
      </section>
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
            <input
              ref="repoUrlInput"
              v-model="repoUrl"
              type="text"
              placeholder="https://github.com/org/repo or https://bitbucket.host/projects/PROJ/repos/name/browse"
              class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
              @input="parseRepoUrl"
            />
            <p class="mt-1 text-xs text-gray-400">{{ t('repos.repoUrlHelp') }}</p>
          </div>

          <div v-if="addForm.full_name" class="space-y-2 rounded-md bg-gray-50 p-3 text-sm">
            <div class="flex justify-between gap-3">
              <span class="text-gray-500">{{ t('repos.fullName') }}</span>
              <span class="truncate font-medium text-gray-900">{{ addForm.full_name }}</span>
            </div>
            <div class="flex justify-between gap-3">
              <span class="text-gray-500">{{ t('repos.name') }}</span>
              <span class="truncate font-medium text-gray-900">{{ addForm.name }}</span>
            </div>
            <div>
              <div class="flex items-center justify-between">
                <span class="text-gray-500">{{ t('repos.cloneUrl') }}</span>
                <span class="inline-flex rounded-md shadow-sm">
                  <button
                    type="button"
                    :class="cloneProtocol === 'http' ? 'bg-teal-700 text-white' : 'bg-white text-gray-700 hover:bg-gray-50'"
                    class="rounded-l-md border border-gray-300 px-2.5 py-0.5 text-xs font-medium"
                    @click="cloneProtocol = 'http'; onProtocolChange()"
                  >
                    HTTP
                  </button>
                  <button
                    type="button"
                    :class="cloneProtocol === 'ssh' ? 'bg-teal-700 text-white' : 'bg-white text-gray-700 hover:bg-gray-50'"
                    class="-ml-px rounded-r-md border border-gray-300 px-2.5 py-0.5 text-xs font-medium"
                    @click="cloneProtocol = 'ssh'; onProtocolChange()"
                  >
                    SSH
                  </button>
                </span>
              </div>
              <div v-if="cloneProtocol === 'ssh' && parsedInfo?.type === 'bitbucket'" class="mt-1">
                <input
                  v-model="sshHost"
                  type="text"
                  placeholder="SSH host, e.g. git.example.com"
                  class="block w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs"
                  @input="onSshHostInput"
                />
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
          <button class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50" @click="closeAddDialog">{{ t('repos.cancel') }}</button>
          <button class="rounded-md bg-teal-700 px-4 py-2 text-sm font-medium text-white hover:bg-teal-800 disabled:opacity-50" :disabled="addLoading" @click="handleAddRepo">
            {{ addLoading ? t('repos.adding') : t('repos.add') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
