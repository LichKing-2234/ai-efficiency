<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import AppPageHeader from '@/components/AppPageHeader.vue'
import { useRepoStore } from '@/stores/repo'
import { listProviders } from '@/api/scmProvider'
import { autoBindUnboundRepos, createRepoDirect, repairFailedWebhooks } from '@/api/repo'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import type { RepoConfig, RepoInventoryProviderSummary, RepoInventoryScopeSummary, RepoListParams, SCMProvider } from '@/types'
import { repositoryStatusLabel, scmProviderTypeLabel } from '@/utils/displayLabels'

type BindingFilter = 'all' | 'bound' | 'unbound'

const route = useRoute()
const router = useRouter()
const repoStore = useRepoStore()
const { t } = useI18n()
const auth = useAuthStore()

const deletingRepoID = ref<number | null>(null)
const bindingFilter = ref<BindingFilter>(initialBindingFilter())
const selectedProviderKey = ref(queryString(route.query.provider))
const selectedScope = ref(queryString(route.query.scope))
const currentPage = ref(readPositiveInt(route.query.page, 1))
const currentPageSize = ref(readPositiveInt(route.query.page_size, 20))
const scopeSearch = ref('')

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

onMounted(() => {
  const listRequest = repoStore.fetchRepos(buildListParams())
  const inventoryRequest = refreshInventoryOnly()

  void listRequest.then((applied) => {
    if (!applied) return
    hydrateServerPagination()
    hydrateServerSelection()
    replaceRepoQuery()
  })
  void inventoryRequest.then(() => {
    ensureSelectionFromInventory()
  })
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
  return priority(a) - priority(b) || a.name.localeCompare(b.name) || a.provider_key.localeCompare(b.provider_key)
}

function firstScope(provider: RepoInventoryProviderSummary | null) {
  return provider?.scopes[0]?.scope ?? ''
}

function ensureSelectionFromInventory() {
  const providers = platformInventory.value
  if (providers.length === 0) {
    if (!selectedProviderKey.value) {
      selectedScope.value = ''
    }
    return
  }

  if (hasExplicitRouteSelection()) {
    if (!queryString(route.query.provider) && queryString(route.query.binding) === 'unbound') {
      selectedProviderKey.value = providers.some((provider) => provider.provider_key === 'unbound') ? 'unbound' : ''
    }
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

function hydrateServerSelection() {
  if (hasExplicitRouteSelection()) return
  const selection = repoStore.selection
  if (!selection) return
  selectedProviderKey.value = selection.provider_key
  selectedScope.value = selection.scope
}

function hydrateServerPagination() {
  currentPage.value = repoStore.page
  currentPageSize.value = repoStore.pageSize
}

function hasExplicitRouteSelection() {
  return Boolean(
    queryString(route.query.provider)
    || queryString(route.query.scope)
    || queryString(route.query.binding),
  )
}

function providerIDFromKey(providerKey: string) {
  const match = providerKey.match(/^scm_provider:(\d+)$/)
  if (!match) return 0
  return Number.parseInt(match[1], 10)
}

function buildListParams(): RepoListParams {
  const params: RepoListParams = {
    page: currentPage.value,
    pageSize: currentPageSize.value,
  }
  const providerKey = selectedProviderKey.value
  if (providerKey === 'unbound') {
    params.bindingState = 'unbound'
  } else {
    const providerID = selectedProvider.value?.provider_id ?? providerIDFromKey(providerKey)
    if (providerID > 0) {
      params.scmProviderId = providerID
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
  const applied = await repoStore.fetchRepos(buildListParams())
  if (!applied) return
  hydrateServerPagination()
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
  if (deletingRepoID.value !== null) return
  deletingRepoID.value = id
  try {
    await repoStore.deleteRepo(id)
    await refreshWorkbench()
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || error?.message || t('repos.deleteFailed'))
  } finally {
    deletingRepoID.value = null
  }
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

function repoStatusType(status: string) {
  if (status === 'active') return 'success'
  if (status === 'webhook_failed') return 'danger'
  return 'info'
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <AppPageHeader
        :eyebrow="t('nav.codeSection')"
        :title="t('repos.title')"
        :description="t('repos.subtitle')"
      >
        <template #actions>
          <div data-testid="repo-page-actions" class="repo-page-actions grid w-full grid-cols-1 gap-3 sm:w-auto sm:items-center">
          <el-select
            v-model="bindingFilter"
            data-testid="repo-binding-filter"
            class="w-full"
            @change="applyBindingFilter"
          >
            <el-option value="all" :label="t('repos.allBindings')" />
            <el-option value="bound" :label="t('repos.bound')" />
            <el-option value="unbound" :label="t('repos.unbound')" />
          </el-select>
          <el-button type="primary" @click="openAddDialog">
            {{ t('repos.addRepo') }}
          </el-button>
          </div>
        </template>
      </AppPageHeader>

      <section class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.health') }}</h2>
            <p class="mt-1 text-sm text-slate-600">{{ t('repos.healthHelp') }}</p>
          </div>
          <div data-testid="repo-health-actions" class="grid w-full grid-cols-1 gap-1 sm:flex sm:w-auto sm:flex-wrap sm:justify-end">
            <el-button
              v-if="auth.isAdmin"
              data-testid="repo-auto-bind-button"
              type="success"
              link
              class="!m-0 min-h-11 w-full !justify-start sm:w-auto"
              :loading="autoBindLoading"
              :disabled="autoBindLoading"
              @click="handleAutoBindUnbound"
            >
              {{ autoBindLoading ? t('repos.autoBinding') : t('repos.autoBind') }}
            </el-button>
            <el-button
              v-if="auth.isAdmin"
              data-testid="repo-repair-webhooks-button"
              type="primary"
              link
              class="!m-0 min-h-11 w-full !justify-start sm:w-auto"
              :loading="webhookRepairLoading"
              :disabled="webhookRepairLoading"
              @click="handleRepairFailedWebhooks"
            >
              {{ webhookRepairLoading ? t('repos.webhookRepairing') : t('repos.repairWebhooks') }}
            </el-button>
            <el-button class="!m-0 min-h-11 w-full !justify-start sm:w-auto" type="primary" link @click="applyBindingFilter('unbound')">
              {{ t('repos.reviewNeedsBinding') }}
            </el-button>
          </div>
        </div>
        <div data-testid="repo-health-metrics" class="mt-4 grid grid-cols-2 gap-3 xl:grid-cols-4">
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
        <el-alert v-if="autoBindMessage" class="mt-4" type="success" :closable="false" show-icon :title="autoBindMessage" />
        <el-alert v-if="autoBindError" class="mt-4" type="error" :closable="false" show-icon :title="autoBindError" />
        <el-alert v-if="webhookRepairMessage" class="mt-4" type="success" :closable="false" show-icon :title="webhookRepairMessage" />
        <el-alert v-if="webhookRepairError" class="mt-4" type="error" :closable="false" show-icon :title="webhookRepairError" />
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
            <el-button
              v-for="provider in platformInventory"
              :key="provider.provider_key"
              data-testid="repo-platform-tab"
              class="!h-auto min-w-[180px] !justify-between !px-3 !py-2 text-left"
              :class="provider.provider_key === selectedProviderKey ? 'border-teal-500 bg-teal-50 text-teal-950' : 'border-slate-200 bg-white text-slate-700'"
              @click="selectProvider(provider)"
            >
              <span class="min-w-0">
                <span class="block truncate text-sm font-semibold">{{ provider.name }}</span>
                <span class="mt-0.5 block truncate text-xs text-slate-500">{{ scmProviderTypeLabel(provider.type, t) }}</span>
              </span>
              <span class="rounded bg-slate-100 px-2 py-1 text-xs font-semibold text-slate-700">{{ provider.total_repos }}</span>
            </el-button>
          </div>
        </div>

        <el-alert
          v-if="repoStore.error"
          data-testid="repo-list-error"
          class="m-5"
          type="error"
          :closable="false"
          show-icon
          :title="repoStore.error"
        />
        <el-alert
          v-if="repoStore.inventoryError"
          data-testid="repo-inventory-error"
          class="m-5"
          type="error"
          :closable="false"
          show-icon
          :title="repoStore.inventoryError"
        />

        <el-skeleton v-if="repoStore.loading && !repoStore.loaded" class="p-5" :rows="6" animated />

        <div
          v-else-if="repoStore.loaded && repoStore.repos.length === 0 && (repoStore.error || repoStore.inventoryError)"
        />

        <el-empty
          v-else-if="repoStore.loaded && repoStore.inventoryLoaded && repoStore.repos.length === 0 && !hasInventory"
          :description="t('repos.empty')"
          :image-size="80"
        />

        <div data-testid="repo-workbench" v-else class="grid min-h-[520px] xl:grid-cols-[280px_minmax(0,1fr)]">
          <aside class="border-b border-slate-200 bg-slate-50 p-4 xl:border-b-0 xl:border-r">
            <div class="flex items-center justify-between gap-3">
              <h3 class="text-sm font-semibold uppercase tracking-wide text-slate-900">{{ t('repos.scopeSection') }}</h3>
              <span class="rounded bg-white px-2 py-1 text-xs font-medium text-slate-500">{{ selectedPlatformScopes.length }}</span>
            </div>
            <el-input
              v-model="scopeSearch"
              type="search"
              :placeholder="t('repos.scopeSearch')"
              clearable
              class="mt-3 w-full"
            />
            <div class="mt-3 max-h-[420px] space-y-1 overflow-y-auto pr-1">
              <el-button
                v-for="scope in filteredScopes"
                :key="scope.scope"
                data-testid="repo-scope-option"
                class="!h-auto w-full !justify-start !px-3 !py-2 text-left"
                :class="scope.scope === selectedScope ? 'border-teal-500 bg-white shadow-sm' : 'border-transparent bg-transparent'"
                @click="selectScope(scope)"
              >
                <span data-testid="repo-scope-option-content" class="flex w-full min-w-0 flex-col items-stretch">
                  <span data-testid="repo-scope-option-heading" class="flex w-full items-center justify-between gap-3">
                    <span class="min-w-0 truncate text-sm font-semibold text-slate-900">{{ scope.scope }}</span>
                    <span class="shrink-0 text-xs text-slate-500">{{ t('repos.scopeCount', { count: scope.total_repos }) }}</span>
                  </span>
                  <span data-testid="repo-scope-option-summary" class="mt-1 flex w-full items-center gap-2 text-xs text-slate-500">
                    <span>{{ t('repos.bound') }} {{ scope.bound_repos }}</span>
                    <span v-if="scope.unbound_repos > 0" class="text-amber-700">{{ t('repos.unbound') }} {{ scope.unbound_repos }}</span>
                    <span v-if="scope.webhook_failed_repos > 0" class="text-red-700">Webhook {{ scope.webhook_failed_repos }}</span>
                  </span>
                </span>
              </el-button>
            </div>
          </aside>

          <div class="min-w-0">
            <div class="flex flex-col gap-2 border-b border-slate-200 px-5 py-4 md:flex-row md:items-center md:justify-between">
              <div class="min-w-0">
        <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ selectedProvider?.name || repoStore.selection?.provider_name }}</p>
                <h3 class="mt-1 truncate text-lg font-semibold text-slate-900">{{ selectedScope || t('repos.selectedScope') }}</h3>
              </div>
              <div class="text-sm text-slate-500">
                {{ repoStore.total }} {{ t('repos.repositoriesInScope') }}
              </div>
            </div>

            <el-skeleton v-if="repoStore.loading && repoStore.repos.length === 0" class="p-5" :rows="5" animated />

            <el-empty v-else-if="repoStore.repos.length === 0" :description="t('repos.scopedEmpty')" :image-size="80" />

            <template v-else>
              <div class="divide-y divide-slate-100">
                <article
          v-for="repo in repoStore.repos"
          :key="repo.id"
          data-testid="repo-row"
          class="cursor-pointer p-4 hover:bg-slate-50 xl:grid xl:grid-cols-[minmax(0,1fr)_minmax(240px,0.8fr)_minmax(180px,auto)] xl:items-center xl:gap-5 xl:px-5"
          role="button"
          tabindex="0"
          @click="goToDetail(repo)"
          @keydown.enter.self.prevent="goToDetail(repo)"
          @keydown.space.self.prevent="goToDetail(repo)"
        >
                  <div class="flex items-start justify-between gap-3">
                    <div class="min-w-0">
            <el-button class="repo-name-button !m-0 min-w-0 max-w-full !p-0 text-left" type="primary" link :title="repo.name" @click.stop="goToDetail(repo)">
                        <span class="block min-w-0 truncate">{{ repo.name }}</span>
                      </el-button>
                      <div class="mt-1 truncate text-xs text-gray-500">{{ repo.full_name }}</div>
                    </div>
                    <el-tag
                      class="shrink-0"
                      :type="repo.binding_state === 'bound' ? 'success' : 'warning'"
                      effect="light"
                      size="small"
                    >
                      {{ repo.binding_state === 'bound' ? t('repos.bound') : t('repos.needsBinding') }}
                    </el-tag>
                  </div>
          <dl class="mt-3 grid grid-cols-2 gap-3 text-xs xl:mt-0">
                    <div>
                      <dt class="text-gray-400">{{ t('repos.status') }}</dt>
                      <dd class="mt-1"><el-tag :type="repoStatusType(repo.status)" size="small">{{ repositoryStatusLabel(repo.status, t) }}</el-tag></dd>
                    </div>
                    <div>
                      <dt class="text-gray-400">{{ t('repos.binding') }}</dt>
                      <dd class="mt-1 text-gray-800">{{ repo.binding_state === 'bound' ? t('repos.bound') : t('repos.needsBinding') }}</dd>
                    </div>
                  </dl>
          <div class="mt-3 flex flex-wrap items-center gap-3 text-sm xl:mt-0 xl:justify-end" @click.stop>
                    <el-button type="primary" link @click="goToDetail(repo)">
                      {{ repoPrimaryAction(repo) }}
                    </el-button>
                    <el-popconfirm
                      :title="`${t('repos.delete')} ${repo.name}?`"
                      :confirm-button-text="t('repos.confirm')"
                      :cancel-button-text="t('repos.cancel')"
                      confirm-button-type="danger"
                      :teleported="false"
                      @confirm="confirmDelete(repo.id)"
                    >
                      <template #reference>
                        <el-button
                          type="danger"
                          link
                          :loading="deletingRepoID === repo.id"
                          :disabled="deletingRepoID !== null"
                        >
                          {{ t('repos.delete') }}
                        </el-button>
                      </template>
                    </el-popconfirm>
                  </div>
                </article>
              </div>


              <div class="flex flex-col gap-3 border-t border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
                <div class="text-sm text-slate-500">
                  {{ t('repos.pageOf', { page: currentPage, pages: pageCount }) }}
                </div>
                <div class="flex items-center gap-2">
                  <el-button
                    data-testid="repo-prev-page"
                    :disabled="!canGoPrevious"
                    @click="goToPage(currentPage - 1)"
                  >
                    {{ t('repos.previousPage') }}
                  </el-button>
                  <el-button
                    data-testid="repo-next-page"
                    :disabled="!canGoNext"
                    @click="goToPage(currentPage + 1)"
                  >
                    {{ t('repos.nextPage') }}
                  </el-button>
                </div>
              </div>
            </template>
          </div>
        </div>
      </section>
    </div>

    <el-dialog
      v-model="showAddDialog"
      :teleported="false"
      width="min(28rem, calc(100vw - 2rem))"
      :title="t('repos.addRepository')"
    >
        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('repos.scmProvider') }}</label>
            <el-select v-model="addForm.scm_provider_id" data-testid="repo-provider-select" class="mt-1 w-full">
              <el-option v-for="p in providers" :key="p.id" :value="p.id" :label="`${p.name} (${scmProviderTypeLabel(p.type, t)})`" />
            </el-select>
            <p v-if="providers.length === 0" class="mt-1 text-xs text-red-500">{{ t('repos.noScmProviders') }}</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('repos.repoUrl') }}</label>
            <el-input
              v-model="repoUrl"
              placeholder="https://github.com/org/repo or https://bitbucket.host/projects/PROJ/repos/name/browse"
              class="mt-1 w-full"
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
                <el-radio-group v-model="cloneProtocol" size="small" @change="onProtocolChange">
                  <el-radio-button value="http">HTTP</el-radio-button>
                  <el-radio-button value="ssh">SSH</el-radio-button>
                </el-radio-group>
              </div>
              <div v-if="cloneProtocol === 'ssh' && parsedInfo?.type === 'bitbucket'" class="mt-1">
                <el-input
                  v-model="sshHost"
                  placeholder="SSH host, e.g. git.example.com"
                  class="w-full"
                  @input="onSshHostInput"
                />
                <p class="mt-0.5 text-xs text-gray-400">{{ t('repos.bitbucketSshHelp') }}</p>
              </div>
              <el-input v-model="addForm.clone_url" data-testid="repo-clone-url" class="mt-1 w-full font-mono" />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('repos.defaultBranch') }}</label>
            <el-input v-model="addForm.default_branch" class="mt-1 w-full" />
          </div>

          <el-alert v-if="addError" type="error" :closable="false" show-icon :title="addError" />
        </div>

        <template #footer>
          <el-button @click="closeAddDialog">{{ t('repos.cancel') }}</el-button>
          <el-button type="primary" :loading="addLoading" :disabled="addLoading" @click="handleAddRepo">
            {{ addLoading ? t('repos.adding') : t('repos.add') }}
          </el-button>
        </template>
    </el-dialog>
  </AppLayout>
</template>

<style>
@media (min-width: 640px) {
  .repo-page-actions {
    grid-template-columns: 11rem auto;
  }
}

.repo-name-button > span {
  min-width: 0;
  max-width: 100%;
}
</style>
