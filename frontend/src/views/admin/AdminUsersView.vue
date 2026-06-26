<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import {
  getAdminUserSubscriptionJob,
  getLatestAdminUserSubscriptionJob,
  listAdminUserDepartments,
  listAdminUsers,
  listAdminUserSubscriptionOptions,
  revealAdminUserRelayPassword,
  startAdminUserSubscriptionJob,
} from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import type {
  AdminAssignableSubscriptionProvider,
  AdminDirectoryDepartmentSummary,
  AdminManageSubscriptionsRequest,
  AdminManageSubscriptionsResultRow,
  AdminSubscriptionJob,
  AdminSubscriptionManageOperation,
  AdminSubscriptionManageScope,
  AdminUser,
} from '@/types'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const error = ref('')
const rows = ref<AdminUser[]>([])
const total = ref(0)
const departments = ref<AdminDirectoryDepartmentSummary[]>([])
const departmentsLoading = ref(false)
const departmentsError = ref('')
const expandedDepartmentIds = ref<Set<string>>(new Set())
const subscriptionProviders = ref<AdminAssignableSubscriptionProvider[]>([])
const subscriptionOptionsLoading = ref(false)
const subscriptionOptionsError = ref('')
const copiedState = reactive<Record<number, string>>({})
const plaintextConfirmUserId = ref<number | null>(null)
const selectedUserIds = ref<Set<number>>(new Set())
const selectAllUsersCheckbox = ref<HTMLInputElement | null>(null)
const subscriptionJob = ref<AdminSubscriptionJob | null>(null)
const subscriptionForm = reactive<{
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  provider_id: number | null
  group_id: string
  days: number
  confirmRemove: boolean
  loading: boolean
  message: string
  results: AdminManageSubscriptionsResultRow[]
}>({
  scope: 'selected',
  operation: 'add',
  provider_id: null,
  group_id: '',
  days: 30,
  confirmRemove: false,
  loading: false,
  message: '',
  results: [],
})
let searchTimer: number | undefined
let subscriptionJobPollTimer: number | undefined

const filters = reactive({
  view: queryString('view') === 'departments' ? 'departments' : 'users',
  q: queryString('q'),
  department_id: queryString('department_id'),
  page: queryNumber('page', 1),
  page_size: queryNumber('page_size', 20),
})

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.page_size)))
const canGoPrev = computed(() => filters.page > 1)
const canGoNext = computed(() => filters.page < totalPages.value)
const selectedUserIdList = computed(() => Array.from(selectedUserIds.value))
const selectedCount = computed(() => selectedUserIdList.value.length)
const allVisibleSelected = computed(() => rows.value.length > 0 && rows.value.every((row) => selectedUserIds.value.has(row.id)))
const visibleSelectionIndeterminate = computed(() => rows.value.some((row) => selectedUserIds.value.has(row.id)) && !allVisibleSelected.value)
const bulkGroups = computed(() => subscriptionProviders.value.find((provider) => provider.id === subscriptionForm.provider_id)?.groups ?? [])
const bulkUsesDays = computed(() => subscriptionForm.operation === 'add' || subscriptionForm.operation === 'extend')
const subscriptionResults = computed(() => subscriptionJob.value?.results ?? subscriptionForm.results)
const visibleDepartments = computed(() => {
  const byID = new Map(departments.value.map((department) => [department.external_id, department]))
  return departments.value.filter((department) => {
    let parentID = department.parent_external_id?.trim()
    const seen = new Set<string>()
    while (parentID) {
      if (seen.has(parentID)) return true
      seen.add(parentID)
      const parent = byID.get(parentID)
      if (!parent) return true
      if (!expandedDepartmentIds.value.has(parentID)) return false
      parentID = parent?.parent_external_id?.trim()
    }
    return true
  })
})
const canSubmitSubscriptionManagement = computed(() => {
  if (subscriptionForm.loading || !subscriptionForm.provider_id || !subscriptionForm.group_id) return false
  if (subscriptionForm.scope === 'selected' && selectedCount.value === 0) return false
  if (bulkUsesDays.value && subscriptionForm.days <= 0) return false
  if (subscriptionForm.operation === 'remove' && !subscriptionForm.confirmRemove) return false
  return true
})

async function loadUsers() {
  loading.value = true
  error.value = ''
	try {
    const params = {
      q: filters.q,
      ...(filters.department_id ? { department_id: filters.department_id } : {}),
      page: filters.page,
      page_size: filters.page_size,
    }
    const res = await listAdminUsers(params)
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

async function loadDepartments() {
  departmentsLoading.value = true
  departmentsError.value = ''
  try {
    const res = await listAdminUserDepartments()
    departments.value = res.data.data?.items ?? []
    expandedDepartmentIds.value = new Set(
      departments.value
        .filter((department) => (department.child_count ?? 0) > 0)
        .map((department) => department.external_id),
    )
  } catch (err: any) {
    departments.value = []
    expandedDepartmentIds.value = new Set()
    departmentsError.value = err.response?.data?.message || err.message || t('adminUsers.departmentsLoadFailed')
  } finally {
    departmentsLoading.value = false
  }
}

async function loadSubscriptionOptions() {
  subscriptionOptionsLoading.value = true
  subscriptionOptionsError.value = ''
  try {
    const res = await listAdminUserSubscriptionOptions()
    subscriptionProviders.value = res.data.data?.providers ?? []
    ensureBulkSubscriptionDefaults()
  } catch (err: any) {
    subscriptionProviders.value = []
    subscriptionOptionsError.value = err.response?.data?.message || err.message || t('adminUsers.loadSubscriptionsFailed')
  } finally {
    subscriptionOptionsLoading.value = false
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
  if (filters.view === 'departments') query.view = 'departments'
  if (filters.q.trim()) query.q = filters.q.trim()
  if (filters.department_id.trim()) query.department_id = filters.department_id.trim()
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

async function changeDepartmentFilter() {
  filters.page = 1
  clearSubscriptionFeedback()
  await loadUsers()
}

async function setAdminUsersView(view: 'users' | 'departments') {
  filters.view = view
  filters.page = 1
  replaceAdminUsersQuery()
  if (view === 'departments' && departments.value.length === 0) {
    await loadDepartments()
  }
}

async function openDepartmentUsers(department: AdminDirectoryDepartmentSummary) {
  filters.view = 'users'
  filters.department_id = department.external_id
  filters.page = 1
  clearSubscriptionFeedback()
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

function departmentLabel(user: AdminUser) {
  return user.department?.display_path || user.department?.name || t('adminUsers.unmatchedDepartment')
}

function departmentDisplayLabel(department: AdminDirectoryDepartmentSummary) {
  return department.display_path || department.name || department.external_id
}

function memberCountLabel(count: number) {
  return t(count === 1 ? 'adminUsers.memberCountSingular' : 'adminUsers.memberCountPlural', { count })
}

function matchedUserCountLabel(count: number) {
  return t(count === 1 ? 'adminUsers.matchedUserCountSingular' : 'adminUsers.matchedUserCountPlural', { count })
}

function departmentDepth(department: AdminDirectoryDepartmentSummary) {
  const depth = Number(department.depth ?? 0)
  return Number.isFinite(depth) && depth > 0 ? Math.min(depth, 8) : 0
}

function departmentIndentStyle(department: AdminDirectoryDepartmentSummary) {
  const depth = departmentDepth(department)
  return { paddingLeft: depth === 0 ? '1rem' : `${depth * 1.25}rem` }
}

function departmentAriaLevel(department: AdminDirectoryDepartmentSummary) {
  return String(departmentDepth(department) + 1)
}

function departmentExpanded(department: AdminDirectoryDepartmentSummary) {
  return expandedDepartmentIds.value.has(department.external_id)
}

function toggleDepartment(department: AdminDirectoryDepartmentSummary) {
  if ((department.child_count ?? 0) <= 0) return
  const next = new Set(expandedDepartmentIds.value)
  if (next.has(department.external_id)) {
    next.delete(department.external_id)
  } else {
    next.add(department.external_id)
  }
  expandedDepartmentIds.value = next
}

function subtreeMemberCountLabel(department: AdminDirectoryDepartmentSummary) {
  const count = department.subtree_member_count ?? department.member_count
  return t(count === 1 ? 'adminUsers.subtreeMemberCountSingular' : 'adminUsers.subtreeMemberCountPlural', { count })
}

function subtreeMatchedUserCountLabel(department: AdminDirectoryDepartmentSummary) {
  const count = department.subtree_matched_user_count ?? department.matched_user_count
  return t(count === 1 ? 'adminUsers.subtreeMatchedUserCountSingular' : 'adminUsers.subtreeMatchedUserCountPlural', { count })
}

function representativeCountLabel(department: AdminDirectoryDepartmentSummary) {
  return t('adminUsers.representativeMatchedCount', {
    matched: department.matched_representative_count ?? 0,
    total: department.representative_count ?? 0,
  })
}

function defaultSubscriptionProvider() {
  return subscriptionProviders.value.find((provider) => provider.groups.length > 0) ?? subscriptionProviders.value[0] ?? null
}

function ensureBulkSubscriptionDefaults() {
  if (subscriptionProviders.value.length === 0) {
    subscriptionForm.provider_id = null
    subscriptionForm.group_id = ''
    return
  }
  const currentProvider = subscriptionProviders.value.find((provider) => provider.id === subscriptionForm.provider_id)
  const provider = currentProvider?.groups.length ? currentProvider : defaultSubscriptionProvider()
  subscriptionForm.provider_id = provider?.id ?? null
  const currentGroupStillAvailable = provider?.groups.some((group) => group.group_id === subscriptionForm.group_id)
  if (!currentGroupStillAvailable) {
    subscriptionForm.group_id = provider?.groups[0]?.group_id ?? ''
  }
}

function isActiveSubscriptionJob(job: AdminSubscriptionJob | null) {
  return job?.status === 'queued' || job?.status === 'running'
}

function subscriptionJobMessage(job: AdminSubscriptionJob) {
  if (job.status === 'queued') {
    return t('adminUsers.subscriptionJobQueued', { processed: job.processed_count, total: job.total_count })
  }
  if (job.status === 'running') {
    return t('adminUsers.subscriptionJobRunning', { processed: job.processed_count, total: job.total_count })
  }
  if (job.status === 'completed') {
    return t('adminUsers.subscriptionJobCompleted', {
      success: job.success_count,
      skipped: job.skipped_count,
      failed: job.failed_count,
    })
  }
  return t('adminUsers.subscriptionJobFailed', { message: job.last_error || t('adminUsers.unknownError') })
}

function stopSubscriptionJobPolling() {
  if (subscriptionJobPollTimer) {
    window.clearInterval(subscriptionJobPollTimer)
    subscriptionJobPollTimer = undefined
  }
}

function applySubscriptionJob(job: AdminSubscriptionJob) {
  subscriptionJob.value = job
  subscriptionForm.results = job.results ?? []
  subscriptionForm.message = subscriptionJobMessage(job)
  subscriptionForm.loading = isActiveSubscriptionJob(job)
  if (!isActiveSubscriptionJob(job)) {
    stopSubscriptionJobPolling()
  }
}

async function refreshSubscriptionJob(jobId: number) {
  try {
    const res = await getAdminUserSubscriptionJob(jobId)
    const job = res.data.data
    if (job) {
      applySubscriptionJob(job)
    }
  } catch (err: any) {
    stopSubscriptionJobPolling()
    subscriptionForm.loading = false
    subscriptionForm.message = err.response?.data?.message || err.message || t('adminUsers.manageSubscriptionsFailed')
  }
}

function startSubscriptionJobPolling(job: AdminSubscriptionJob) {
  stopSubscriptionJobPolling()
  if (!isActiveSubscriptionJob(job)) return
  subscriptionJobPollTimer = window.setInterval(() => {
    void refreshSubscriptionJob(job.id)
  }, 1500)
}

async function recoverLatestSubscriptionJob() {
  try {
    const res = await getLatestAdminUserSubscriptionJob()
    const job = res.data.data
    if (job && isActiveSubscriptionJob(job)) {
      applySubscriptionJob(job)
      startSubscriptionJobPolling(job)
    }
  } catch {
    // Latest-job recovery is best-effort; normal user loading errors stay visible separately.
  }
}

function clearSubscriptionFeedback() {
  stopSubscriptionJobPolling()
  subscriptionJob.value = null
  subscriptionForm.message = ''
  subscriptionForm.results = []
}

function setBulkProvider(value: string) {
  const parsed = Number(value)
  subscriptionForm.provider_id = Number.isFinite(parsed) && parsed > 0 ? parsed : null
  subscriptionForm.group_id = bulkGroups.value[0]?.group_id ?? ''
  clearSubscriptionFeedback()
}

function setBulkGroup(value: string) {
  subscriptionForm.group_id = value
  clearSubscriptionFeedback()
}

function setBulkDays(value: string) {
  const parsed = Number(value)
  subscriptionForm.days = Number.isFinite(parsed) ? parsed : 0
  clearSubscriptionFeedback()
}

function setSubscriptionScope(value: string) {
  if (value === 'selected' || value === 'current_filter' || value === 'all_mapped') {
    subscriptionForm.scope = value
    clearSubscriptionFeedback()
  }
}

function setSubscriptionOperation(value: string) {
  if (value === 'add' || value === 'extend' || value === 'remove') {
    subscriptionForm.operation = value
    subscriptionForm.confirmRemove = false
    if (value === 'add' && subscriptionForm.days <= 0) subscriptionForm.days = 30
    if (value === 'extend' && subscriptionForm.days <= 0) subscriptionForm.days = 7
    clearSubscriptionFeedback()
  }
}

function setUserSelected(userId: number, checked: boolean) {
  const next = new Set(selectedUserIds.value)
  if (checked) {
    next.add(userId)
  } else {
    next.delete(userId)
  }
  selectedUserIds.value = next
  clearSubscriptionFeedback()
}

function setAllVisibleSelected(checked: boolean) {
  const next = new Set(selectedUserIds.value)
  for (const row of rows.value) {
    if (checked) {
      next.add(row.id)
    } else {
      next.delete(row.id)
    }
  }
  selectedUserIds.value = next
  clearSubscriptionFeedback()
}

function syncVisibleSelectionIndeterminate() {
  if (selectAllUsersCheckbox.value) {
    selectAllUsersCheckbox.value.indeterminate = visibleSelectionIndeterminate.value
  }
}

function scopeSummaryLabel() {
  if (subscriptionForm.scope === 'selected') {
    return t('adminUsers.selectedUsersCount', { count: selectedCount.value })
  }
  if (subscriptionForm.scope === 'current_filter') {
    if (filters.q.trim()) return t('adminUsers.currentFilterScopeWithQuery', { query: filters.q.trim() })
    if (filters.department_id.trim()) return t('adminUsers.currentFilterScopeWithDepartment')
    return t('adminUsers.currentFilterScope')
  }
  return t('adminUsers.allMappedScope')
}

function operationDaysLabel() {
  return subscriptionForm.operation === 'extend' ? t('adminUsers.extensionDays') : t('adminUsers.validityDays')
}

async function submitSubscriptionManagement() {
  if (!canSubmitSubscriptionManagement.value || !subscriptionForm.provider_id) return
  const payload: AdminManageSubscriptionsRequest = {
    scope: subscriptionForm.scope,
    operation: subscriptionForm.operation,
    provider_id: subscriptionForm.provider_id,
    group_id: subscriptionForm.group_id,
  }
  if (subscriptionForm.scope === 'selected') {
    payload.user_ids = selectedUserIdList.value
  } else if (subscriptionForm.scope === 'current_filter') {
    payload.filters = { q: filters.q.trim(), department_id: filters.department_id.trim() }
  }
  if (subscriptionForm.operation === 'add') {
    payload.validity_days = subscriptionForm.days
  } else if (subscriptionForm.operation === 'extend') {
    payload.days = subscriptionForm.days
  }

  clearSubscriptionFeedback()
  subscriptionForm.loading = true
  try {
    const res = await startAdminUserSubscriptionJob(payload)
    const job = res.data.data
    if (job) {
      applySubscriptionJob(job)
      startSubscriptionJobPolling(job)
    } else {
      subscriptionForm.loading = false
      subscriptionForm.message = t('adminUsers.manageSubscriptionsFailed')
    }
  } catch (err: any) {
    subscriptionForm.message = err.response?.data?.message || err.message || t('adminUsers.manageSubscriptionsFailed')
    subscriptionForm.loading = false
  }
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

watch([visibleSelectionIndeterminate, allVisibleSelected], syncVisibleSelectionIndeterminate, { flush: 'post' })

onMounted(() => {
  void loadUsers()
  void loadDepartments()
  void loadSubscriptionOptions()
  void recoverLatestSubscriptionJob()
  syncVisibleSelectionIndeterminate()
})
onBeforeUnmount(() => {
  clearSearchTimer()
  stopSubscriptionJobPolling()
})
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

      <div class="inline-flex rounded-md border border-gray-200 bg-white p-1 shadow-sm">
        <button
          data-testid="admin-users-view-users"
          type="button"
          class="rounded px-3 py-1.5 text-sm font-medium"
          :class="filters.view === 'users' ? 'bg-gray-900 text-white' : 'text-gray-600 hover:bg-gray-50'"
          @click="setAdminUsersView('users')"
        >
          {{ t('adminUsers.userView') }}
        </button>
        <button
          data-testid="admin-users-view-departments"
          type="button"
          class="rounded px-3 py-1.5 text-sm font-medium"
          :class="filters.view === 'departments' ? 'bg-gray-900 text-white' : 'text-gray-600 hover:bg-gray-50'"
          @click="setAdminUsersView('departments')"
        >
          {{ t('adminUsers.departmentView') }}
        </button>
      </div>

      <div v-if="filters.view === 'users'" class="rounded-lg bg-white p-4 shadow">
        <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_220px_120px_auto]">
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
            {{ t('adminUsers.department') }}
            <select
              v-model="filters.department_id"
              data-testid="admin-users-department-filter"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              @change="changeDepartmentFilter"
            >
	              <option value="">{{ t('adminUsers.allDepartments') }}</option>
	              <option v-for="department in departments" :key="department.external_id" :value="department.external_id">
	                {{ departmentDisplayLabel(department) }}
	              </option>
            </select>
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
        <p v-if="departmentsError" class="mt-3 rounded-md bg-amber-50 p-3 text-sm text-amber-800">{{ departmentsError }}</p>
      </div>

      <div v-if="filters.view === 'users'" class="rounded-lg bg-white p-4 shadow">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('adminUsers.subscriptionManagement') }}</h2>
            <p class="mt-1 text-sm text-gray-500">{{ scopeSummaryLabel() }}</p>
          </div>
          <button
            data-testid="manage-subscriptions-submit"
            class="shrink-0 rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="!canSubmitSubscriptionManagement"
            @click="submitSubscriptionManagement"
          >
            {{ subscriptionForm.loading ? t('adminUsers.working') : t('adminUsers.applySubscriptionChange') }}
          </button>
        </div>

        <p v-if="subscriptionOptionsError" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ subscriptionOptionsError }}</p>

        <div class="mt-4 grid gap-3 lg:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_130px]">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.subscriptionScope') }}
            <select
              data-testid="subscription-scope"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              :value="subscriptionForm.scope"
              :disabled="subscriptionForm.loading"
              @change="setSubscriptionScope(($event.target as HTMLSelectElement).value)"
            >
              <option value="selected">{{ t('adminUsers.scopeSelected') }}</option>
              <option value="current_filter">{{ t('adminUsers.scopeCurrentFilter') }}</option>
              <option value="all_mapped">{{ t('adminUsers.scopeAllMapped') }}</option>
            </select>
          </label>

          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.subscriptionOperation') }}
            <select
              data-testid="subscription-operation"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              :value="subscriptionForm.operation"
              :disabled="subscriptionForm.loading"
              @change="setSubscriptionOperation(($event.target as HTMLSelectElement).value)"
            >
              <option value="add">{{ t('adminUsers.operationAdd') }}</option>
              <option value="extend">{{ t('adminUsers.operationExtend') }}</option>
              <option value="remove">{{ t('adminUsers.operationRemove') }}</option>
            </select>
          </label>

          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.selectProvider') }}
            <select
              data-testid="subscription-provider"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              :value="subscriptionForm.provider_id ?? ''"
              :disabled="subscriptionOptionsLoading || subscriptionForm.loading"
              @change="setBulkProvider(($event.target as HTMLSelectElement).value)"
            >
              <option value="">{{ t('adminUsers.selectProvider') }}</option>
              <option v-for="provider in subscriptionProviders" :key="provider.id" :value="provider.id">
                {{ provider.display_name }}
              </option>
            </select>
          </label>

          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.selectGroup') }}
            <select
              data-testid="subscription-group"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              :value="subscriptionForm.group_id"
              :disabled="subscriptionOptionsLoading || subscriptionForm.loading || bulkGroups.length === 0"
              @change="setBulkGroup(($event.target as HTMLSelectElement).value)"
            >
              <option value="">{{ t('adminUsers.selectGroup') }}</option>
              <option v-for="group in bulkGroups" :key="group.group_id" :value="group.group_id">
                {{ group.group_name }} · {{ group.platform }}
              </option>
            </select>
          </label>

          <label v-if="bulkUsesDays" class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ operationDaysLabel() }}
            <input
              data-testid="subscription-days"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              type="number"
              min="1"
              :value="subscriptionForm.days"
              :disabled="subscriptionForm.loading"
              @input="setBulkDays(($event.target as HTMLInputElement).value)"
            />
          </label>
        </div>

        <label v-if="subscriptionForm.operation === 'remove'" class="mt-3 flex items-start gap-2 text-sm text-amber-900">
          <input
            data-testid="confirm-remove-subscription"
            class="mt-1 h-4 w-4 rounded border-gray-300"
            type="checkbox"
            :checked="subscriptionForm.confirmRemove"
            :disabled="subscriptionForm.loading"
            @change="subscriptionForm.confirmRemove = ($event.target as HTMLInputElement).checked"
          />
          <span>{{ t('adminUsers.confirmRemoveSubscription') }}</span>
        </label>

        <p v-if="subscriptionForm.message" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-700" aria-live="polite">
          {{ subscriptionForm.message }}
        </p>
        <div v-if="subscriptionJob" class="mt-3 flex flex-wrap gap-3 text-xs text-gray-500">
          <span>{{ subscriptionJob.processed_count }} / {{ subscriptionJob.total_count }}</span>
          <span>{{ t('adminUsers.subscriptionSuccessCount', { count: subscriptionJob.success_count }) }}</span>
          <span>{{ t('adminUsers.subscriptionSkippedCount', { count: subscriptionJob.skipped_count }) }}</span>
          <span>{{ t('adminUsers.subscriptionFailedCount', { count: subscriptionJob.failed_count }) }}</span>
        </div>
        <div v-if="subscriptionResults.length > 0" class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          <div
            v-for="result in subscriptionResults.slice(0, 6)"
            :key="`${result.user_id}-${result.status}`"
            class="rounded-md border border-gray-200 p-2 text-xs"
          >
            <div class="font-medium text-gray-900">{{ result.username || result.email || `#${result.user_id}` }}</div>
            <div class="mt-1 text-gray-500">{{ result.status }}<span v-if="result.message"> · {{ result.message }}</span></div>
          </div>
        </div>
      </div>

      <div v-if="filters.view === 'departments'" class="rounded-lg bg-white p-5 shadow">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('adminUsers.departments') }}</h2>
          <span class="text-xs text-gray-500">{{ departments.length }} {{ t('adminUsers.totalSuffix') }}</span>
        </div>
        <p v-if="departmentsError" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ departmentsError }}</p>
        <div v-if="departmentsLoading" class="mt-3 text-sm text-gray-500">{{ t('adminUsers.loading') }}</div>
        <div v-else-if="departments.length === 0" class="mt-3 text-sm text-gray-400">{{ t('adminUsers.noDepartments') }}</div>
	        <div v-else class="mt-3 overflow-hidden rounded-md border border-gray-200" role="tree">
	          <div
	            v-for="department in visibleDepartments"
	            :key="department.external_id"
	            :data-testid="`admin-users-department-open-${department.external_id}`"
	            role="treeitem"
	            :aria-level="departmentAriaLevel(department)"
	            :aria-expanded="(department.child_count ?? 0) > 0 ? departmentExpanded(department) : undefined"
	            :style="departmentIndentStyle(department)"
	            class="flex w-full cursor-pointer flex-col gap-2 border-b border-gray-100 bg-white py-3 pr-4 text-left last:border-b-0 hover:bg-gray-50 sm:flex-row sm:items-center sm:justify-between"
	            tabindex="0"
	            @click="openDepartmentUsers(department)"
	            @keydown.enter.prevent="openDepartmentUsers(department)"
	            @keydown.space.prevent="openDepartmentUsers(department)"
	          >
	            <div class="min-w-0">
	              <div class="flex items-center gap-2">
	                <button
	                  v-if="(department.child_count ?? 0) > 0"
	                  :data-testid="`admin-users-department-toggle-${department.external_id}`"
	                  type="button"
	                  class="inline-flex h-5 w-5 items-center justify-center rounded text-xs text-gray-500 hover:bg-gray-100"
	                  :aria-label="departmentExpanded(department) ? t('adminUsers.collapseDepartment') : t('adminUsers.expandDepartment')"
	                  @click.stop="toggleDepartment(department)"
	                >
	                  {{ departmentExpanded(department) ? '▾' : '▸' }}
	                </button>
	                <span v-else class="inline-flex h-5 w-5" aria-hidden="true"></span>
	                <span class="truncate font-medium text-gray-900">{{ department.name }}</span>
	              </div>
	              <div class="mt-1 truncate text-xs text-gray-500">{{ departmentDisplayLabel(department) }}</div>
	            </div>
            <div class="flex shrink-0 flex-wrap gap-2 text-xs text-gray-600">
              <span class="rounded-full bg-gray-100 px-2 py-0.5">{{ memberCountLabel(department.member_count) }}</span>
              <span class="rounded-full bg-slate-50 px-2 py-0.5 text-slate-700">{{ subtreeMemberCountLabel(department) }}</span>
              <span class="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">{{ matchedUserCountLabel(department.matched_user_count) }}</span>
              <span class="rounded-full bg-teal-50 px-2 py-0.5 text-teal-700">{{ subtreeMatchedUserCountLabel(department) }}</span>
              <span
                v-if="(department.representative_count ?? 0) > 0"
                class="rounded-full bg-indigo-50 px-2 py-0.5 text-indigo-700"
              >
                {{ representativeCountLabel(department) }}
              </span>
            </div>
	          </div>
	        </div>
      </div>

      <div v-if="filters.view === 'users'" class="rounded-lg bg-white p-5 shadow">
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
              <label class="flex min-w-0 items-start gap-3">
                <input
                  :data-testid="`select-user-mobile-${row.id}`"
                  class="mt-1 h-4 w-4 rounded border-gray-300"
                  type="checkbox"
                  :checked="selectedUserIds.has(row.id)"
                  :disabled="subscriptionForm.loading"
                  @change="setUserSelected(row.id, ($event.target as HTMLInputElement).checked)"
                />
                <span class="min-w-0">
                  <span class="block truncate font-medium text-gray-900">{{ row.username }}</span>
                  <span class="block truncate text-xs text-gray-500">{{ row.email }}</span>
                  <span class="mt-1 block font-mono text-[11px] text-gray-400">{{ t('adminUsers.localId') }} #{{ row.id }}</span>
                </span>
              </label>
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
                <dt class="text-gray-400">{{ t('adminUsers.department') }}</dt>
                <dd class="mt-1 text-gray-800">{{ departmentLabel(row) }}</dd>
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
          <table class="min-w-[1080px] divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="w-10 px-3 py-2 text-left font-medium">
                  <input
                    ref="selectAllUsersCheckbox"
                    data-testid="select-all-users"
                    class="h-4 w-4 rounded border-gray-300"
                    type="checkbox"
                    :checked="allVisibleSelected"
                    :aria-checked="visibleSelectionIndeterminate ? 'mixed' : allVisibleSelected"
                    :disabled="subscriptionForm.loading"
                    @change="setAllVisibleSelected(($event.target as HTMLInputElement).checked)"
                  />
                </th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.user') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.role') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.authSource') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.department') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.relayMapping') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.accessStatus') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.created') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.updated') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('adminUsers.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="row in rows" :key="row.id">
                <td class="px-3 py-2 align-top">
                  <input
                    :data-testid="`select-user-${row.id}`"
                    class="h-4 w-4 rounded border-gray-300"
                    type="checkbox"
                    :checked="selectedUserIds.has(row.id)"
                    :disabled="subscriptionForm.loading"
                    @change="setUserSelected(row.id, ($event.target as HTMLInputElement).checked)"
                  />
                </td>
                <td class="px-3 py-2">
                  <div class="font-medium text-gray-900">{{ row.username }}</div>
                  <div class="text-xs text-gray-500">{{ row.email }}</div>
                  <div class="mt-1 font-mono text-[11px] text-gray-400">{{ t('adminUsers.localId') }} #{{ row.id }}</div>
                </td>
                <td class="px-3 py-2 text-gray-700">{{ row.role }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.auth_source }}</td>
                <td class="px-3 py-2 text-gray-700">{{ departmentLabel(row) }}</td>
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
