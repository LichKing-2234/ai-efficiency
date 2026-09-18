import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, reactive } from 'vue'
import {
  useAdminDepartmentPickerWorkflow,
  useAdminUsersWorkflow,
} from '@/composables/useAdminUsersWorkflow'
import type {
  AdminDepartmentChildrenResponse,
  AdminDepartmentOption,
  AdminDepartmentOptionsResponse,
  AdminDirectoryDepartmentSummary,
  AdminUser,
  AdminUsersListResponse,
} from '@/types'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

function user(id: number, username: string): AdminUser {
  return {
    id,
    username,
    email: `${username}@example.com`,
    role: 'user',
    auth_source: 'ldap',
    relay_auth_password: '',
    created_at: '2026-08-26T00:00:00Z',
    updated_at: '2026-08-26T00:00:00Z',
  }
}

function userPage(items: AdminUser[], page = 1, pageSize = 20): AdminUsersListResponse {
  return { items, total: items.length, page, page_size: pageSize }
}

function department(id: string, parentID?: string): AdminDirectoryDepartmentSummary {
  return {
    external_id: id,
    parent_external_id: parentID,
    name: id,
    path: id,
    display_path: id,
    depth: parentID ? 1 : 0,
    child_count: parentID ? 0 : 1,
    has_children: !parentID,
    member_count: 1,
    matched_user_count: 1,
    subtree_member_count: 1,
    subtree_matched_user_count: 1,
    representative_count: 0,
    matched_representative_count: 0,
  }
}

function departmentPage(items: AdminDirectoryDepartmentSummary[], parentID = ''): AdminDepartmentChildrenResponse {
  return { items, parent_department_id: parentID, total: items.length, page: 1, page_size: 25 }
}

function departmentOption(id: string): AdminDepartmentOption {
  return { external_id: id, name: id, display_path: `Company / ${id}` }
}

function departmentOptionsPage(
  items: AdminDepartmentOption[],
  overrides: Partial<AdminDepartmentOptionsResponse> = {},
): AdminDepartmentOptionsResponse {
  return { items, selected: null, total: items.length, page: 1, page_size: 20, ...overrides }
}

describe('useAdminUsersWorkflow', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('rejects an older user response as soon as a newer search intent starts', async () => {
    const older = deferred<AdminUsersListResponse>()
    const newer = deferred<AdminUsersListResponse>()
    const loadUsers = vi.fn()
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise)
    const router = { push: vi.fn(), replace: vi.fn() }
    const route = reactive({ query: {}, fullPath: '/admin/users' })
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route,
      router,
      loadUsers,
      loadDepartmentChildren: vi.fn(),
      userLoadError: () => 'load failed',
      departmentLoadError: () => 'department load failed',
    }))!

    const firstRequest = workflow.start()
    workflow.setSearchQuery('new')
    const latestRequest = workflow.applySearch()
    older.resolve(userPage([user(1, 'older')]))
    await firstRequest
    expect(workflow.rows.value).toEqual([])
    expect(workflow.loading.value).toBe(true)

    newer.resolve(userPage([user(2, 'newer')]))
    await latestRequest

    expect(workflow.rows.value.map((row) => row.username)).toEqual(['newer'])
    expect(workflow.filters.q).toBe('new')
    expect(router.push).toHaveBeenCalledWith({ query: { q: 'new' } })
    expect(workflow.loading.value).toBe(false)
    scope.stop()
  })

  it('invalidates expanded branch responses on hierarchy reset and reloads the branch on demand', async () => {
    const staleChildren = deferred<AdminDepartmentChildrenResponse>()
    const freshChildren = deferred<AdminDepartmentChildrenResponse>()
    const root = department('department-root')
    const loadDepartmentChildren = vi.fn()
      .mockResolvedValueOnce(departmentPage([root]))
      .mockImplementationOnce(() => staleChildren.promise)
      .mockResolvedValueOnce(departmentPage([root]))
      .mockImplementationOnce(() => freshChildren.promise)
    const route = reactive({ query: { view: 'departments' }, fullPath: '/admin/users?view=departments' })
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route,
      router: { push: vi.fn(), replace: vi.fn() },
      loadUsers: vi.fn().mockResolvedValue(userPage([])),
      loadDepartmentChildren,
      userLoadError: () => 'load failed',
      departmentLoadError: () => 'department load failed',
    }))!

    await workflow.start()
    const firstExpansion = workflow.toggleDepartment(root)
    await workflow.refreshActiveView()
    staleChildren.resolve(departmentPage([department('department-stale', root.external_id)], root.external_id))
    await firstExpansion

    expect(workflow.departmentExpanded(root)).toBe(false)
    expect(workflow.visibleDepartmentRows.value.map(({ department }) => department.external_id)).toEqual([root.external_id])

    const latestExpansion = workflow.toggleDepartment(root)
    freshChildren.resolve(departmentPage([department('department-fresh', root.external_id)], root.external_id))
    await latestExpansion

    expect(workflow.visibleDepartmentRows.value.map(({ department }) => department.external_id)).toEqual([
      root.external_id,
      'department-fresh',
    ])
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(4)
    await workflow.toggleDepartment(root)
    await workflow.toggleDepartment(root)
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(4)
    expect(workflow.visibleDepartmentRows.value.map(({ department }) => department.external_id)).toEqual([
      root.external_id,
      'department-fresh',
    ])
    scope.stop()
  })

  it('owns persistent selection and prepares selected or current-filter bulk targets', async () => {
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route: reactive({ query: {}, fullPath: '/admin/users' }),
      router: { push: vi.fn(), replace: vi.fn() },
      loadUsers: vi.fn().mockResolvedValue(userPage([user(7, 'alice'), user(8, 'bob')])),
      loadDepartmentChildren: vi.fn(),
      userLoadError: () => 'load failed',
      departmentLoadError: () => 'department load failed',
    }))!

    await workflow.start()
    workflow.setUserSelected(7, true)
    workflow.setAllVisibleSelected(true)

    expect(workflow.selectedUserIdList.value).toEqual([7, 8])
    expect(workflow.buildBulkTarget('selected')).toEqual({ user_ids: [7, 8] })

    workflow.setSearchQuery('  alice  ')
    await workflow.changeDepartmentFilter(' department-alpha ')
    await workflow.changeAccessStatusFilter('configured')

    expect(workflow.buildBulkTarget('current_filter')).toEqual({
      filters: {
        q: 'alice',
        department_id: 'department-alpha',
        access_status: 'configured',
      },
    })
    expect(workflow.selectedUserIdList.value).toEqual([7, 8])
    workflow.dispose()
    scope.stop()
  })

  it('restores browser history through the workflow and resets branches only when department view becomes active', async () => {
    const route = reactive({
      query: { view: 'departments' } as Record<string, unknown>,
      fullPath: '/admin/users?view=departments',
    })
    const root = department('department-root')
    const child = department('department-child', root.external_id)
    const loadUsers = vi.fn().mockResolvedValue(userPage([user(7, 'alice')]))
    const loadDepartmentChildren = vi.fn()
      .mockResolvedValueOnce(departmentPage([root]))
      .mockResolvedValueOnce(departmentPage([child], root.external_id))
      .mockResolvedValueOnce(departmentPage([root]))
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route,
      router: { push: vi.fn(), replace: vi.fn() },
      loadUsers,
      loadDepartmentChildren,
      userLoadError: () => 'load failed',
      departmentLoadError: () => 'department load failed',
    }))!

    await workflow.start()
    workflow.setUserSelected(7, true)
    await workflow.toggleDepartment(root)
    expect(workflow.departmentExpanded(root)).toBe(true)

    route.query = { q: 'alice' }
    route.fullPath = '/admin/users?q=alice'
    await nextTick()
    await Promise.resolve()
    expect(workflow.departmentExpanded(root)).toBe(true)

    route.query = { view: 'departments' }
    route.fullPath = '/admin/users?view=departments'
    await nextTick()
    await Promise.resolve()
    await Promise.resolve()

    expect(workflow.departmentExpanded(root)).toBe(false)
    expect(workflow.rootDepartments.value?.items[0].external_id).toBe(root.external_id)
    expect(workflow.selectedUserIdList.value).toEqual([7])
    expect(loadUsers).toHaveBeenCalledTimes(2)
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(3)
    workflow.dispose()
    scope.stop()
  })

  it('rolls back indexed pages and appends child continuation without replacing an existing summary', async () => {
    const root = department('department-root')
    const firstChild = department('department-child', root.external_id)
    const duplicateChild = { ...firstChild, member_count: 99 }
    const nextChild = department('department-next', root.external_id)
    const loadUsers = vi.fn()
      .mockResolvedValueOnce({ items: [user(7, 'stable')], total: 40, page: 1, page_size: 20 })
      .mockRejectedValueOnce(new Error('user page failed'))
    const loadDepartmentChildren = vi.fn().mockImplementation((params: {
      parent_department_id?: string
      page: number
      page_size: number
    }) => {
      if (!params.parent_department_id && params.page === 2) throw new Error('root page failed')
      if (!params.parent_department_id) {
        return { items: [root], parent_department_id: '', total: 26, page: 1, page_size: 25 }
      }
      if (params.page === 2) {
        return { items: [duplicateChild, nextChild], parent_department_id: root.external_id, total: 26, page: 2, page_size: 25 }
      }
      return { items: [firstChild], parent_department_id: root.external_id, total: 26, page: 1, page_size: 25 }
    })
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route: reactive({ query: { view: 'departments' }, fullPath: '/admin/users?view=departments' }),
      router: { push: vi.fn(), replace: vi.fn() },
      loadUsers,
      loadDepartmentChildren,
      userLoadError: (error) => (error as Error).message,
      departmentLoadError: (error) => (error as Error).message,
    }))!

    await workflow.start()
    await workflow.changePage(2)
    expect(workflow.filters.page).toBe(1)
    expect(workflow.rows.value.map((row) => row.username)).toEqual(['stable'])

    await workflow.changeRootDepartmentPage(2)
    expect(workflow.rootDepartments.value?.page).toBe(1)
    expect(workflow.rootDepartments.value?.items).toEqual([root])

    await workflow.toggleDepartment(root)
    await workflow.loadMoreDepartmentChildren(root.external_id)
    const visibleChildren = workflow.visibleDepartmentRows.value.slice(1).map(({ department }) => department)
    expect(visibleChildren.map((item) => item.external_id)).toEqual([firstChild.external_id, nextChild.external_id])
    expect(visibleChildren[0].member_count).toBe(firstChild.member_count)
    scope.stop()
  })

  it('deduplicates a pending root load, retries failure, and refreshes only the active view', async () => {
    const pendingRoot = deferred<AdminDepartmentChildrenResponse>()
    const root = department('department-root')
    const loadUsers = vi.fn().mockResolvedValue(userPage([]))
    const loadDepartmentChildren = vi.fn()
      .mockImplementationOnce(() => pendingRoot.promise)
      .mockResolvedValue(departmentPage([root]))
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route: reactive({ query: {}, fullPath: '/admin/users' }),
      router: { push: vi.fn(), replace: vi.fn() },
      loadUsers,
      loadDepartmentChildren,
      userLoadError: (error) => (error as Error).message,
      departmentLoadError: (error) => (error as Error).message,
    }))!

    await workflow.start()
    const firstActivation = workflow.setView('departments')
    const duplicateActivation = workflow.setView('departments')
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(1)
    pendingRoot.reject(new Error('root request failed'))
    await Promise.all([firstActivation, duplicateActivation])
    expect(workflow.departmentsError.value).toBe('root request failed')

    await workflow.setView('users')
    await workflow.setView('departments')
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(2)
    expect(workflow.rootDepartments.value?.items).toEqual([root])

    loadUsers.mockClear()
    loadDepartmentChildren.mockClear()
    await workflow.refreshActiveView()
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(1)
    expect(loadUsers).not.toHaveBeenCalled()

    await workflow.setView('users')
    await workflow.refreshActiveView()
    expect(loadUsers).toHaveBeenCalledTimes(1)
    expect(loadDepartmentChildren).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it('normalizes URL defaults and persists explicit search and pagination intent', async () => {
    const route = reactive({
      query: { page: '2', page_size: '10' } as Record<string, unknown>,
      fullPath: '/admin/users?page=2&page_size=10',
    })
    const router = { push: vi.fn(), replace: vi.fn() }
    const loadUsers = vi.fn().mockImplementation((params) => Promise.resolve({
      items: [],
      total: 100,
      page: params.page,
      page_size: params.page_size,
    }))
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route,
      router,
      loadUsers,
      loadDepartmentChildren: vi.fn(),
      userLoadError: () => 'load failed',
      departmentLoadError: () => 'department load failed',
    }))!

    await workflow.start()
    expect(loadUsers).toHaveBeenLastCalledWith({ q: '', page: 2, page_size: 20 })
    expect(router.replace).toHaveBeenLastCalledWith({ query: { page: '2' } })

    workflow.setSearchQuery('alice')
    await workflow.applySearch()
    expect(router.push).toHaveBeenLastCalledWith({ query: { q: 'alice' } })

    await workflow.changePageSize(50)
    expect(router.push).toHaveBeenLastCalledWith({ query: { q: 'alice', page_size: '50' } })

    await workflow.changePage(2)
    expect(loadUsers).toHaveBeenLastCalledWith({ q: 'alice', page: 2, page_size: 50 })
    expect(router.push).toHaveBeenLastCalledWith({ query: { q: 'alice', page: '2', page_size: '50' } })
    workflow.dispose()
    scope.stop()
  })

  it('debounces user search and disposes pending responses without route writes', async () => {
    vi.useFakeTimers()
    const pending = deferred<AdminUsersListResponse>()
    const loadUsers = vi.fn()
      .mockResolvedValueOnce(userPage([]))
      .mockImplementationOnce(() => pending.promise)
    const router = { push: vi.fn(), replace: vi.fn() }
    const scope = effectScope()
    const workflow = scope.run(() => useAdminUsersWorkflow({
      route: reactive({ query: {}, fullPath: '/admin/users' }),
      router,
      loadUsers,
      loadDepartmentChildren: vi.fn(),
      userLoadError: () => 'load failed',
      departmentLoadError: () => 'department load failed',
    }))!

    await workflow.start()
    router.replace.mockClear()
    workflow.setSearchQuery('ali')
    workflow.setSearchQuery('alice')
    await vi.advanceTimersByTimeAsync(299)
    expect(loadUsers).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(loadUsers).toHaveBeenCalledTimes(2)
    expect(loadUsers).toHaveBeenLastCalledWith({ q: 'alice', page: 1, page_size: 20 })

    workflow.dispose()
    pending.resolve(userPage([user(7, 'late')], 2, 50))
    await Promise.resolve()
    expect(workflow.rows.value).toEqual([])
    expect(router.push).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
    scope.stop()
  })
})

describe('useAdminDepartmentPickerWorkflow', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('resets picker paging on close and rejects the response from the closed lifecycle', async () => {
    const firstPage = departmentOption('department-alpha')
    const staleNextPage = deferred<AdminDepartmentOptionsResponse>()
    const reopenedPage = departmentOption('department-reopened')
    const loadOptions = vi.fn()
      .mockResolvedValueOnce(departmentOptionsPage([firstPage], { total: 21 }))
      .mockImplementationOnce(() => staleNextPage.promise)
      .mockResolvedValueOnce(departmentOptionsPage([reopenedPage]))
    const workflow = useAdminDepartmentPickerWorkflow({
      getModelValue: () => '',
      loadOptions,
      optionLoadError: () => 'load failed',
    })

    await workflow.openPicker()
    const nextPageRequest = workflow.changePage(2)
    workflow.closePicker()

    expect(workflow.open.value).toBe(false)
    expect(workflow.items.value).toEqual([])
    expect(workflow.page.value).toBe(1)

    staleNextPage.resolve(departmentOptionsPage([departmentOption('department-stale')], { page: 2, total: 21 }))
    await nextPageRequest
    expect(workflow.items.value).toEqual([])

    await workflow.openPicker()
    expect(workflow.items.value).toEqual([reopenedPage])
    expect(loadOptions).toHaveBeenLastCalledWith({ page: 1, page_size: 20 })
    workflow.dispose()
  })

  it('debounces picker search and keeps only the latest response', async () => {
    vi.useFakeTimers()
    const older = deferred<AdminDepartmentOptionsResponse>()
    const newer = deferred<AdminDepartmentOptionsResponse>()
    const loadOptions = vi.fn()
      .mockResolvedValueOnce(departmentOptionsPage([]))
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise)
    const workflow = useAdminDepartmentPickerWorkflow({
      getModelValue: () => '',
      loadOptions,
      optionLoadError: () => 'load failed',
    })

    await workflow.openPicker()
    workflow.setSearchQuery('  old  ')
    await vi.advanceTimersByTimeAsync(300)
    workflow.setSearchQuery('  new  ')
    await vi.advanceTimersByTimeAsync(300)

    newer.resolve(departmentOptionsPage([departmentOption('department-new')]))
    await Promise.resolve()
    older.resolve(departmentOptionsPage([departmentOption('department-old')]))
    await Promise.resolve()

    expect(workflow.items.value).toEqual([departmentOption('department-new')])
    expect(loadOptions).toHaveBeenNthCalledWith(2, { q: 'old', page: 1, page_size: 20 })
    expect(loadOptions).toHaveBeenNthCalledWith(3, { q: 'new', page: 1, page_size: 20 })
    workflow.dispose()
  })

  it('keeps a failed picker search editable until the picker closes', async () => {
    vi.useFakeTimers()
    const workflow = useAdminDepartmentPickerWorkflow({
      getModelValue: () => '',
      loadOptions: vi.fn()
        .mockResolvedValueOnce(departmentOptionsPage([departmentOption('department-alpha')]))
        .mockRejectedValueOnce(new Error('search failed')),
      optionLoadError: (error) => (error as Error).message,
    })

    await workflow.openPicker()
    workflow.setSearchQuery('  recover  ')
    await vi.advanceTimersByTimeAsync(300)
    await Promise.resolve()

    expect(workflow.searchQuery.value).toBe('  recover  ')
    expect(workflow.items.value).toEqual([])
    expect(workflow.error.value).toBe('search failed')

    workflow.closePicker()
    expect(workflow.searchQuery.value).toBe('')
    workflow.dispose()
  })

  it('resolves only the latest controlled picker selection', async () => {
    let modelValue = 'department-alpha'
    const alphaRequest = deferred<AdminDepartmentOptionsResponse>()
    const betaRequest = deferred<AdminDepartmentOptionsResponse>()
    const loadOptions = vi.fn()
      .mockImplementationOnce(() => alphaRequest.promise)
      .mockImplementationOnce(() => betaRequest.promise)
    const workflow = useAdminDepartmentPickerWorkflow({
      getModelValue: () => modelValue,
      loadOptions,
      optionLoadError: () => 'load failed',
    })

    const resolvingAlpha = workflow.resolveSelection(modelValue)
    modelValue = 'department-beta'
    const resolvingBeta = workflow.changeModelValue(modelValue)
    alphaRequest.resolve(departmentOptionsPage([], { selected: departmentOption('department-alpha') }))
    await resolvingAlpha
    expect(workflow.selectedOption.value).toBeNull()

    betaRequest.resolve(departmentOptionsPage([], { selected: departmentOption('department-beta') }))
    await resolvingBeta
    expect(workflow.selectedOption.value).toEqual(departmentOption('department-beta'))
    workflow.dispose()
  })
})
