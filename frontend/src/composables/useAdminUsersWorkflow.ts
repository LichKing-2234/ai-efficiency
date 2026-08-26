import { computed, reactive, ref, watch } from 'vue'
import type {
  AdminDepartmentChildrenParams,
  AdminDepartmentOptionsParams,
  AdminUsersListParams,
} from '@/api/adminUsers'
import { fullPageSize, positivePage } from '@/utils/pagination'
import type {
  AdminDepartmentChildrenResponse,
  AdminDepartmentOption,
  AdminDepartmentOptionsResponse,
  AdminDirectoryDepartmentSummary,
  AdminUser,
  AdminUsersListResponse,
} from '@/types'

export type AdminUsersViewMode = 'users' | 'departments'

export interface AdminUsersWorkflowRoute {
  query: Record<string, unknown>
  fullPath: string
}

export interface AdminUsersWorkflowRouter {
  push: (location: { query: Record<string, string> }) => unknown
  replace: (location: { query: Record<string, string> }) => unknown
}

export interface AdminUsersWorkflowOptions {
  route: AdminUsersWorkflowRoute
  router: AdminUsersWorkflowRouter
  loadUsers: (params: AdminUsersListParams) => Promise<AdminUsersListResponse>
  loadDepartmentChildren: (params: AdminDepartmentChildrenParams) => Promise<AdminDepartmentChildrenResponse>
  userLoadError: (error: unknown) => string
  departmentLoadError: (error: unknown) => string
  onTargetContextChange?: () => void
}

export interface LoadedAdminDepartmentPage {
  items: AdminDirectoryDepartmentSummary[]
  page: number
  page_size: number
  total: number
}

function queryString(route: AdminUsersWorkflowRoute, key: string) {
  const value = route.query[key]
  return typeof value === 'string' ? value : ''
}

export interface AdminDepartmentPickerWorkflowOptions {
  getModelValue: () => string
  loadOptions: (params: AdminDepartmentOptionsParams) => Promise<AdminDepartmentOptionsResponse>
  optionLoadError: (error: unknown) => string
}

export function useAdminDepartmentPickerWorkflow(options: AdminDepartmentPickerWorkflowOptions) {
  const open = ref(false)
  const loading = ref(false)
  const error = ref('')
  const searchQuery = ref('')
  const items = ref<AdminDepartmentOption[]>([])
  const selectedOption = ref<AdminDepartmentOption | null>(null)
  const page = ref(1)
  const pageSize = ref(20)
  const total = ref(0)
  const hasLoadedOptions = ref(false)
  const selectionCache = new Map<string, AdminDepartmentOption | null>()
  let optionsRequested = false
  let requestGeneration = 0
  let selectionRequestGeneration = 0
  let selectionPendingID = ''
  let searchTimer: number | undefined

  const showPagination = computed(() => items.value.length > 0 && total.value > pageSize.value)

  function clearSearchTimer() {
    if (searchTimer !== undefined) {
      window.clearTimeout(searchTimer)
      searchTimer = undefined
    }
  }

  function cacheOptions(nextItems: AdminDepartmentOption[]) {
    for (const item of nextItems) selectionCache.set(item.external_id, item)
  }

  function resetOptions(clearSearch = false) {
    if (clearSearch) searchQuery.value = ''
    items.value = []
    page.value = 1
    pageSize.value = 20
    total.value = 0
    hasLoadedOptions.value = false
    error.value = ''
    optionsRequested = false
  }

  async function loadOptions(
    targetPage: number,
    selectedID = '',
    query = searchQuery.value.trim(),
    preserveResults = false,
  ) {
    const generation = ++requestGeneration
    const selectionGeneration = selectedID ? ++selectionRequestGeneration : 0
    if (selectedID) selectionPendingID = selectedID
    optionsRequested = true
    loading.value = true
    error.value = ''
    try {
      const data = await options.loadOptions({
        ...(query ? { q: query } : {}),
        ...(selectedID ? { selected_id: selectedID } : {}),
        page: targetPage,
        page_size: 20,
      })
      if (selectedID && selectionGeneration === selectionRequestGeneration) {
        const resolved = data?.selected ?? null
        selectionCache.set(selectedID, resolved)
        if (options.getModelValue() === selectedID) selectedOption.value = resolved
      }
      if (generation !== requestGeneration) return
      const nextItems = data?.items ?? []
      items.value = nextItems
      cacheOptions(nextItems)
      page.value = data?.page ?? targetPage
      pageSize.value = data?.page_size ?? 20
      total.value = data?.total ?? 0
      hasLoadedOptions.value = true
    } catch (requestError) {
      if (generation !== requestGeneration) return
      if (!preserveResults) resetOptions()
      error.value = options.optionLoadError(requestError)
    } finally {
      if (selectedID && selectionGeneration === selectionRequestGeneration && selectionPendingID === selectedID) {
        selectionPendingID = ''
      }
      if (generation === requestGeneration) loading.value = false
    }
  }

  async function resolveSelection(value: string) {
    const selectedID = value.trim()
    if (!selectedID) {
      selectedOption.value = null
      return
    }
    if (selectionCache.has(selectedID)) {
      selectedOption.value = selectionCache.get(selectedID) ?? null
      return
    }
    if (selectionPendingID === selectedID) return
    await loadOptions(1, selectedID, '')
  }

  async function changeModelValue(value: string) {
    selectionRequestGeneration += 1
    selectionPendingID = ''
    if (!value) {
      selectedOption.value = null
      return
    }
    if (selectedOption.value?.external_id !== value) {
      selectedOption.value = null
      await resolveSelection(value)
    }
  }

  async function openPicker() {
    if (open.value) return
    open.value = true
    const selectedID = options.getModelValue().trim()
    const selectionUnresolved = selectedID
      && selectedOption.value?.external_id !== selectedID
      && !selectionCache.has(selectedID)
    if (selectionUnresolved && selectionPendingID !== selectedID) {
      await resolveSelection(selectedID)
    } else if (!hasLoadedOptions.value && !loading.value && !optionsRequested) {
      await loadOptions(1)
    }
  }

  function closePicker() {
    if (!open.value) return false
    open.value = false
    clearSearchTimer()
    requestGeneration += 1
    loading.value = false
    resetOptions(true)
    return true
  }

  function chooseOption(option: AdminDepartmentOption | null) {
    selectionRequestGeneration += 1
    selectionPendingID = ''
    selectedOption.value = option
    if (option) selectionCache.set(option.external_id, option)
    closePicker()
    return option?.external_id ?? ''
  }

  function setSearchQuery(value: string) {
    searchQuery.value = value
    clearSearchTimer()
    requestGeneration += 1
    searchTimer = window.setTimeout(() => {
      searchTimer = undefined
      void loadOptions(1)
    }, 300)
  }

  async function changePage(targetPage: number) {
    if (loading.value || targetPage === page.value) return
    await loadOptions(targetPage, '', searchQuery.value.trim(), true)
  }

  function dispose() {
    requestGeneration += 1
    selectionRequestGeneration += 1
    selectionPendingID = ''
    clearSearchTimer()
  }

  return {
    open,
    loading,
    error,
    searchQuery,
    items,
    selectedOption,
    page,
    pageSize,
    total,
    hasLoadedOptions,
    showPagination,
    loadOptions,
    resolveSelection,
    changeModelValue,
    openPicker,
    closePicker,
    chooseOption,
    setSearchQuery,
    changePage,
    dispose,
  }
}

export function useAdminUsersWorkflow(options: AdminUsersWorkflowOptions) {
  const rows = ref<AdminUser[]>([])
  const total = ref(0)
  const loading = ref(false)
  const error = ref('')
  const rootDepartments = ref<LoadedAdminDepartmentPage | null>(null)
  const childrenByParentID = ref<Map<string, LoadedAdminDepartmentPage>>(new Map())
  const departmentsLoading = ref(false)
  const departmentsError = ref('')
  const expandedDepartmentIds = ref<Set<string>>(new Set())
  const departmentChildrenLoadingIDs = ref<Set<string>>(new Set())
  const departmentChildrenErrors = ref<Map<string, string>>(new Map())
  const selectedUserIds = ref<Set<number>>(new Set())
  let userRequestGeneration = 0
  let rootDepartmentRequestGeneration = 0
  let childDepartmentRequestGeneration = 0
  let searchTimer: number | undefined

  const filters = reactive({
    view: queryString(options.route, 'view') === 'departments' ? 'departments' as AdminUsersViewMode : 'users' as AdminUsersViewMode,
    q: queryString(options.route, 'q'),
    department_id: queryString(options.route, 'department_id'),
    access_status: queryString(options.route, 'access_status'),
    page: positivePage(options.route.query.page),
    page_size: fullPageSize(options.route.query.page_size),
  })
  const selectedUserIdList = computed(() => Array.from(selectedUserIds.value))
  const selectedCount = computed(() => selectedUserIdList.value.length)
  const allVisibleSelected = computed(() => rows.value.length > 0 && rows.value.every((row) => selectedUserIds.value.has(row.id)))
  const visibleSelectionIndeterminate = computed(() => rows.value.some((row) => selectedUserIds.value.has(row.id)) && !allVisibleSelected.value)
  const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.page_size)))
  const pageStart = computed(() => total.value === 0 ? 0 : ((filters.page - 1) * filters.page_size) + 1)
  const pageEnd = computed(() => Math.min(total.value, filters.page * filters.page_size))
  const showUserPagination = computed(() => rows.value.length > 0 && total.value > filters.page_size)
  const activeViewLoading = computed(() => filters.view === 'departments' ? departmentsLoading.value : loading.value)
  const visibleDepartmentRows = computed(() => flattenLoadedDepartmentRows(
    rootDepartments.value?.items ?? [],
    childrenByParentID.value,
    expandedDepartmentIds.value,
  ))
  const showRootDepartmentPagination = computed(() => {
    const current = rootDepartments.value
    return current != null && (current.page * current.page_size < current.total || current.page > 1)
  })

  function currentQuery() {
    const query: Record<string, string> = {}
    if (filters.view === 'departments') query.view = 'departments'
    if (filters.q.trim()) query.q = filters.q.trim()
    if (filters.department_id.trim()) query.department_id = filters.department_id.trim()
    if (filters.access_status.trim()) query.access_status = filters.access_status.trim()
    if (filters.page > 1) query.page = String(filters.page)
    if (filters.page_size !== 20) query.page_size = String(filters.page_size)
    return query
  }

  function syncQuery(pushHistory = false) {
    if (pushHistory) void options.router.push({ query: currentQuery() })
    else void options.router.replace({ query: currentQuery() })
  }

  async function loadUsers(preserveResults = false, pushHistory = false) {
    const generation = ++userRequestGeneration
    loading.value = true
    error.value = ''
    try {
      const data = await options.loadUsers({
        q: filters.q,
        ...(filters.department_id ? { department_id: filters.department_id } : {}),
        ...(filters.access_status ? { access_status: filters.access_status } : {}),
        page: filters.page,
        page_size: filters.page_size,
      })
      if (generation !== userRequestGeneration) return undefined
      rows.value = data.items ?? []
      total.value = data.total ?? 0
      filters.page = data.page ?? filters.page
      filters.page_size = data.page_size ?? filters.page_size
      syncQuery(pushHistory)
      return true
    } catch (requestError) {
      if (generation !== userRequestGeneration) return undefined
      error.value = options.userLoadError(requestError)
      if (!preserveResults) {
        rows.value = []
        total.value = 0
      }
      return false
    } finally {
      if (generation === userRequestGeneration) loading.value = false
    }
  }

  function setSearchQuery(value: string) {
    if (filters.q === value) return
    filters.q = value
    userRequestGeneration += 1
    clearSearchTimer()
    searchTimer = window.setTimeout(() => {
      searchTimer = undefined
      void applySearch()
    }, 300)
  }

  function setDepartmentFilter(value: string) {
    if (filters.department_id === value) return
    filters.department_id = value
    filters.page = 1
    userRequestGeneration += 1
    options.onTargetContextChange?.()
  }

  function setAccessStatusFilter(value: string) {
    if (filters.access_status === value) return
    filters.access_status = value
    filters.page = 1
    userRequestGeneration += 1
    options.onTargetContextChange?.()
  }

  async function applySearch() {
    clearSearchTimer()
    filters.page = 1
    return loadUsers(false, true)
  }

  function clearSearchTimer() {
    if (searchTimer !== undefined) {
      window.clearTimeout(searchTimer)
      searchTimer = undefined
    }
  }

  async function changeDepartmentFilter(value: string) {
    setDepartmentFilter(value)
    return loadUsers(false, true)
  }

  async function changeAccessStatusFilter(value: string) {
    setAccessStatusFilter(value)
    return loadUsers(false, true)
  }

  function loadedDepartmentPage(data: AdminDepartmentChildrenResponse, requestedPage: number): LoadedAdminDepartmentPage {
    return {
      items: data.items ?? [],
      page: data.page ?? requestedPage,
      page_size: data.page_size ?? 25,
      total: data.total ?? 0,
    }
  }

  async function loadRootDepartments(page = 1, preserveResults = false) {
    if (departmentsLoading.value) return
    const generation = ++rootDepartmentRequestGeneration
    departmentsLoading.value = true
    departmentsError.value = ''
    try {
      const data = await options.loadDepartmentChildren({ page, page_size: 25 })
      if (generation !== rootDepartmentRequestGeneration) return
      rootDepartments.value = loadedDepartmentPage(data, page)
    } catch (requestError) {
      if (generation !== rootDepartmentRequestGeneration) return
      if (!preserveResults) rootDepartments.value = null
      departmentsError.value = options.departmentLoadError(requestError)
    } finally {
      if (generation === rootDepartmentRequestGeneration) departmentsLoading.value = false
    }
  }

  async function loadDepartmentChildren(parentDepartmentID: string, page = 1, append = false) {
    if (departmentChildrenLoadingIDs.value.has(parentDepartmentID)) return
    const generation = childDepartmentRequestGeneration
    departmentChildrenLoadingIDs.value = new Set(departmentChildrenLoadingIDs.value).add(parentDepartmentID)
    const nextErrors = new Map(departmentChildrenErrors.value)
    nextErrors.delete(parentDepartmentID)
    departmentChildrenErrors.value = nextErrors
    try {
      const data = await options.loadDepartmentChildren({
        parent_department_id: parentDepartmentID,
        page,
        page_size: 25,
      })
      if (generation !== childDepartmentRequestGeneration) return
      const loaded = loadedDepartmentPage(data, page)
      const current = childrenByParentID.value.get(parentDepartmentID)
      if (append && current) {
        const seen = new Set(current.items.map((item) => item.external_id))
        loaded.items = [...current.items, ...loaded.items.filter((item) => !seen.has(item.external_id))]
      }
      const nextChildren = new Map(childrenByParentID.value)
      nextChildren.set(parentDepartmentID, loaded)
      childrenByParentID.value = nextChildren
    } catch (requestError) {
      if (generation !== childDepartmentRequestGeneration) return
      const nextErrors = new Map(departmentChildrenErrors.value)
      nextErrors.set(parentDepartmentID, options.departmentLoadError(requestError))
      departmentChildrenErrors.value = nextErrors
    } finally {
      if (generation !== childDepartmentRequestGeneration) return
      const loadingIDs = new Set(departmentChildrenLoadingIDs.value)
      loadingIDs.delete(parentDepartmentID)
      departmentChildrenLoadingIDs.value = loadingIDs
    }
  }

  function flattenLoadedDepartmentRows(
    roots: AdminDirectoryDepartmentSummary[],
    children: Map<string, LoadedAdminDepartmentPage>,
    expandedIDs: Set<string>,
  ) {
    const visible: Array<{ department: AdminDirectoryDepartmentSummary; depth: number }> = []
    const visited = new Set<string>()
    const visit = (departments: AdminDirectoryDepartmentSummary[], depth: number) => {
      for (const department of departments) {
        if (visited.has(department.external_id)) continue
        visited.add(department.external_id)
        visible.push({ department, depth })
        if (expandedIDs.has(department.external_id)) {
          visit(children.get(department.external_id)?.items ?? [], depth + 1)
        }
      }
    }
    visit(roots, 0)
    return visible
  }

  async function toggleDepartment(department: AdminDirectoryDepartmentSummary) {
    if (!(department.has_children || department.child_count > 0)) return
    const next = new Set(expandedDepartmentIds.value)
    if (next.has(department.external_id)) {
      next.delete(department.external_id)
      expandedDepartmentIds.value = next
      return
    }
    next.add(department.external_id)
    expandedDepartmentIds.value = next
    if (!childrenByParentID.value.has(department.external_id)) {
      await loadDepartmentChildren(department.external_id, 1)
    }
  }

  function resetHierarchy() {
    rootDepartmentRequestGeneration += 1
    rootDepartments.value = null
    departmentsLoading.value = false
    departmentsError.value = ''
    invalidateDepartmentChildren()
  }

  function invalidateDepartmentChildren() {
    childDepartmentRequestGeneration += 1
    childrenByParentID.value = new Map()
    expandedDepartmentIds.value = new Set()
    departmentChildrenLoadingIDs.value = new Set()
    departmentChildrenErrors.value = new Map()
  }

  function departmentExpanded(department: AdminDirectoryDepartmentSummary) {
    return expandedDepartmentIds.value.has(department.external_id)
  }

  function departmentHasChildren(department: AdminDirectoryDepartmentSummary) {
    return department.has_children || department.child_count > 0
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

  async function setView(view: AdminUsersViewMode) {
    filters.view = view
    filters.page = 1
    syncQuery(true)
    if (view === 'departments' && rootDepartments.value === null && !departmentsLoading.value) {
      await loadRootDepartments(1)
    }
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
    userRequestGeneration += 1
    options.onTargetContextChange?.()
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

  function setUserSelected(userID: number, checked: boolean) {
    const next = new Set(selectedUserIds.value)
    if (checked) next.add(userID)
    else next.delete(userID)
    selectedUserIds.value = next
    options.onTargetContextChange?.()
  }

  function setAllVisibleSelected(checked: boolean) {
    const next = new Set(selectedUserIds.value)
    for (const row of rows.value) {
      if (checked) next.add(row.id)
      else next.delete(row.id)
    }
    selectedUserIds.value = next
    options.onTargetContextChange?.()
  }

  function buildBulkTarget(scope: 'selected' | 'current_filter' | 'all_mapped') {
    if (scope === 'selected') return { user_ids: selectedUserIdList.value }
    if (scope === 'current_filter') {
      return {
        filters: {
          q: filters.q.trim(),
          department_id: filters.department_id.trim(),
          access_status: filters.access_status.trim(),
        },
      }
    }
    return {}
  }

  function routeFilters() {
    return {
      view: queryString(options.route, 'view') === 'departments' ? 'departments' as AdminUsersViewMode : 'users' as AdminUsersViewMode,
      q: queryString(options.route, 'q'),
      department_id: queryString(options.route, 'department_id'),
      access_status: queryString(options.route, 'access_status'),
      page: positivePage(options.route.query.page),
      page_size: fullPageSize(options.route.query.page_size),
    }
  }

  async function restoreRoute() {
    const next = routeFilters()
    if (
      filters.view === next.view
      && filters.q === next.q
      && filters.department_id === next.department_id
      && filters.access_status === next.access_status
      && filters.page === next.page
      && filters.page_size === next.page_size
    ) return
    Object.assign(filters, next)
    userRequestGeneration += 1
    options.onTargetContextChange?.()
    clearSearchTimer()
    if (next.view === 'departments') {
      resetHierarchy()
      await loadRootDepartments(1)
    } else {
      await loadUsers()
    }
  }

  const stopRouteWatch = watch(
    () => options.route.fullPath,
    () => { void restoreRoute() },
  )

  async function start() {
    const requests: Promise<unknown>[] = [loadUsers()]
    if (filters.view === 'departments') requests.push(loadRootDepartments(1))
    await Promise.all(requests)
  }

  function dispose() {
    stopRouteWatch()
    clearSearchTimer()
    userRequestGeneration += 1
    rootDepartmentRequestGeneration += 1
    childDepartmentRequestGeneration += 1
  }

  return {
    rows,
    total,
    loading,
    error,
    filters,
    rootDepartments,
    departmentsLoading,
    departmentsError,
    selectedUserIds,
    selectedUserIdList,
    selectedCount,
    allVisibleSelected,
    visibleSelectionIndeterminate,
    totalPages,
    pageStart,
    pageEnd,
    showUserPagination,
    activeViewLoading,
    visibleDepartmentRows,
    showRootDepartmentPagination,
    setSearchQuery,
    applySearch,
    changeDepartmentFilter,
    changeAccessStatusFilter,
    toggleDepartment,
    departmentExpanded,
    departmentHasChildren,
    departmentChildrenLoading,
    departmentChildrenError,
    departmentChildrenEmpty,
    canLoadMoreDepartmentChildren,
    loadMoreDepartmentChildren,
    changeRootDepartmentPage,
    setView,
    refreshActiveView,
    openDepartmentUsers,
    changePageSize,
    changePage,
    setUserSelected,
    setAllVisibleSelected,
    buildBulkTarget,
    start,
    dispose,
  }
}
