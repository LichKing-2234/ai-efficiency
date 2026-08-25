<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, useId, watch } from 'vue'
import type { Directive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import AdminDepartmentPicker from '@/components/admin/AdminDepartmentPicker.vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import DepartmentTreeToggle from '@/components/DepartmentTreeToggle.vue'
import {
  disableAdminUserAccess,
  getAdminUserSubscriptionJob,
  getLatestAdminUserSubscriptionJob,
  listAdminUserDepartmentChildren,
  listAdminUsers,
  listAdminUserSubscriptionOptions,
  revealAdminUserRelayPassword,
  startAdminUserSubscriptionJob,
} from '@/api/adminUsers'
import { useI18n } from '@/i18n'
import { authSourceLabel, subscriptionResultStatusLabel, userRoleLabel } from '@/utils/displayLabels'
import { FULL_PAGE_SIZES, fullPageSize, positivePage } from '@/utils/pagination'
import type {
  AdminAssignableSubscriptionProvider,
  AdminDepartmentChildrenResponse,
  AdminDirectoryDepartmentSummary,
  AdminManageSubscriptionsRequest,
  AdminManageSubscriptionsResultRow,
  AdminSubscriptionJob,
  AdminSubscriptionManageOperation,
  AdminSubscriptionManageScope,
  AdminUser,
  AdminUserAccessStatus,
} from '@/types'

type LoadedDepartmentChildren = {
  items: AdminDirectoryDepartmentSummary[]
  page: number
  page_size: number
  total: number
}

type VisibleDepartmentRow = {
  department: AdminDirectoryDepartmentSummary
  depth: number
}

function markAdminUserRow(element: HTMLElement) {
  element.closest('tr')?.setAttribute('data-admin-user-row', '')
}

const vAdminUserRow: Directive<HTMLElement> = {
  mounted: markAdminUserRow,
  updated: markAdminUserRow,
}

function tableAdminUser(row: unknown) {
  return row as AdminUser
}

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const departmentFilterLabelID = useId()
const loading = ref(false)
const error = ref('')
const rows = ref<AdminUser[]>([])
const total = ref(0)
const desktopUserRows = useMediaQuery('(min-width: 1440px)')
const desktopPagination = useMediaQuery('(min-width: 768px)')
const rootDepartments = ref<LoadedDepartmentChildren | null>(null)
const childrenByParentID = ref<Map<string, LoadedDepartmentChildren>>(new Map())
const departmentsLoading = ref(false)
const departmentsError = ref('')
const expandedDepartmentIds = ref<Set<string>>(new Set())
const departmentChildrenLoadingIDs = ref<Set<string>>(new Set())
const departmentChildrenErrors = ref<Map<string, string>>(new Map())
const subscriptionProviders = ref<AdminAssignableSubscriptionProvider[]>([])
const subscriptionOptionsLoading = ref(false)
const subscriptionOptionsError = ref('')
const plaintextDialog = reactive<{
  open: boolean
  user: AdminUser | null
  loading: boolean
  message: string
  messageType: 'success' | 'error' | ''
}>({
  open: false,
  user: null,
  loading: false,
  message: '',
  messageType: '',
})
const disableAccessDialog = reactive<{
  open: boolean
  user: AdminUser | null
  confirmEmail: string
  loading: boolean
  message: string
  messageType: 'success' | 'error' | ''
}>({
  open: false,
  user: null,
  confirmEmail: '',
  loading: false,
  message: '',
  messageType: '',
})
const selectedUserIds = ref<Set<number>>(new Set())
const disableAccessConfirmInput = ref<{ input?: HTMLInputElement } | null>(null)
const subscriptionJob = ref<AdminSubscriptionJob | null>(null)
const subscriptionPanelExpanded = ref(false)
const subscriptionForm = reactive<{
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  provider_id: number | null
  group_id: string
  days: number
  confirmRemove: boolean
  confirmResetQuota: boolean
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
  confirmResetQuota: false,
  loading: false,
  message: '',
  results: [],
})
const subscriptionPanelForcedOpen = computed(() => subscriptionForm.loading)
const subscriptionToolsVisible = computed(() => subscriptionPanelExpanded.value || subscriptionPanelForcedOpen.value)
let searchTimer: number | undefined
let subscriptionJobPollTimer: number | undefined
let userRequestGeneration = 0
let rootDepartmentRequestGeneration = 0
let childDepartmentRequestGeneration = 0

const filters = reactive({
  view: queryString('view') === 'departments' ? 'departments' : 'users',
  q: queryString('q'),
  department_id: queryString('department_id'),
  access_status: queryString('access_status'),
  page: positivePage(route.query.page),
  page_size: fullPageSize(route.query.page_size),
})

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.page_size)))
const pageStart = computed(() => total.value === 0 ? 0 : ((filters.page - 1) * filters.page_size) + 1)
const pageEnd = computed(() => Math.min(total.value, filters.page * filters.page_size))
const showUserPagination = computed(() => rows.value.length > 0 && total.value > filters.page_size)
const activeViewLoading = computed(() => filters.view === 'departments' ? departmentsLoading.value : loading.value)
const showMobileUserRows = computed(() => filters.view === 'users' && rows.value.length > 0 && !desktopUserRows.value)
const showDesktopUserRows = computed(() => filters.view === 'users' && rows.value.length > 0 && desktopUserRows.value)
const selectedUserIdList = computed(() => Array.from(selectedUserIds.value))
const selectedCount = computed(() => selectedUserIdList.value.length)
const allVisibleSelected = computed(() => rows.value.length > 0 && rows.value.every((row) => selectedUserIds.value.has(row.id)))
const visibleSelectionIndeterminate = computed(() => rows.value.some((row) => selectedUserIds.value.has(row.id)) && !allVisibleSelected.value)
const bulkGroups = computed(() => subscriptionProviders.value.find((provider) => provider.id === subscriptionForm.provider_id)?.groups ?? [])
const bulkUsesDays = computed(() => subscriptionForm.operation === 'add' || subscriptionForm.operation === 'extend')
const bulkRequiresRemoveConfirmation = computed(() => subscriptionForm.operation === 'remove')
const bulkRequiresResetConfirmation = computed(() => subscriptionForm.operation === 'reset_quota')
const subscriptionResults = computed(() => subscriptionJob.value?.results ?? subscriptionForm.results)
const visibleSubscriptionResults = computed(() => [...subscriptionResults.value]
  .sort((left, right) => Number(right.status === 'failed') - Number(left.status === 'failed'))
  .slice(0, 6))
const disableAccessConfirmMatches = computed(() => {
  if (!disableAccessDialog.user) return false
  return disableAccessDialog.confirmEmail.trim().toLowerCase() === disableAccessDialog.user.email.trim().toLowerCase()
})
const disableAccessCompleted = computed(() => disableAccessDialog.messageType === 'success')
const visibleDepartmentRows = computed<VisibleDepartmentRow[]>(() => flattenLoadedDepartmentRows(
  rootDepartments.value?.items ?? [],
  childrenByParentID.value,
  expandedDepartmentIds.value,
))
const showRootDepartmentPagination = computed(() => {
  const current = rootDepartments.value
  return current != null && current.page * current.page_size < current.total
    || current != null && current.page > 1
})
const canSubmitSubscriptionManagement = computed(() => {
  if (subscriptionForm.loading || !subscriptionForm.provider_id || !subscriptionForm.group_id) return false
  if (subscriptionForm.scope === 'selected' && selectedCount.value === 0) return false
  if (bulkUsesDays.value && subscriptionForm.days <= 0) return false
  if (subscriptionForm.operation === 'remove' && !subscriptionForm.confirmRemove) return false
  if (subscriptionForm.operation === 'reset_quota' && !subscriptionForm.confirmResetQuota) return false
  return true
})

async function loadUsers(preserveResults = false, pushHistory = false) {
  const generation = ++userRequestGeneration
  loading.value = true
  error.value = ''
	try {
    const params = {
      q: filters.q,
      ...(filters.department_id ? { department_id: filters.department_id } : {}),
      ...(filters.access_status ? { access_status: filters.access_status } : {}),
      page: filters.page,
      page_size: filters.page_size,
    }
    const res = await listAdminUsers(params)
    if (generation !== userRequestGeneration) return undefined
    const data = res.data.data
    rows.value = data?.items ?? []
    total.value = data?.total ?? 0
    filters.page = data?.page ?? filters.page
    filters.page_size = data?.page_size ?? filters.page_size
    syncAdminUsersQuery(pushHistory)
    return true
  } catch (err: any) {
    if (generation !== userRequestGeneration) return undefined
    error.value = err.response?.data?.message || err.message || t('adminUsers.loadFailed')
    if (!preserveResults) {
      rows.value = []
      total.value = 0
    }
    return false
  } finally {
    if (generation === userRequestGeneration) loading.value = false
  }
}

function loadedDepartmentPage(
  data: AdminDepartmentChildrenResponse | undefined,
  requestedPage: number,
): LoadedDepartmentChildren {
  return {
    items: data?.items ?? [],
    page: data?.page ?? requestedPage,
    page_size: data?.page_size ?? 25,
    total: data?.total ?? 0,
  }
}

function flattenLoadedDepartmentRows(
  roots: AdminDirectoryDepartmentSummary[],
  children: Map<string, LoadedDepartmentChildren>,
  expandedIDs: Set<string>,
) {
  const visible: VisibleDepartmentRow[] = []
  const visited = new Set<string>()

  const visit = (departments: AdminDirectoryDepartmentSummary[], depth: number) => {
    for (const department of departments) {
      if (visited.has(department.external_id)) continue
      visited.add(department.external_id)
      visible.push({ department, depth })
      if (!expandedIDs.has(department.external_id)) continue
      visit(children.get(department.external_id)?.items ?? [], depth + 1)
    }
  }

  visit(roots, 0)
  return visible
}

async function loadRootDepartments(page = 1, preserveResults = false) {
  if (departmentsLoading.value) return
  const generation = ++rootDepartmentRequestGeneration
  departmentsLoading.value = true
  departmentsError.value = ''
  try {
    const res = await listAdminUserDepartmentChildren({ page, page_size: 25 })
    if (generation !== rootDepartmentRequestGeneration) return
    rootDepartments.value = loadedDepartmentPage(res.data.data, page)
  } catch (err: any) {
    if (generation !== rootDepartmentRequestGeneration) return
    if (!preserveResults) rootDepartments.value = null
    departmentsError.value = err.response?.data?.message || err.message || t('adminUsers.departmentsLoadFailed')
  } finally {
    if (generation === rootDepartmentRequestGeneration) departmentsLoading.value = false
  }
}

function mergeDepartmentItems(
  existing: AdminDirectoryDepartmentSummary[],
  incoming: AdminDirectoryDepartmentSummary[],
) {
  const seen = new Set<string>()
  const merged: AdminDirectoryDepartmentSummary[] = []
  for (const item of [...existing, ...incoming]) {
    if (seen.has(item.external_id)) continue
    seen.add(item.external_id)
    merged.push(item)
  }
  return merged
}

async function loadDepartmentChildren(parentDepartmentID: string, page = 1, append = false) {
  if (departmentChildrenLoadingIDs.value.has(parentDepartmentID)) return
  const generation = childDepartmentRequestGeneration
  departmentChildrenLoadingIDs.value = new Set(departmentChildrenLoadingIDs.value).add(parentDepartmentID)
  const nextErrors = new Map(departmentChildrenErrors.value)
  nextErrors.delete(parentDepartmentID)
  departmentChildrenErrors.value = nextErrors
  try {
    const res = await listAdminUserDepartmentChildren({
      parent_department_id: parentDepartmentID,
      page,
      page_size: 25,
    })
    if (generation !== childDepartmentRequestGeneration) return
    const loaded = loadedDepartmentPage(res.data.data, page)
    const current = childrenByParentID.value.get(parentDepartmentID)
    if (append && current) loaded.items = mergeDepartmentItems(current.items, loaded.items)
    const nextChildren = new Map(childrenByParentID.value)
    nextChildren.set(parentDepartmentID, loaded)
    childrenByParentID.value = nextChildren
  } catch (err: any) {
    if (generation !== childDepartmentRequestGeneration) return
    const errors = new Map(departmentChildrenErrors.value)
    errors.set(
      parentDepartmentID,
      err.response?.data?.message || err.message || t('adminUsers.departmentsLoadFailed'),
    )
    departmentChildrenErrors.value = errors
  } finally {
    if (generation !== childDepartmentRequestGeneration) return
    const loadingIDs = new Set(departmentChildrenLoadingIDs.value)
    loadingIDs.delete(parentDepartmentID)
    departmentChildrenLoadingIDs.value = loadingIDs
  }
}

function invalidateDepartmentChildren() {
  childDepartmentRequestGeneration += 1
  childrenByParentID.value = new Map()
  expandedDepartmentIds.value = new Set()
  departmentChildrenLoadingIDs.value = new Set()
  departmentChildrenErrors.value = new Map()
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

function syncAdminUsersQuery(pushHistory = false) {
  const query: Record<string, string> = {}
  if (filters.view === 'departments') query.view = 'departments'
  if (filters.q.trim()) query.q = filters.q.trim()
  if (filters.department_id.trim()) query.department_id = filters.department_id.trim()
  if (filters.access_status.trim()) query.access_status = filters.access_status.trim()
  if (filters.page > 1) query.page = String(filters.page)
  if (filters.page_size !== 20) query.page_size = String(filters.page_size)
  if (pushHistory) void router.push({ query })
  else void router.replace({ query })
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
  await loadUsers(false, true)
}

async function changeDepartmentFilter() {
  filters.page = 1
  clearSubscriptionFeedback()
  await loadUsers(false, true)
}

async function changeAccessStatusFilter() {
  filters.page = 1
  clearSubscriptionFeedback()
  await loadUsers(false, true)
}

async function setAdminUsersView(view: 'users' | 'departments') {
  filters.view = view
  filters.page = 1
  syncAdminUsersQuery(true)
  if (view === 'departments' && rootDepartments.value === null && !departmentsLoading.value) {
    await loadRootDepartments(1)
  }
}

function handleAdminUsersViewChange(view: string | number | boolean | undefined) {
  if (view === 'users' || view === 'departments') void setAdminUsersView(view)
}

async function refreshActiveView() {
  if (filters.view === 'departments') {
    invalidateDepartmentChildren()
    await loadRootDepartments(rootDepartments.value?.page ?? 1)
    return
  }
  await loadUsers()
}

async function openDepartmentUsers(department: AdminDirectoryDepartmentSummary) {
  filters.view = 'users'
  filters.department_id = department.external_id
  filters.page = 1
  clearSubscriptionFeedback()
  await loadUsers(false, true)
}

async function changePageSize(pageSize: number) {
  const previousPage = filters.page
  const previousPageSize = filters.page_size
  filters.page_size = pageSize
  filters.page = 1
  if (await loadUsers(true, true) === false) {
    filters.page = previousPage
    filters.page_size = previousPageSize
  }
}

async function changePage(page: number) {
  if (page < 1 || page > totalPages.value || page === filters.page) return
  const previousPage = filters.page
  filters.page = page
  if (await loadUsers(true, true) === false) filters.page = previousPage
}

function formatDate(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString(locale.value)
}

function relayMappingLabel(user: AdminUser) {
  return user.relay_user_id == null ? t('adminUsers.notMapped') : `${t('adminUsers.mapped')} #${user.relay_user_id}`
}

function accessStatusLabel(user: AdminUser) {
  return accessStatusText(adminUserAccessStatus(user))
}

function adminUserAccessStatus(user: AdminUser): AdminUserAccessStatus {
  if (user.access_status === 'disabled' || user.token_valid_after || user.relay_disabled_at || user.offboarding_status === 'succeeded') {
    return 'disabled'
  }
  if (user.access_status === 'configured' || user.relay_auth_password) {
    return 'configured'
  }
  return 'missing_credential'
}

function accessStatusText(status: AdminUserAccessStatus | '') {
  if (status === 'disabled') return t('adminUsers.disabled')
  if (status === 'configured') return t('adminUsers.configured')
  if (status === 'missing_credential') return t('adminUsers.missingRelayCredential')
  return t('adminUsers.allAccessStatuses')
}

function accessStatusTagType(user: AdminUser): 'danger' | 'success' | 'warning' {
  const status = adminUserAccessStatus(user)
  if (status === 'disabled') return 'danger'
  if (status === 'configured') return 'success'
  return 'warning'
}

function canDisableAccess(user: AdminUser) {
  return adminUserAccessStatus(user) !== 'disabled' && user.relay_user_id != null
}

function isDisablingAccess(user: AdminUser) {
  return disableAccessDialog.loading && disableAccessDialog.user?.id === user.id
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

function departmentDepth(depthValue: number) {
  const depth = Number(depthValue)
  return Number.isFinite(depth) && depth > 0 ? Math.min(depth, 8) : 0
}

function departmentIndentStyle(depthValue: number) {
  const depth = departmentDepth(depthValue)
  return { paddingLeft: depth === 0 ? '1rem' : `${depth * 1.25}rem` }
}

function departmentAriaLevel(depthValue: number) {
  return String(departmentDepth(depthValue) + 1)
}

function departmentExpanded(department: AdminDirectoryDepartmentSummary) {
  return expandedDepartmentIds.value.has(department.external_id)
}

function departmentHasChildren(department: AdminDirectoryDepartmentSummary) {
  return department.has_children || department.child_count > 0
}

async function toggleDepartment(department: AdminDirectoryDepartmentSummary) {
  if (!departmentHasChildren(department)) return
  const next = new Set(expandedDepartmentIds.value)
  if (next.has(department.external_id)) {
    next.delete(department.external_id)
    expandedDepartmentIds.value = next
    return
  } else {
    next.add(department.external_id)
  }
  expandedDepartmentIds.value = next
  if (!childrenByParentID.value.has(department.external_id)) {
    await loadDepartmentChildren(department.external_id, 1)
  }
}

function departmentChildrenLoading(departmentID: string) {
  return departmentChildrenLoadingIDs.value.has(departmentID)
}

function departmentChildrenError(departmentID: string) {
  return departmentChildrenErrors.value.get(departmentID) ?? ''
}

function departmentChildrenEmpty(departmentID: string) {
  const loaded = childrenByParentID.value.get(departmentID)
  return loaded != null && loaded.items.length === 0
}

function canLoadMoreDepartmentChildren(departmentID: string) {
  const loaded = childrenByParentID.value.get(departmentID)
  return loaded != null && loaded.page * loaded.page_size < loaded.total
}

async function loadMoreDepartmentChildren(departmentID: string) {
  const loaded = childrenByParentID.value.get(departmentID)
  if (!loaded || loaded.page * loaded.page_size >= loaded.total) return
  await loadDepartmentChildren(departmentID, loaded.page + 1, true)
}

async function changeRootDepartmentPage(page: number) {
  const current = rootDepartments.value
  if (!current || page === current.page || departmentsLoading.value) return
  await loadRootDepartments(page, true)
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
  const wasActive = isActiveSubscriptionJob(subscriptionJob.value)
  const isActive = isActiveSubscriptionJob(job)
  subscriptionJob.value = job
  subscriptionForm.results = job.results ?? []
  subscriptionForm.message = subscriptionJobMessage(job)
  subscriptionForm.loading = isActive
  if (wasActive && !isActive) subscriptionPanelExpanded.value = true
  if (!isActive) {
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
    subscriptionPanelExpanded.value = true
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
    if (job) {
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
  subscriptionForm.confirmRemove = false
  subscriptionForm.confirmResetQuota = false
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
  if (value === 'add' || value === 'extend' || value === 'remove' || value === 'reset_quota') {
    subscriptionForm.operation = value
    subscriptionForm.confirmRemove = false
    subscriptionForm.confirmResetQuota = false
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

function scopeSummaryLabel() {
  if (subscriptionForm.scope === 'selected') {
    return t('adminUsers.selectedUsersCount', { count: selectedCount.value })
  }
  if (subscriptionForm.scope === 'current_filter') {
    if (filters.q.trim()) return t('adminUsers.currentFilterScopeWithQuery', { query: filters.q.trim() })
    if (filters.department_id.trim()) return t('adminUsers.currentFilterScopeWithDepartment')
    if (filters.access_status.trim()) return t('adminUsers.currentFilterScopeWithStatus', {
      status: accessStatusText(filters.access_status as AdminUserAccessStatus),
    })
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
    payload.filters = {
      q: filters.q.trim(),
      department_id: filters.department_id.trim(),
      access_status: filters.access_status.trim(),
    }
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
    ElMessage.error(t('adminUsers.noEncryptedPassword'))
    return
  }
  try {
    await navigator.clipboard.writeText(user.relay_auth_password)
    ElMessage.success(t('adminUsers.copiedEncrypted'))
  } catch (err: any) {
    ElMessage.error(err.message || t('adminUsers.copyFailed'))
  }
}

function requestPlaintextCopy(user: AdminUser) {
  ElMessage.closeAll()
  plaintextDialog.open = true
  plaintextDialog.user = user
  plaintextDialog.loading = false
  plaintextDialog.message = ''
  plaintextDialog.messageType = ''
}

function requestDisableAccess(user: AdminUser) {
  ElMessage.closeAll()
  disableAccessDialog.open = true
  disableAccessDialog.user = user
  disableAccessDialog.confirmEmail = ''
  disableAccessDialog.loading = false
  disableAccessDialog.message = ''
  disableAccessDialog.messageType = ''
  void focusDisableAccessConfirmation()
}

function closePlaintextDialog() {
  if (plaintextDialog.loading) return
  plaintextDialog.open = false
  plaintextDialog.user = null
  plaintextDialog.message = ''
  plaintextDialog.messageType = ''
}

function closeDisableAccessDialog() {
  if (disableAccessDialog.loading) return
  disableAccessDialog.open = false
  disableAccessDialog.user = null
  disableAccessDialog.confirmEmail = ''
  disableAccessDialog.message = ''
  disableAccessDialog.messageType = ''
}

async function focusDisableAccessConfirmation() {
  await nextTick()
  await nextTick()
  await nextTick()
  disableAccessConfirmInput.value?.input?.focus()
}

async function confirmCopyPlaintext() {
  const user = plaintextDialog.user
  if (!user) return
  plaintextDialog.loading = true
  plaintextDialog.message = ''
  plaintextDialog.messageType = ''
  try {
    const res = await revealAdminUserRelayPassword(user.id)
    const password = res.data.data?.password || ''
    if (!password) {
      plaintextDialog.message = t('adminUsers.noPlaintextReturned')
      plaintextDialog.messageType = 'error'
      return
    }
    await navigator.clipboard.writeText(password)
    plaintextDialog.message = t('adminUsers.copiedPlaintext')
    plaintextDialog.messageType = 'success'
  } catch (err: any) {
    plaintextDialog.message = err.response?.data?.message || err.message || t('adminUsers.copyFailed')
    plaintextDialog.messageType = 'error'
  } finally {
    plaintextDialog.loading = false
  }
}

async function confirmDisableAccess() {
  const user = disableAccessDialog.user
  if (!user || !disableAccessConfirmMatches.value) return
  disableAccessDialog.loading = true
  disableAccessDialog.message = ''
  disableAccessDialog.messageType = ''
  try {
    const res = await disableAdminUserAccess(user.id, { confirm_email: disableAccessDialog.confirmEmail.trim() })
    const disabledAt = res.data.data?.relay_disabled_at || new Date().toISOString()
    const index = rows.value.findIndex((row) => row.id === user.id)
    if (index >= 0) {
      rows.value[index] = {
        ...rows.value[index],
        access_status: 'disabled',
        relay_disabled_at: disabledAt,
      }
    }
    disableAccessDialog.message = t('adminUsers.disabledUser', { email: user.email })
    disableAccessDialog.messageType = 'success'
  } catch (err: any) {
    disableAccessDialog.message = err.response?.data?.message || err.message || t('adminUsers.disableAccessFailed')
    disableAccessDialog.messageType = 'error'
  } finally {
    disableAccessDialog.loading = false
  }
}

watch(
			() => filters.q,
  () => {
    if (filters.q === queryString('q')) return
    userRequestGeneration += 1
    clearSearchTimer()
    searchTimer = window.setTimeout(() => {
      void applySearch()
    }, 300)
	}
)

watch(
  () => route.fullPath,
  () => {
    const next = {
      view: queryString('view') === 'departments' ? 'departments' as const : 'users' as const,
      q: queryString('q'),
      department_id: queryString('department_id'),
      access_status: queryString('access_status'),
      page: positivePage(route.query.page),
      page_size: fullPageSize(route.query.page_size),
    }
    if (
      filters.view === next.view
      && filters.q === next.q
      && filters.department_id === next.department_id
      && filters.access_status === next.access_status
      && filters.page === next.page
      && filters.page_size === next.page_size
    ) return
    clearSearchTimer()
    Object.assign(filters, next)
    clearSubscriptionFeedback()
    if (next.view === 'departments') {
      invalidateDepartmentChildren()
      void loadRootDepartments(1)
    } else {
      void loadUsers()
    }
  },
)

onMounted(() => {
  void loadUsers()
  if (filters.view === 'departments') void loadRootDepartments(1)
  void loadSubscriptionOptions()
  void recoverLatestSubscriptionJob()
})
onBeforeUnmount(() => {
  userRequestGeneration += 1
  rootDepartmentRequestGeneration += 1
  childDepartmentRequestGeneration += 1
  clearSearchTimer()
  stopSubscriptionJobPolling()
})
</script>

<template>
  <AppLayout>
    <div class="flex flex-col gap-5">
      <div class="order-1 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">{{ t('nav.userManagement') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('adminUsers.subtitle') }}</p>
        </div>
        <ElButton
          data-testid="admin-users-refresh"
          class="shrink-0 self-start whitespace-nowrap sm:self-auto"
          :disabled="activeViewLoading"
          @click="refreshActiveView"
        >
          {{ activeViewLoading ? t('adminUsers.loading') : t('adminUsers.refresh') }}
        </ElButton>
      </div>

      <ElRadioGroup class="order-2" :model-value="filters.view" @change="handleAdminUsersViewChange">
        <ElRadioButton
          data-testid="admin-users-view-users"
          value="users"
        >
          {{ t('adminUsers.userView') }}
        </ElRadioButton>
        <ElRadioButton
          data-testid="admin-users-view-departments"
          value="departments"
        >
          {{ t('adminUsers.departmentView') }}
        </ElRadioButton>
      </ElRadioGroup>

      <div v-if="filters.view === 'users'" class="order-3 rounded-lg bg-white p-4 shadow">
        <div data-testid="admin-users-filter-grid" class="grid gap-3 xl:grid-cols-[minmax(0,1fr)_220px_180px_auto]">
          <label data-testid="admin-users-search-field" class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ t('adminUsers.search') }}
            <ElInput
              v-model="filters.q"
              data-testid="admin-users-search"
              class="mt-1 w-full"
              :placeholder="t('adminUsers.searchPlaceholder')"
              @keyup.enter="applySearch"
            />
          </label>
	          <div class="text-xs font-medium uppercase tracking-wide text-gray-500">
	            <span :id="departmentFilterLabelID" data-testid="admin-users-department-label">{{ t('adminUsers.department') }}</span>
	            <AdminDepartmentPicker
	              v-model="filters.department_id"
	              :labelled-by="departmentFilterLabelID"
	              @change="changeDepartmentFilter"
	            />
	          </div>
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500">
            <span>{{ t('adminUsers.accessStatus') }}</span>
            <ElSelect
              v-model="filters.access_status"
              data-testid="admin-users-access-status-filter"
              class="mt-1 w-full"
              :teleported="false"
              :aria-label="t('adminUsers.accessStatus')"
              @change="changeAccessStatusFilter"
            >
              <ElOption data-testid="admin-users-access-status-option-all" value="" :label="t('adminUsers.allAccessStatuses')" />
              <ElOption data-testid="admin-users-access-status-option-configured" value="configured" :label="t('adminUsers.configured')" />
              <ElOption data-testid="admin-users-access-status-option-disabled" value="disabled" :label="t('adminUsers.disabled')" />
              <ElOption data-testid="admin-users-access-status-option-missing-credential" value="missing_credential" :label="t('adminUsers.missingRelayCredential')" />
            </ElSelect>
          </div>
          <div class="flex items-end">
            <ElButton
              data-testid="admin-users-search-button"
              type="primary"
              :disabled="loading"
              @click="applySearch"
            >
              {{ t('adminUsers.search') }}
            </ElButton>
          </div>
        </div>
        <ElAlert v-if="error" class="mt-3" type="error" :closable="false" show-icon :title="error" />
	      </div>

      <div
        v-if="filters.view === 'users'"
        data-testid="admin-users-subscription-panel"
        class="order-5 rounded-lg bg-white p-4 shadow"
      >
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('adminUsers.subscriptionManagement') }}</h2>
            <p class="mt-1 text-sm text-gray-500">{{ scopeSummaryLabel() }}</p>
          </div>
          <ElButton
            data-testid="admin-users-subscription-toggle"
            class="shrink-0"
            :disabled="subscriptionPanelForcedOpen"
            @click="subscriptionPanelExpanded = !subscriptionPanelExpanded"
          >
            {{ subscriptionToolsVisible ? t('user.hide') : t('adminUsers.subscriptionManagement') }}
          </ElButton>
        </div>

        <div
          v-show="subscriptionToolsVisible"
          data-testid="admin-users-subscription-tools"
        >
          <div class="mt-4 flex justify-end">
            <ElButton
              data-testid="manage-subscriptions-submit"
              type="primary"
              :disabled="!canSubmitSubscriptionManagement"
              @click="submitSubscriptionManagement"
            >
              {{ subscriptionForm.loading ? t('adminUsers.working') : t('adminUsers.applySubscriptionChange') }}
            </ElButton>
          </div>

          <ElAlert v-if="subscriptionOptionsError" class="mt-3" type="error" :closable="false" show-icon :title="subscriptionOptionsError" />

        <div class="mt-4 grid gap-3 lg:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_130px]">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500">
            <span>{{ t('adminUsers.subscriptionScope') }}</span>
            <ElSelect
              data-testid="subscription-scope"
              class="mt-1 w-full"
              :model-value="subscriptionForm.scope"
              :teleported="false"
              :aria-label="t('adminUsers.subscriptionScope')"
              :disabled="subscriptionForm.loading"
              @change="setSubscriptionScope(String($event))"
            >
              <ElOption data-testid="subscription-scope-option-selected" value="selected" :label="t('adminUsers.scopeSelected')" />
              <ElOption data-testid="subscription-scope-option-current-filter" value="current_filter" :label="t('adminUsers.scopeCurrentFilter')" />
              <ElOption data-testid="subscription-scope-option-all-mapped" value="all_mapped" :label="t('adminUsers.scopeAllMapped')" />
            </ElSelect>
          </div>

          <div class="text-xs font-medium uppercase tracking-wide text-gray-500">
            <span>{{ t('adminUsers.subscriptionOperation') }}</span>
            <ElSelect
              data-testid="subscription-operation"
              class="mt-1 w-full"
              :model-value="subscriptionForm.operation"
              :teleported="false"
              :aria-label="t('adminUsers.subscriptionOperation')"
              :disabled="subscriptionForm.loading"
              @change="setSubscriptionOperation(String($event))"
            >
              <ElOption data-testid="subscription-operation-option-add" value="add" :label="t('adminUsers.operationAdd')" />
              <ElOption data-testid="subscription-operation-option-extend" value="extend" :label="t('adminUsers.operationExtend')" />
              <ElOption data-testid="subscription-operation-option-remove" value="remove" :label="t('adminUsers.operationRemove')" />
              <ElOption data-testid="subscription-operation-option-reset-quota" value="reset_quota" :label="t('adminUsers.operationResetQuota')" />
            </ElSelect>
          </div>

          <div class="text-xs font-medium uppercase tracking-wide text-gray-500">
            <span>{{ t('adminUsers.selectProvider') }}</span>
            <ElSelect
              data-testid="subscription-provider"
              class="mt-1 w-full"
              :model-value="subscriptionForm.provider_id ?? ''"
              :teleported="false"
              :aria-label="t('adminUsers.selectProvider')"
              :disabled="subscriptionOptionsLoading || subscriptionForm.loading"
              @change="setBulkProvider(String($event))"
            >
              <ElOption data-testid="subscription-provider-option-empty" value="" :label="t('adminUsers.selectProvider')" />
              <ElOption
                v-for="provider in subscriptionProviders"
                :key="provider.id"
                :data-testid="`subscription-provider-option-${provider.id}`"
                :value="provider.id"
                :label="provider.display_name"
              />
            </ElSelect>
          </div>

          <div class="text-xs font-medium uppercase tracking-wide text-gray-500">
            <span>{{ t('adminUsers.selectGroup') }}</span>
            <ElSelect
              data-testid="subscription-group"
              class="mt-1 w-full"
              :model-value="subscriptionForm.group_id"
              :teleported="false"
              :aria-label="t('adminUsers.selectGroup')"
              :disabled="subscriptionOptionsLoading || subscriptionForm.loading || bulkGroups.length === 0"
              @change="setBulkGroup(String($event))"
            >
              <ElOption data-testid="subscription-group-option-empty" value="" :label="t('adminUsers.selectGroup')" />
              <ElOption
                v-for="group in bulkGroups"
                :key="group.group_id"
                :data-testid="`subscription-group-option-${group.group_id}`"
                :value="group.group_id"
                :label="`${group.group_name} · ${group.platform}`"
              />
            </ElSelect>
          </div>

          <label v-if="bulkUsesDays" class="text-xs font-medium uppercase tracking-wide text-gray-500">
            {{ operationDaysLabel() }}
            <ElInput
              data-testid="subscription-days"
              class="mt-1 w-full"
              type="number"
              min="1"
              :model-value="String(subscriptionForm.days)"
              :disabled="subscriptionForm.loading"
              @input="setBulkDays(String($event))"
            />
          </label>
        </div>

        <ElCheckbox
          v-if="bulkRequiresRemoveConfirmation"
          data-testid="confirm-remove-subscription"
          class="mt-3"
          :model-value="subscriptionForm.confirmRemove"
          :disabled="subscriptionForm.loading"
          @change="subscriptionForm.confirmRemove = Boolean($event)"
        >
          {{ t('adminUsers.confirmRemoveSubscription') }}
        </ElCheckbox>

        <ElCheckbox
          v-if="bulkRequiresResetConfirmation"
          data-testid="confirm-reset-subscription-quota"
          class="mt-3"
          :model-value="subscriptionForm.confirmResetQuota"
          :disabled="subscriptionForm.loading"
          @change="subscriptionForm.confirmResetQuota = Boolean($event)"
        >
          {{ t('adminUsers.confirmResetSubscriptionQuota') }}
        </ElCheckbox>

        <ElAlert v-if="subscriptionForm.message" class="mt-3" type="info" :closable="false" :title="subscriptionForm.message" />
        <div v-if="subscriptionJob" class="mt-3 flex flex-wrap gap-3 text-xs text-gray-500">
          <span>{{ subscriptionJob.processed_count }} / {{ subscriptionJob.total_count }}</span>
          <span>{{ t('adminUsers.subscriptionSuccessCount', { count: subscriptionJob.success_count }) }}</span>
          <span>{{ t('adminUsers.subscriptionSkippedCount', { count: subscriptionJob.skipped_count }) }}</span>
          <span>{{ t('adminUsers.subscriptionFailedCount', { count: subscriptionJob.failed_count }) }}</span>
        </div>
        <div v-if="visibleSubscriptionResults.length > 0" class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          <div
            v-for="result in visibleSubscriptionResults"
            :key="`${result.user_id}-${result.status}`"
            :data-testid="`subscription-result-${result.user_id}`"
            class="rounded-md border border-gray-200 p-2 text-xs"
          >
            <div class="font-medium text-gray-900">{{ result.username || result.email || `#${result.user_id}` }}</div>
            <div class="mt-1 text-gray-500">{{ subscriptionResultStatusLabel(result.status, t) }}<span v-if="result.message"> · {{ result.message }}</span></div>
          </div>
        </div>
        </div>
      </div>

      <div v-if="filters.view === 'departments'" class="order-4 rounded-lg bg-white p-5 shadow">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('adminUsers.departments') }}</h2>
          <span class="text-xs text-gray-500">{{ rootDepartments?.total ?? 0 }} {{ t('adminUsers.totalSuffix') }}</span>
        </div>
        <ElAlert v-if="departmentsError" class="mt-3" type="error" :closable="false" show-icon :title="departmentsError" />
        <div v-if="departmentsLoading && !rootDepartments" class="mt-3 text-sm text-gray-500">{{ t('adminUsers.loading') }}</div>
        <div v-else-if="visibleDepartmentRows.length === 0" class="mt-3 text-sm text-gray-400">{{ t('adminUsers.noDepartments') }}</div>
		        <div v-else class="mt-3 overflow-hidden rounded-md border border-gray-200" role="tree">
	          <div
	            v-for="visibleRow in visibleDepartmentRows"
	            :key="visibleRow.department.external_id"
	            :data-testid="`admin-users-department-open-${visibleRow.department.external_id}`"
	            role="treeitem"
	            :aria-level="departmentAriaLevel(visibleRow.depth)"
	            :aria-expanded="departmentHasChildren(visibleRow.department) ? departmentExpanded(visibleRow.department) : undefined"
	            :style="departmentIndentStyle(visibleRow.depth)"
	            class="flex w-full cursor-pointer flex-col gap-2 border-b border-gray-100 bg-white py-3 pr-4 text-left last:border-b-0 hover:bg-gray-50 sm:flex-row sm:items-center sm:justify-between"
	            tabindex="0"
	            @click="openDepartmentUsers(visibleRow.department)"
	            @keydown.enter.prevent="openDepartmentUsers(visibleRow.department)"
	            @keydown.space.prevent="openDepartmentUsers(visibleRow.department)"
	          >
	            <div class="min-w-0">
	              <div class="flex items-center gap-2">
	                <DepartmentTreeToggle
	                  v-if="departmentHasChildren(visibleRow.department)"
	                  :data-testid="`admin-users-department-toggle-${visibleRow.department.external_id}`"
	                  :expanded="departmentExpanded(visibleRow.department)"
	                  :expanded-label="t('adminUsers.collapseDepartment')"
	                  :collapsed-label="t('adminUsers.expandDepartment')"
	                  @toggle="toggleDepartment(visibleRow.department)"
	                />
	                <span v-else class="inline-flex h-7 w-7" aria-hidden="true"></span>
	                <span class="truncate font-medium text-gray-900">{{ visibleRow.department.name }}</span>
	              </div>
	              <div class="mt-1 truncate text-xs text-gray-500">{{ departmentDisplayLabel(visibleRow.department) }}</div>
	              <div
	                v-if="departmentExpanded(visibleRow.department) && departmentChildrenLoading(visibleRow.department.external_id)"
	                class="mt-1 text-xs text-gray-500"
	              >
	                {{ t('adminUsers.loading') }}
	              </div>
	              <div
	                v-else-if="departmentExpanded(visibleRow.department) && departmentChildrenError(visibleRow.department.external_id)"
	                :data-testid="`admin-users-department-children-error-${visibleRow.department.external_id}`"
	                class="mt-1 text-xs text-red-700"
	              >
	                {{ departmentChildrenError(visibleRow.department.external_id) }}
	              </div>
	              <div
	                v-else-if="departmentExpanded(visibleRow.department) && departmentChildrenEmpty(visibleRow.department.external_id)"
	                :data-testid="`admin-users-department-children-empty-${visibleRow.department.external_id}`"
	                class="mt-1 text-xs text-gray-400"
		              >
		                {{ t('adminUsers.noDepartments') }}
		              </div>
		            </div>
            <div class="flex shrink-0 flex-wrap items-center gap-2 text-xs text-gray-600">
              <span class="rounded-full bg-gray-100 px-2 py-0.5">{{ memberCountLabel(visibleRow.department.member_count) }}</span>
              <span class="rounded-full bg-slate-50 px-2 py-0.5 text-slate-700">{{ subtreeMemberCountLabel(visibleRow.department) }}</span>
              <span class="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">{{ matchedUserCountLabel(visibleRow.department.matched_user_count) }}</span>
              <span class="rounded-full bg-teal-50 px-2 py-0.5 text-teal-700">{{ subtreeMatchedUserCountLabel(visibleRow.department) }}</span>
              <span
                v-if="visibleRow.department.representative_count > 0"
                class="rounded-full bg-indigo-50 px-2 py-0.5 text-indigo-700"
              >
                {{ representativeCountLabel(visibleRow.department) }}
              </span>
	          <ElButton
	            v-if="departmentExpanded(visibleRow.department) && canLoadMoreDepartmentChildren(visibleRow.department.external_id)"
	            :data-testid="`admin-users-department-load-more-${visibleRow.department.external_id}`"
	            :disabled="departmentChildrenLoading(visibleRow.department.external_id)"
	            @click.stop="loadMoreDepartmentChildren(visibleRow.department.external_id)"
		            @keydown.enter.prevent.stop="loadMoreDepartmentChildren(visibleRow.department.external_id)"
		            @keydown.space.prevent.stop="loadMoreDepartmentChildren(visibleRow.department.external_id)"
	          >
		            {{ t('teamUsage.loadMoreDepartments') }}
	          </ElButton>
			          </div>
			        </div>
        </div>
        <div v-if="showRootDepartmentPagination" class="mt-4 flex justify-end border-t border-slate-200 pt-4">
          <ElPagination
            data-testid="admin-users-department-pagination"
            size="small"
            background
            :layout="desktopPagination ? 'prev, pager, next' : 'prev, slot, next'"
            :pager-count="5"
            :current-page="rootDepartments?.page ?? 1"
            :page-size="rootDepartments?.page_size ?? 25"
            :total="rootDepartments?.total ?? 0"
            :disabled="departmentsLoading"
            @current-change="changeRootDepartmentPage"
          >
            <span v-if="!desktopPagination" class="px-1 text-xs text-gray-500">
              {{ t('pagination.pageOf', {
                page: rootDepartments?.page ?? 1,
                pages: Math.max(1, Math.ceil((rootDepartments?.total ?? 0) / (rootDepartments?.page_size ?? 25))),
              }) }}
            </span>
          </ElPagination>
        </div>
      </div>

      <div v-if="filters.view === 'users'" data-testid="admin-users-list-panel" class="order-4 rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">{{ t('adminUsers.localUsers') }}</h2>

	        <div v-if="showMobileUserRows" data-admin-user-list="mobile" class="mt-3 divide-y divide-slate-200 border-y border-slate-200">
	          <div
	            v-for="row in rows"
	            :key="row.id"
	            data-admin-user-row
	            class="bg-white py-4"
	          >
            <div class="flex items-start justify-between gap-3">
              <label class="flex min-w-0 items-start gap-3">
                <ElCheckbox
                  :data-testid="`select-user-mobile-${row.id}`"
                  class="mt-1"
                  :model-value="selectedUserIds.has(row.id)"
                  :disabled="subscriptionForm.loading"
                  @change="setUserSelected(row.id, Boolean($event))"
                />
                <span class="min-w-0">
                  <span class="block truncate font-medium text-gray-900">{{ row.username }}</span>
                  <span :data-testid="`admin-user-mobile-email-${row.id}`" class="block break-all text-xs text-gray-500">{{ row.email }}</span>
                  <span class="mt-1 block font-mono text-[11px] text-gray-400">{{ t('adminUsers.localId') }} #{{ row.id }}</span>
                </span>
              </label>
              <ElTag class="shrink-0" :type="accessStatusTagType(row)" effect="light">
                {{ accessStatusLabel(row) }}
              </ElTag>
            </div>
            <dl class="mt-3 grid grid-cols-2 gap-3 text-xs">
              <div>
                <dt class="text-gray-400">{{ t('adminUsers.role') }}</dt>
                <dd class="mt-1 text-gray-800">{{ userRoleLabel(row.role, t) }}</dd>
              </div>
              <div>
                <dt class="text-gray-400">{{ t('adminUsers.authSource') }}</dt>
                <dd class="mt-1 text-gray-800">{{ authSourceLabel(row.auth_source, t) }}</dd>
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
            <div :data-testid="`admin-user-mobile-actions-${row.id}`" class="mt-3 grid grid-cols-2 gap-2">
              <ElButton
                :data-testid="`copy-encrypted-${row.id}`"
                class="!ml-0 w-full"
                :disabled="!row.relay_auth_password"
                @click="copyEncrypted(row)"
              >
                {{ t('adminUsers.copyEncrypted') }}
              </ElButton>
              <ElButton
                :data-testid="`copy-plaintext-${row.id}`"
                class="!ml-0 w-full"
                :disabled="!row.relay_auth_password"
                @click="requestPlaintextCopy(row)"
              >
                {{ t('adminUsers.copyPlaintext') }}
              </ElButton>
              <ElButton
                v-if="canDisableAccess(row)"
                :data-testid="`disable-access-${row.id}`"
                type="danger"
                plain
                class="!ml-0 col-span-2 w-full"
                :disabled="isDisablingAccess(row)"
                @click="requestDisableAccess(row)"
              >
                {{ t('adminUsers.disableUser') }}
              </ElButton>
            </div>
          </div>
        </div>

        <div v-if="showDesktopUserRows" data-admin-user-list="desktop" class="mt-3">
          <ElTable :data="rows" row-key="id" class="w-full">
            <ElTableColumn width="48" align="center">
              <template #header>
                <ElCheckbox
                  data-testid="select-all-users"
                  :model-value="allVisibleSelected"
                  :indeterminate="visibleSelectionIndeterminate"
                  :disabled="subscriptionForm.loading"
                  @change="setAllVisibleSelected(Boolean($event))"
                />
              </template>
              <template #default="{ row }">
                <span v-admin-user-row>
                  <ElCheckbox
                    :data-testid="`select-user-${row.id}`"
                    :model-value="selectedUserIds.has(row.id)"
                    :disabled="subscriptionForm.loading"
                    @change="setUserSelected(row.id, Boolean($event))"
                  />
                </span>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.user')" min-width="150">
              <template #default="{ row }">
                <div class="font-medium text-gray-900">{{ row.username }}</div>
                <div class="text-xs text-gray-500">{{ row.email }}</div>
                <div class="mt-1 font-mono text-[11px] text-gray-400">{{ t('adminUsers.localId') }} #{{ row.id }}</div>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.role')" min-width="70">
              <template #default="{ row }">{{ userRoleLabel(row.role, t) }}</template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.authSource')" min-width="115">
              <template #default="{ row }">{{ authSourceLabel(row.auth_source, t) }}</template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.department')" min-width="125">
              <template #default="{ row }">{{ departmentLabel(tableAdminUser(row)) }}</template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.relayMapping')" min-width="100">
              <template #default="{ row }">{{ relayMappingLabel(tableAdminUser(row)) }}</template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.accessStatus')" min-width="115">
              <template #default="{ row }">
                <ElTag
                  class="!h-auto max-w-full !whitespace-normal py-1 text-center !leading-tight"
                  :type="accessStatusTagType(tableAdminUser(row))"
                  effect="light"
                >
                  {{ accessStatusLabel(tableAdminUser(row)) }}
                </ElTag>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="`${t('adminUsers.created')} / ${t('adminUsers.updated')}`" min-width="180">
              <template #default="{ row }">
                <dl :data-testid="`admin-user-timestamps-${row.id}`" class="space-y-1 text-xs">
                  <div>
                    <dt class="text-gray-400">{{ t('adminUsers.created') }}</dt>
                    <dd class="whitespace-nowrap text-gray-700">{{ formatDate(row.created_at) }}</dd>
                  </div>
                  <div>
                    <dt class="text-gray-400">{{ t('adminUsers.updated') }}</dt>
                    <dd class="whitespace-nowrap text-gray-700">{{ formatDate(row.updated_at) }}</dd>
                  </div>
                </dl>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('adminUsers.actions')" min-width="150">
              <template #default="{ row }">
                <div :data-testid="`admin-user-desktop-actions-${row.id}`" class="flex flex-col gap-1">
                  <ElButton
                    :data-testid="`copy-encrypted-${row.id}`"
                    class="!ml-0"
                    :disabled="!row.relay_auth_password"
                    @click="copyEncrypted(tableAdminUser(row))"
                  >
                    {{ t('adminUsers.copyEncrypted') }}
                  </ElButton>
                  <ElButton
                    :data-testid="`copy-plaintext-${row.id}`"
                    class="!ml-0"
                    :disabled="!row.relay_auth_password"
                    @click="requestPlaintextCopy(tableAdminUser(row))"
                  >
                    {{ t('adminUsers.copyPlaintext') }}
                  </ElButton>
                  <ElButton
                    v-if="canDisableAccess(tableAdminUser(row))"
                    :data-testid="`disable-access-${row.id}`"
                    type="danger"
                    plain
                    class="!ml-0"
                    :disabled="isDisablingAccess(tableAdminUser(row))"
                    @click="requestDisableAccess(tableAdminUser(row))"
                  >
                    {{ t('adminUsers.disableUser') }}
                  </ElButton>
                </div>
              </template>
            </ElTableColumn>
          </ElTable>
	        </div>
	        <ElEmpty v-if="!error && rows.length === 0" class="mt-3" :description="t('adminUsers.empty')" />
        <div
          v-if="showUserPagination"
          class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-slate-200 pt-4"
        >
          <span
            v-if="desktopPagination"
            data-testid="admin-users-page-range"
            class="text-sm text-slate-500"
          >
            {{ t('pagination.range', { start: pageStart, end: pageEnd, total }) }}
          </span>
          <ElPagination
            data-testid="admin-users-pagination"
            class="ml-auto"
            background
            :current-page="filters.page"
            :page-size="filters.page_size"
            :page-sizes="FULL_PAGE_SIZES"
            :total="total"
            :pager-count="5"
            :disabled="loading"
            :layout="desktopPagination ? 'sizes, prev, pager, next' : 'prev, slot, next'"
            @current-change="changePage"
            @size-change="changePageSize"
          >
            <span v-if="!desktopPagination" class="px-2 text-sm text-slate-600">
              {{ t('pagination.pageOf', { page: filters.page, pages: totalPages }) }}
            </span>
          </ElPagination>
        </div>
      </div>
    </div>

    <ElDialog
      v-if="plaintextDialog.user"
      :model-value="plaintextDialog.open"
      append-to-body
      :show-close="false"
      align-center
      width="min(100%, 28rem)"
      :close-on-click-modal="!plaintextDialog.loading"
      :close-on-press-escape="!plaintextDialog.loading"
      @update:model-value="(value) => { if (!value) closePlaintextDialog() }"
    >
      <template #header>
        <div data-testid="plaintext-dialog" class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-gray-900">{{ t('adminUsers.copyPlaintext') }}</h2>
            <p class="mt-1 truncate text-sm text-gray-500">{{ plaintextDialog.user.email }}</p>
          </div>
          <ElButton
            data-testid="plaintext-dialog-close"
            class="shrink-0"
            :disabled="plaintextDialog.loading"
            @click="closePlaintextDialog"
          >
            {{ t('adminUsers.closeDialog') }}
          </ElButton>
        </div>
      </template>
      <ElAlert
        type="warning"
        :closable="false"
        show-icon
        :title="t('adminUsers.plaintextWarning')"
      />
      <ElAlert
          v-if="plaintextDialog.message"
          class="mt-3"
          :type="plaintextDialog.messageType === 'error' ? 'error' : 'success'"
          :closable="false"
          show-icon
          :title="plaintextDialog.message"
        />
      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton
            :disabled="plaintextDialog.loading"
            @click="closePlaintextDialog"
          >
            {{ t('adminUsers.cancelDisableUser') }}
          </ElButton>
          <ElButton
            :data-testid="`confirm-copy-plaintext-${plaintextDialog.user.id}`"
            type="warning"
            :disabled="plaintextDialog.loading"
            @click="confirmCopyPlaintext"
          >
            {{ plaintextDialog.loading ? t('adminUsers.working') : t('adminUsers.confirmRevealAndCopy') }}
          </ElButton>
        </div>
      </template>
    </ElDialog>

    <ElDialog
      v-if="disableAccessDialog.user"
      :model-value="disableAccessDialog.open"
      append-to-body
      :show-close="false"
      align-center
      width="min(100%, 32rem)"
      :close-on-click-modal="!disableAccessDialog.loading"
      :close-on-press-escape="!disableAccessDialog.loading"
      @open="focusDisableAccessConfirmation"
      @opened="focusDisableAccessConfirmation"
      @update:model-value="(value) => { if (!value) closeDisableAccessDialog() }"
    >
      <template #header>
        <div
          data-testid="disable-access-dialog"
          class="flex items-start justify-between gap-4"
          @keydown.esc.stop.prevent="closeDisableAccessDialog"
        >
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-gray-900">{{ t('adminUsers.disableUserConfirmTitle') }}</h2>
            <p class="mt-1 truncate text-sm text-gray-500">{{ disableAccessDialog.user.email }}</p>
          </div>
          <ElButton
            data-testid="disable-access-dialog-close"
            class="shrink-0"
            :disabled="disableAccessDialog.loading"
            @click="closeDisableAccessDialog"
          >
            {{ t('adminUsers.closeDialog') }}
          </ElButton>
        </div>
      </template>
      <ElAlert
        type="error"
        :closable="false"
        show-icon
        :title="t('adminUsers.disableUserWarning')"
      />
        <label class="mt-4 block text-xs font-medium uppercase tracking-wide text-gray-500">
          {{ t('adminUsers.disableUserConfirmHint', { email: disableAccessDialog.user.email }) }}
          <ElInput
            ref="disableAccessConfirmInput"
            v-model="disableAccessDialog.confirmEmail"
            :data-testid="`disable-access-confirm-email-${disableAccessDialog.user.id}`"
            class="mt-1 block w-full"
            autofocus
            :aria-label="t('adminUsers.disableUserConfirmHint', { email: disableAccessDialog.user.email })"
            :placeholder="disableAccessDialog.user.email"
            :disabled="disableAccessDialog.loading || disableAccessCompleted"
          />
        </label>
        <ElAlert
          v-if="disableAccessDialog.message"
          class="mt-3"
          :type="disableAccessDialog.messageType === 'error' ? 'error' : 'success'"
          :closable="false"
          show-icon
          :title="disableAccessDialog.message"
        />
      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton
            data-testid="disable-access-dialog-cancel"
            :disabled="disableAccessDialog.loading"
            @click="closeDisableAccessDialog"
          >
            {{ disableAccessCompleted ? t('adminUsers.closeDialog') : t('adminUsers.cancelDisableUser') }}
          </ElButton>
          <ElButton
            :data-testid="`confirm-disable-access-${disableAccessDialog.user.id}`"
            type="danger"
            :disabled="!disableAccessConfirmMatches || disableAccessDialog.loading || disableAccessCompleted"
            @click="confirmDisableAccess"
          >
            {{ disableAccessDialog.loading ? t('adminUsers.working') : t('adminUsers.confirmDisableUser') }}
          </ElButton>
        </div>
      </template>
    </ElDialog>
  </AppLayout>
</template>
