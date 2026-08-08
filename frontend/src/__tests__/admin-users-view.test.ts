import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import AdminUsersView from '@/views/admin/AdminUsersView.vue'
import { setLocale } from '@/i18n'

const messageSuccess = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as any)
const messageError = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as any)

vi.mock('@/api/adminUsers', () => ({
  assignAdminUserSubscription: vi.fn(),
  disableAdminUserAccess: vi.fn(),
  getAdminUserSubscriptionJob: vi.fn(),
  getLatestAdminUserSubscriptionJob: vi.fn(),
  listAdminUserDepartmentChildren: vi.fn(),
  listAdminUserDepartmentOptions: vi.fn(),
  listAdminUserDepartments: vi.fn(),
  listAdminUsers: vi.fn(),
  listAdminUserSubscriptionOptions: vi.fn(),
  manageAdminUserSubscriptions: vi.fn(),
  revealAdminUserRelayPassword: vi.fn(),
  startAdminUserSubscriptionJob: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

type MatchMediaController = ReturnType<typeof installMatchMedia>

function installMatchMedia(initialMatches: boolean) {
  const listeners = new Set<(event: { matches: boolean; media: string }) => void>()
  const addEventListener = vi.fn((type: string, listener: (event: { matches: boolean; media: string }) => void) => {
    if (type === 'change') listeners.add(listener)
  })
  const removeEventListener = vi.fn((type: string, listener: (event: { matches: boolean; media: string }) => void) => {
    if (type === 'change') listeners.delete(listener)
  })
  const mediaQuery = {
    matches: initialMatches,
    media: '(min-width: 768px)',
    onchange: null,
    addEventListener,
    removeEventListener,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(() => true),
  }
  const matchMedia = vi.fn((query: string) => {
    expect(query).toBe('(min-width: 768px)')
    return mediaQuery
  })
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: matchMedia,
  })

  return {
    mediaQuery,
    matchMedia,
    addEventListener,
    removeEventListener,
    change(matches: boolean) {
      mediaQuery.matches = matches
      const event = { matches, media: mediaQuery.media }
      for (const listener of Array.from(listeners)) listener(event)
    },
  }
}

let matchMediaController: MatchMediaController
const mountedWrappers = new Set<VueWrapper>()

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

async function selectElementOption(wrapper: VueWrapper, selectTestID: string, optionTestID: string) {
  await wrapper.get(`[data-testid="${selectTestID}"]`).trigger('click')
  await flushPromises()
  await wrapper.get(`[data-testid="${optionTestID}"]`).trigger('click')
  await flushPromises()
}

async function setElementCheckbox(wrapper: VueWrapper, checkboxTestID: string, checked = true) {
  await wrapper.get(`[data-testid="${checkboxTestID}"]`).get('input').setValue(checked)
  await flushPromises()
}

async function selectElementRadio(wrapper: VueWrapper, radioTestID: string) {
  const control = wrapper.get(`[data-testid="${radioTestID}"]`)
  const input = control.element instanceof HTMLInputElement
    ? control
    : control.get('input[type="radio"]')
  await input.setValue()
  await flushPromises()
}

function userRow(id: number, username: string) {
  return {
    id,
    username,
    email: `${username}@example.com`,
    role: 'user',
    auth_source: 'ldap',
    relay_user_id: id + 1000,
    relay_auth_password: '',
    created_at: '2026-05-26T00:00:00Z',
    updated_at: '2026-05-26T01:00:00Z',
  }
}

const rootDepartments = [
  {
    external_id: 'dept-alpha',
    name: 'Department Alpha',
    path: '1.781448',
    display_path: 'Department Alpha',
    depth: 0,
    child_count: 1,
    has_children: true,
    member_count: 1,
    matched_user_count: 1,
    subtree_member_count: 2,
    subtree_matched_user_count: 2,
    representative_count: 0,
    matched_representative_count: 0,
  },
  {
    external_id: 'dept-beta',
    name: 'Department Beta',
    path: '1.1178135',
    display_path: 'Department Beta',
    depth: 0,
    child_count: 0,
    has_children: false,
    member_count: 2,
    matched_user_count: 1,
    subtree_member_count: 2,
    subtree_matched_user_count: 1,
    representative_count: 0,
    matched_representative_count: 0,
  },
  {
    external_id: 'dept-gamma',
    parent_external_id: 'dept-missing',
    name: 'Department Gamma',
    path: '1.999999',
    display_path: 'Department Gamma',
    depth: 0,
    child_count: 0,
    has_children: false,
    member_count: 1,
    matched_user_count: 1,
    subtree_member_count: 1,
    subtree_matched_user_count: 1,
    representative_count: 0,
    matched_representative_count: 0,
  },
  {
    external_id: 'dept-cycle-a',
    parent_external_id: 'dept-cycle-c',
    name: 'Cycle A',
    path: 'cycle.a',
    display_path: 'Cycle A',
    depth: 0,
    child_count: 1,
    has_children: true,
    member_count: 1,
    matched_user_count: 1,
    subtree_member_count: 3,
    subtree_matched_user_count: 3,
    representative_count: 0,
    matched_representative_count: 0,
  },
]

const childDepartments: Record<string, any[]> = {
  'dept-alpha': [
    {
      external_id: 'dept-alpha-team-one',
      parent_external_id: 'dept-alpha',
      name: 'Team One',
      path: '1.781448.1683962',
      display_path: 'Department Alpha / Team One',
      depth: 1,
      child_count: 0,
      has_children: false,
      member_count: 1,
      matched_user_count: 1,
      subtree_member_count: 1,
      subtree_matched_user_count: 1,
      representative_count: 2,
      matched_representative_count: 1,
    },
  ],
  'dept-cycle-a': [
    {
      external_id: 'dept-cycle-b',
      parent_external_id: 'dept-cycle-a',
      name: 'Cycle B',
      path: 'cycle.b',
      display_path: 'Cycle A / Cycle B',
      depth: 1,
      child_count: 1,
      has_children: true,
      member_count: 1,
      matched_user_count: 1,
      subtree_member_count: 2,
      subtree_matched_user_count: 2,
      representative_count: 0,
      matched_representative_count: 0,
    },
  ],
  'dept-cycle-b': [
    {
      external_id: 'dept-cycle-c',
      parent_external_id: 'dept-cycle-b',
      name: 'Cycle C',
      path: 'cycle.c',
      display_path: 'Cycle A / Cycle B / Cycle C',
      depth: 2,
      child_count: 0,
      has_children: false,
      member_count: 1,
      matched_user_count: 1,
      subtree_member_count: 1,
      subtree_matched_user_count: 1,
      representative_count: 0,
      matched_representative_count: 0,
    },
  ],
  'dept-cycle-c': [],
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/admin/users', component: AdminUsersView },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

async function mountAdminUsersView(
  path = '/admin/users',
  userListFactory?: (params: any) => {
    items: any[]
    total?: number
    page?: number
    page_size?: number
  } | Promise<{
    items: any[]
    total?: number
    page?: number
    page_size?: number
  }>,
  attachToDocument = false,
	departmentChildrenFactory?: (params: any) => {
	  items: any[]
	  total?: number
	  page?: number
	  page_size?: number
	  parent_department_id?: string
	},
) {
  const {
    getLatestAdminUserSubscriptionJob,
    listAdminUserDepartmentChildren,
    listAdminUserDepartmentOptions,
    listAdminUserDepartments,
    listAdminUsers,
    listAdminUserSubscriptionOptions,
  } = await import('@/api/adminUsers')
  ;(listAdminUsers as any).mockImplementation(async (params: any) => ({
    data: {
      data: userListFactory ? await userListFactory(params) : {
        items: [
          {
            id: 7,
            username: 'alice',
            email: 'alice@example.com',
            role: 'user',
            auth_source: 'ldap',
            relay_user_id: 42,
	            relay_auth_password: 'encrypted-relay-password-ciphertext',
		            department: {
		              external_id: 'dept-alpha',
		              name: 'Department Alpha',
		              path: '1.781448',
		              display_path: 'Department Alpha',
		            },
	            created_at: '2026-05-26T00:00:00Z',
            updated_at: '2026-05-26T01:00:00Z',
          },
          {
            id: 8,
            username: 'bob',
            email: 'bob@example.org',
            role: 'user',
            auth_source: 'ldap',
            relay_user_id: 99,
            relay_auth_password: '',
            created_at: '2026-05-26T00:00:00Z',
            updated_at: '2026-05-26T01:00:00Z',
          },
        ],
        total: 120,
        page: params?.page ?? 1,
        page_size: params?.page_size ?? 20,
      },
	    },
  }))
  ;(listAdminUserDepartments as any).mockResolvedValue({
    data: {
      data: {
        items: Array.from({ length: 120 }, (_, index) => ({
          external_id: `legacy-dept-${index + 1}`,
          name: `Legacy Department ${index + 1}`,
          member_count: 0,
          matched_user_count: 0,
        })),
      },
    },
  })
  ;(listAdminUserDepartmentOptions as any).mockImplementation((params: any) => {
    const items = rootDepartments.map(({ external_id, name, display_path }) => ({ external_id, name, display_path }))
    const selected = params?.selected_id
      ? items.find((item) => item.external_id === params.selected_id) ?? {
          external_id: params.selected_id,
          name: params.selected_id,
          display_path: params.selected_id,
        }
      : null
    return Promise.resolve({
      data: {
        data: {
          items,
          selected,
          total: items.length,
          page: params?.page ?? 1,
          page_size: params?.page_size ?? 20,
        },
      },
    })
  })
  ;(listAdminUserDepartmentChildren as any).mockImplementation((params: any) => {
    const fallbackItems = params?.parent_department_id
      ? (childDepartments[params.parent_department_id] ?? [])
      : rootDepartments
    const result = departmentChildrenFactory?.(params) ?? {
      items: fallbackItems,
      total: fallbackItems.length,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 25,
      parent_department_id: params?.parent_department_id ?? '',
    }
    return Promise.resolve({ data: { data: result } })
  })
  ;(listAdminUserSubscriptionOptions as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 3,
            name: 'sub2api',
            display_name: 'Sub2API',
            groups: [
              {
                group_id: '42',
                group_name: 'Group Alpha',
                platform: 'openai',
                subscription_type: 'subscription',
              },
            ],
          },
        ],
      },
    },
  })
  if (!(getLatestAdminUserSubscriptionJob as any).getMockImplementation()) {
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({ data: { data: null } })
  }

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'relay_sso' }

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = mount(AdminUsersView, {
    attachTo: attachToDocument ? document.body : undefined,
    global: {
      plugins: [pinia, router],
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
      },
    },
  })
  mountedWrappers.add(wrapper)
  await flushPromises()
  return {
    wrapper,
    router,
    getLatestAdminUserSubscriptionJob,
    listAdminUserDepartmentChildren,
    listAdminUserDepartmentOptions,
    listAdminUserDepartments,
    listAdminUsers,
    listAdminUserSubscriptionOptions,
  }
}

function subscriptionJob(overrides: Record<string, any> = {}) {
  return {
    id: 12,
    status: 'queued',
    phase: 'queued',
    scope: 'selected',
    operation: 'add',
    provider_id: 3,
    group_id: '42',
    total_count: 1,
    processed_count: 0,
    success_count: 0,
    skipped_count: 0,
    failed_count: 0,
    results: [],
    ...overrides,
  }
}

describe('AdminUsersView', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.resetAllMocks()
    messageSuccess.mockImplementation(() => undefined as any)
    messageError.mockImplementation(() => undefined as any)
    matchMediaController = installMatchMedia(true)
  })

  afterEach(() => {
    for (const wrapper of mountedWrappers) {
      if (wrapper.exists()) wrapper.unmount()
    }
    mountedWrappers.clear()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('loads only the default user dependencies and never the 120-row legacy or bounded department routes', async () => {
    const {
      wrapper,
      getLatestAdminUserSubscriptionJob,
      listAdminUserDepartmentChildren,
      listAdminUserDepartmentOptions,
      listAdminUserDepartments,
      listAdminUsers,
      listAdminUserSubscriptionOptions,
    } = await mountAdminUsersView()

    expect(listAdminUsers).toHaveBeenCalledWith({ q: '', page: 1, page_size: 20 })
    expect(listAdminUsers).toHaveBeenCalledTimes(1)
    expect(listAdminUserSubscriptionOptions).toHaveBeenCalledTimes(1)
    expect(getLatestAdminUserSubscriptionJob).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartments).not.toHaveBeenCalled()
    expect(listAdminUserDepartmentOptions).not.toHaveBeenCalled()
    expect(listAdminUserDepartmentChildren).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Users & Access')
    expect(wrapper.text()).toContain('Relay mapping')
	    expect(wrapper.text()).toContain('Access status')
	    expect(wrapper.text()).toContain('Department')
	    expect(wrapper.text()).toContain('Department Alpha')
	    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('ldap')
    expect(wrapper.text()).toContain('42')
	    expect(wrapper.text()).toContain('Configured')
    expect(wrapper.get('[data-admin-user-list="desktop"]').find('.el-table').exists()).toBe(true)
    expect(wrapper.find('.el-tag').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('encrypted-relay-password-ciphertext')
    expect(wrapper.text()).toContain('120 total')
    expect(wrapper.text()).toContain('Page 1 / 6')
	  })

  it('renders user-list failures with Element Plus feedback', async () => {
    const { wrapper } = await mountAdminUsersView('/admin/users', async () => {
      throw new Error('synthetic user list failure')
    })

    expect(wrapper.text()).toContain('synthetic user list failure')
    expect(wrapper.find('.el-alert--error').exists()).toBe(true)
    expect(wrapper.find('.el-empty').exists()).toBe(false)
  })

  it('programmatically associates the visible department label with the picker value', async () => {
    const { wrapper } = await mountAdminUsersView()
    const label = wrapper.get('[data-testid="admin-users-department-label"]')
    const trigger = wrapper.get('[data-testid="admin-department-picker-trigger"]')
    const labelledBy = (trigger.attributes('aria-labelledby') ?? '').split(' ')

    expect(label.attributes('id')).toBeTruthy()
    expect(labelledBy[0]).toBe(label.attributes('id'))
    expect(labelledBy).toHaveLength(2)
    expect(wrapper.get(`[id="${labelledBy[1]}"]`).text()).toContain('All departments')
  })

  it('filters and clears users through the bounded picker with one list reload per change', async () => {
    const { wrapper, router, listAdminUserDepartmentOptions, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await flushPromises()
    expect(listAdminUserDepartmentOptions).toHaveBeenCalledWith({ page: 1, page_size: 20 })

    await wrapper.get('[data-testid="admin-department-picker-option-dept-alpha"]').trigger('click')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
      q: '',
      department_id: 'dept-alpha',
      page: 1,
      page_size: 20,
    })
    expect(listAdminUsers).toHaveBeenCalledTimes(2)
    expect(router.currentRoute.value.query.department_id).toBe('dept-alpha')

    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
    await wrapper.get('[data-testid="admin-department-picker-all"]').trigger('click')
    await flushPromises()

    expect(listAdminUsers).toHaveBeenCalledTimes(3)
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 1, page_size: 20 })
    expect(router.currentRoute.value.query.department_id).toBeUndefined()
  })

  it('resolves a deep-linked department label with one selected_id request and no tree request', async () => {
    const { listAdminUserDepartmentChildren, listAdminUserDepartmentOptions } = await mountAdminUsersView(
      '/admin/users?department_id=dept-alpha',
    )

    expect(listAdminUserDepartmentOptions).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartmentOptions).toHaveBeenCalledWith({
      selected_id: 'dept-alpha',
      page: 1,
      page_size: 20,
    })
    expect(listAdminUserDepartmentChildren).not.toHaveBeenCalled()
  })

  it('keeps the newer user response when two list requests resolve out of order', async () => {
    const older = deferred<any>()
    const newer = deferred<any>()
    let requestCount = 0
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView(
      '/admin/users',
      () => requestCount++ === 0 ? older.promise : newer.promise,
    )
    const replace = vi.spyOn(router, 'replace')

    expect(wrapper.get('[data-testid="admin-users-access-status-filter"]').classes()).toContain('el-select')
    await selectElementOption(wrapper, 'admin-users-access-status-filter', 'admin-users-access-status-option-disabled')
    expect(listAdminUsers).toHaveBeenCalledTimes(2)

    newer.resolve({ items: [userRow(2, 'newer')], total: 1, page: 1, page_size: 20 })
    await flushPromises()
    expect(wrapper.text()).toContain('newer@example.com')
    expect(replace).toHaveBeenCalledTimes(1)

    older.resolve({ items: [userRow(1, 'older')], total: 999, page: 3, page_size: 50 })
    await flushPromises()

    expect(wrapper.text()).toContain('newer@example.com')
    expect(wrapper.text()).not.toContain('older@example.com')
    expect(wrapper.text()).toContain('1 total')
    expect(wrapper.text()).toContain('Page 1 / 1')
    expect(router.currentRoute.value.query.page).toBeUndefined()
    expect(router.currentRoute.value.query.page_size).toBeUndefined()
    expect(replace).toHaveBeenCalledTimes(1)
  })

  it('keeps user loading active when an older request finishes before the latest request', async () => {
    const older = deferred<any>()
    const newer = deferred<any>()
    let requestCount = 0
    const { wrapper } = await mountAdminUsersView(
      '/admin/users',
      () => requestCount++ === 0 ? older.promise : newer.promise,
    )

    await selectElementOption(wrapper, 'admin-users-access-status-filter', 'admin-users-access-status-option-disabled')
    older.resolve({ items: [userRow(1, 'older')], total: 1, page: 1, page_size: 20 })
    await flushPromises()

    const refresh = wrapper.get('[data-testid="admin-users-refresh"]')
    expect(refresh.text()).toContain('Loading...')
    expect((refresh.element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.text()).not.toContain('older@example.com')

    newer.resolve({ items: [userRow(2, 'newer')], total: 1, page: 1, page_size: 20 })
    await flushPromises()
    expect(refresh.text()).toContain('Refresh')
    expect(wrapper.text()).toContain('newer@example.com')
  })

  it('invalidates a pending user request as soon as a debounced query changes', async () => {
    vi.useFakeTimers()
    const older = deferred<any>()
    const newer = deferred<any>()
    let requestCount = 0
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView(
      '/admin/users',
      () => requestCount++ === 0 ? older.promise : newer.promise,
    )
    const replace = vi.spyOn(router, 'replace')

    await wrapper.get('[data-testid="admin-users-search"]').setValue('new query')
    expect(listAdminUsers).toHaveBeenCalledTimes(1)

    older.resolve({ items: [userRow(1, 'older')], total: 999, page: 4, page_size: 50 })
    await flushPromises()

    const refresh = wrapper.get('[data-testid="admin-users-refresh"]')
    expect(wrapper.text()).not.toContain('older@example.com')
    expect(refresh.text()).toContain('Loading...')
    expect((refresh.element as HTMLButtonElement).disabled).toBe(true)
    expect(replace).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(300)
    expect(listAdminUsers).toHaveBeenCalledTimes(2)
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: 'new query', page: 1, page_size: 20 })

    newer.resolve({ items: [userRow(2, 'newer')], total: 1, page: 1, page_size: 20 })
    await flushPromises()

    expect(wrapper.text()).toContain('newer@example.com')
    expect(refresh.text()).toContain('Refresh')
    expect((refresh.element as HTMLButtonElement).disabled).toBe(false)
    expect(replace).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.query.q).toBe('new query')
  })

  it('invalidates a pending user request on unmount before it can replace the route query', async () => {
    const pending = deferred<any>()
    const { wrapper, router } = await mountAdminUsersView('/admin/users', () => pending.promise)
    const replace = vi.spyOn(router, 'replace')

    wrapper.unmount()
    pending.resolve({ items: [userRow(1, 'late')], total: 1, page: 4, page_size: 50 })
    await flushPromises()

    expect(replace).not.toHaveBeenCalled()
  })

  it('filters users by access status and keeps the filter in the URL', async () => {
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView()

    await selectElementOption(wrapper, 'admin-users-access-status-filter', 'admin-users-access-status-option-disabled')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
      q: '',
      access_status: 'disabled',
      page: 1,
      page_size: 20,
    })
    expect(router.currentRoute.value.query.access_status).toBe('disabled')
  })

  it('renders disabled access status in Chinese', async () => {
    setLocale('zh-CN')

    const { wrapper } = await mountAdminUsersView('/admin/users', (params) => ({
      items: [
        {
          id: 9,
          username: 'carol',
          email: 'carol@example.net',
          role: 'user',
          auth_source: 'ldap',
          relay_user_id: 77,
          relay_auth_password: 'encrypted-disabled-password-ciphertext',
          access_status: 'disabled',
          token_valid_after: '2026-06-26T09:00:00Z',
          offboarding_status: 'succeeded',
          created_at: '2026-05-26T00:00:00Z',
          updated_at: '2026-06-26T09:00:00Z',
        },
      ],
      total: 1,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 20,
    }))

    expect(wrapper.text()).toContain('已禁用')
    expect(wrapper.text()).not.toContain('encrypted-disabled-password-ciphertext')
  })

  it('loads root page 1/25 only on department view entry and drills into the user filter', async () => {
    const {
      wrapper,
      router,
      listAdminUserDepartmentChildren,
      listAdminUserDepartments,
      listAdminUsers,
    } = await mountAdminUsersView()

    expect(listAdminUserDepartmentChildren).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="admin-users-view-departments"]').element.closest('.el-radio-button')).not.toBeNull()
    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()

    expect(listAdminUserDepartments).not.toHaveBeenCalled()
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledWith({ page: 1, page_size: 25 })
    expect(wrapper.text()).toContain('Departments')
    expect(wrapper.text()).toContain('Department Alpha')
    expect(wrapper.text()).toContain('1 member')
    expect(wrapper.text()).toContain('1 matched user')

    await wrapper.get('[data-testid="admin-users-department-open-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.query.view).toBeUndefined()
    expect(router.currentRoute.value.query.department_id).toBe('dept-alpha')
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
      q: '',
      department_id: 'dept-alpha',
      page: 1,
      page_size: 20,
    })
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)
  })

  it('keeps a failed root request retryable and deduplicates department-tab activation while pending', async () => {
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView()
    const failed = deferred<any>()
    ;(listAdminUserDepartmentChildren as any).mockReset()
    ;(listAdminUserDepartmentChildren as any).mockImplementation(() => failed.promise)

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await selectElementRadio(wrapper, 'admin-users-view-departments')
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)

    failed.reject(new Error('root request failed'))
    await flushPromises()
    expect(wrapper.text()).toContain('root request failed')

    ;(listAdminUserDepartmentChildren as any).mockReset()
    ;(listAdminUserDepartmentChildren as any).mockResolvedValue({
      data: {
        data: {
          items: rootDepartments,
          parent_department_id: '',
          total: rootDepartments.length,
          page: 1,
          page_size: 25,
        },
      },
    })
    await selectElementRadio(wrapper, 'admin-users-view-users')
    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()

    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledWith({ page: 1, page_size: 25 })
    expect(wrapper.text()).toContain('Department Alpha')
    expect(wrapper.text()).not.toContain('root request failed')
  })

  it('refreshes only the active users or root-departments collection', async () => {
    const { wrapper, listAdminUserDepartmentChildren, listAdminUsers } = await mountAdminUsersView()
    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    ;(listAdminUserDepartmentChildren as any).mockClear()
    ;(listAdminUsers as any).mockClear()

    await wrapper.get('[data-testid="admin-users-refresh"]').trigger('click')
    await flushPromises()
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledWith({ page: 1, page_size: 25 })
    expect(listAdminUsers).not.toHaveBeenCalled()

    await selectElementRadio(wrapper, 'admin-users-view-users')
    await wrapper.get('[data-testid="admin-users-refresh"]').trigger('click')
    await flushPromises()
    expect(listAdminUsers).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)
  })

  it('clears cached children on department refresh and reloads them only after re-expansion', async () => {
    const oldChild = {
      ...childDepartments['dept-alpha'][0],
      external_id: 'dept-alpha-old-team',
      name: 'Old Team',
      display_path: 'Department Alpha / Old Team',
    }
    const freshChild = {
      ...childDepartments['dept-alpha'][0],
      external_id: 'dept-alpha-fresh-team',
      name: 'Fresh Team',
      display_path: 'Department Alpha / Fresh Team',
    }
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView()
    let alphaRequests = 0
    ;(listAdminUserDepartmentChildren as any).mockReset()
    ;(listAdminUserDepartmentChildren as any).mockImplementation((params: any) => {
      const items = params.parent_department_id === 'dept-alpha'
        ? [alphaRequests++ === 0 ? oldChild : freshChild]
        : rootDepartments
      return Promise.resolve({
        data: {
          data: {
            items,
            parent_department_id: params.parent_department_id ?? '',
            total: items.length,
            page: params.page ?? 1,
            page_size: 25,
          },
        },
      })
    })

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-old-team"]').exists()).toBe(true)

    await wrapper.get('[data-testid="admin-users-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-old-team"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Expand department')
    expect((listAdminUserDepartmentChildren as any).mock.calls.filter(
      ([params]: any[]) => params.parent_department_id === 'dept-alpha',
    )).toHaveLength(1)

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()

    expect((listAdminUserDepartmentChildren as any).mock.calls.filter(
      ([params]: any[]) => params.parent_department_id === 'dept-alpha',
    )).toHaveLength(2)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-fresh-team"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-old-team"]').exists()).toBe(false)
  })

  it('ignores a child response started before refresh and requests that parent again', async () => {
    const staleChild = {
      ...childDepartments['dept-alpha'][0],
      external_id: 'dept-alpha-stale-team',
      name: 'Stale Team',
      display_path: 'Department Alpha / Stale Team',
    }
    const freshChild = {
      ...childDepartments['dept-alpha'][0],
      external_id: 'dept-alpha-new-team',
      name: 'New Team',
      display_path: 'Department Alpha / New Team',
    }
    const pendingChild = deferred<any>()
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView()

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    ;(listAdminUserDepartmentChildren as any).mockReset()
    let alphaRequests = 0
    ;(listAdminUserDepartmentChildren as any).mockImplementation((params: any) => {
      if (params.parent_department_id === 'dept-alpha') {
        alphaRequests += 1
        if (alphaRequests === 1) return pendingChild.promise
        return Promise.resolve({
          data: {
            data: {
              items: [freshChild],
              parent_department_id: 'dept-alpha',
              total: 1,
              page: 1,
              page_size: 25,
            },
          },
        })
      }
      return Promise.resolve({
        data: {
          data: {
            items: rootDepartments,
            parent_department_id: '',
            total: rootDepartments.length,
            page: params.page ?? 1,
            page_size: 25,
          },
        },
      })
    })

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    expect(alphaRequests).toBe(1)

    await wrapper.get('[data-testid="admin-users-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Expand department')
    expect(alphaRequests).toBe(1)

    pendingChild.resolve({
      data: {
        data: {
          items: [staleChild],
          parent_department_id: 'dept-alpha',
          total: 1,
          page: 1,
          page_size: 25,
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-stale-team"]').exists()).toBe(false)

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(alphaRequests).toBe(2)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-new-team"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-stale-team"]').exists()).toBe(false)
  })

  it('loads only one parent immediate page and renders hierarchy and subtree counts', async () => {
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView()

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(false)

    const alphaToggle = wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]')
    expect(alphaToggle.attributes('aria-label')).toBe('Expand department')
    await alphaToggle.trigger('click')
    await flushPromises()

    expect(listAdminUserDepartmentChildren).toHaveBeenLastCalledWith({
      parent_department_id: 'dept-alpha',
      page: 1,
      page_size: 25,
    })
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(2)
    const alpha = wrapper.get('[data-testid="admin-users-department-open-dept-alpha"]')
    const child = wrapper.get('[data-testid="admin-users-department-open-dept-alpha-team-one"]')
    const beta = wrapper.get('[data-testid="admin-users-department-open-dept-beta"]')
    expect(alpha.attributes('aria-level')).toBe('1')
    expect(child.attributes('aria-level')).toBe('2')
    expect(beta.attributes('aria-level')).toBe('1')
    expect(child.attributes('style')).toContain('padding-left: 1.25rem')
    expect(alpha.text()).toContain('2 total members')
    expect(alpha.text()).toContain('2 total matched')
    expect(child.text()).toContain('1 total member')
    expect(child.text()).toContain('1 / 2 representatives matched')
    expect(alphaToggle.attributes('aria-label')).toBe('Collapse department')
    expect(alphaToggle.classes()).toContain('h-7')
    expect(alphaToggle.classes()).toContain('w-7')
    expect(alphaToggle.classes()).toContain('is-circle')
  })

  it('caches collapsed child pages and hides raw source paths from labels', async () => {
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView()

    expect(wrapper.html()).toContain('Department Alpha')
    expect(wrapper.html()).not.toContain('1.781448')

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-gamma"]').exists()).toBe(true)

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)
    expect(wrapper.html()).toContain('Department Alpha / Team One')
    expect(wrapper.html()).toContain('Department Gamma')
    expect(wrapper.html()).not.toContain('1.781448.1683962')

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Expand department')

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)
    expect((listAdminUserDepartmentChildren as any).mock.calls.filter(
      ([params]: any[]) => params.parent_department_id === 'dept-alpha',
    )).toHaveLength(1)
  })

  it('keeps keyboard activation on the department toggle scoped to expansion', async () => {
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView()

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()

    const alphaToggle = wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]')
    expect(alphaToggle.attributes('aria-label')).toBe('Expand department')
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(false)

    await alphaToggle.trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(router.currentRoute.value.query.department_id).toBeUndefined()
    expect(listAdminUsers).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Collapse department')

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('keydown', { key: ' ' })
    await flushPromises()

    expect(router.currentRoute.value.query.department_id).toBeUndefined()
    expect(listAdminUsers).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Expand department')
  })

  it('loads root children on an initial department route and mounts no hidden user-row tree', async () => {
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView('/admin/users?view=departments')

    expect(listAdminUserDepartmentChildren).toHaveBeenCalledTimes(1)
    expect(listAdminUserDepartmentChildren).toHaveBeenCalledWith({ page: 1, page_size: 25 })
    expect(wrapper.findAll('[data-admin-user-row]')).toHaveLength(0)
    expect(wrapper.find('[data-admin-user-list="mobile"]').exists()).toBe(false)
    expect(wrapper.find('[data-admin-user-list="desktop"]').exists()).toBe(false)
  })

  it('uses server root paging and appends deduplicated child continuation pages', async () => {
    const extraChild = {
      ...childDepartments['dept-alpha'][0],
      external_id: 'dept-alpha-team-two',
      name: 'Team Two',
      display_path: 'Department Alpha / Team Two',
    }
    const { wrapper, listAdminUserDepartmentChildren } = await mountAdminUsersView(
      '/admin/users?view=departments',
      undefined,
      false,
      (params) => {
        if (!params.parent_department_id) {
          if (params.page === 2) {
            return { items: [rootDepartments[1]], total: 26, page: 2, page_size: 25, parent_department_id: '' }
          }
          return { items: [rootDepartments[0]], total: 26, page: 1, page_size: 25, parent_department_id: '' }
        }
        if (params.parent_department_id === 'dept-alpha' && params.page === 2) {
          return {
            items: [childDepartments['dept-alpha'][0], extraChild],
            total: 26,
            page: 2,
            page_size: 25,
            parent_department_id: 'dept-alpha',
          }
        }
        return {
          items: childDepartments['dept-alpha'],
          total: 26,
          page: 1,
          page_size: 25,
          parent_department_id: 'dept-alpha',
        }
      },
    )

    await wrapper.get('[data-testid="admin-users-department-roots-next"]').trigger('click')
    await flushPromises()
    expect(listAdminUserDepartmentChildren).toHaveBeenLastCalledWith({ page: 2, page_size: 25 })
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-beta"]').exists()).toBe(true)

    await wrapper.get('[data-testid="admin-users-department-roots-prev"]').trigger('click')
    await flushPromises()
    expect(listAdminUserDepartmentChildren).toHaveBeenLastCalledWith({ page: 1, page_size: 25 })

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-users-department-load-more-dept-alpha"]').trigger('click')
    await flushPromises()

    expect(listAdminUserDepartmentChildren).toHaveBeenLastCalledWith({
      parent_department_id: 'dept-alpha',
      page: 2,
      page_size: 25,
    })
    expect(wrapper.findAll('[data-testid="admin-users-department-open-dept-alpha-team-one"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-two"]').exists()).toBe(true)
  })

  it('renders stable empty state for an unknown parent child page', async () => {
    const unknownParent = {
      ...rootDepartments[0],
      external_id: 'dept-unknown-parent',
      name: 'Unknown Parent',
      display_path: 'Unknown Parent',
    }
    const { wrapper } = await mountAdminUsersView(
      '/admin/users?view=departments',
      undefined,
      false,
      (params) => params.parent_department_id
        ? {
            items: [],
            total: 0,
            page: 1,
            page_size: 25,
            parent_department_id: params.parent_department_id,
          }
        : { items: [unknownParent], total: 1, page: 1, page_size: 25, parent_department_id: '' },
    )

    await wrapper.get('[data-testid="admin-users-department-toggle-dept-unknown-parent"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="admin-users-department-children-empty-dept-unknown-parent"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('No departments in the latest directory snapshot.')
  })

  it('drills into cycle non-anchor B and renders only the mocked B/C user scope', async () => {
    const cycleUser = (id: number, suffix: string) => ({
      id,
      username: `cycle-${suffix}`,
      email: `cycle-${suffix}@example.com`,
      role: 'user',
      auth_source: 'ldap',
      relay_user_id: id + 1000,
      relay_auth_password: '',
      created_at: '2026-05-26T00:00:00Z',
      updated_at: '2026-05-26T01:00:00Z',
    })
    const { wrapper, listAdminUsers } = await mountAdminUsersView('/admin/users', (params) => ({
      items: params.department_id === 'dept-cycle-b'
        ? [cycleUser(102, 'b'), cycleUser(103, 'c')]
        : [cycleUser(101, 'a'), cycleUser(102, 'b'), cycleUser(103, 'c')],
      total: params.department_id === 'dept-cycle-b' ? 2 : 3,
      page: 1,
      page_size: 20,
    }))

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    await wrapper.get('[data-testid="admin-users-department-toggle-dept-cycle-a"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-users-department-toggle-dept-cycle-b"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-users-department-open-dept-cycle-b"]').trigger('click')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
      q: '',
      department_id: 'dept-cycle-b',
      page: 1,
      page_size: 20,
    })
    const rowText = wrapper.findAll('[data-admin-user-row]').map((row) => row.text()).join(' ')
    expect(rowText).toContain('cycle-b')
    expect(rowText).toContain('cycle-c')
    expect(rowText).not.toContain('cycle-a')
  })

  it('mounts one 100-row viewport tree, swaps without reload or duplicate selection, and removes the exact listener', async () => {
    const users = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1,
      username: `user-${index + 1}`,
      email: `user-${index + 1}@example.com`,
      role: 'user',
      auth_source: 'ldap',
      relay_user_id: index + 1001,
      relay_auth_password: '',
      created_at: '2026-05-26T00:00:00Z',
      updated_at: '2026-05-26T01:00:00Z',
    }))
    const { wrapper, listAdminUsers } = await mountAdminUsersView('/admin/users', () => ({
      items: users,
      total: 100,
      page: 1,
      page_size: 100,
    }))

    expect(matchMediaController.matchMedia).toHaveBeenCalledTimes(1)
    expect(matchMediaController.addEventListener).toHaveBeenCalledTimes(1)
    const listener = matchMediaController.addEventListener.mock.calls[0][1]
    expect(wrapper.find('[data-admin-user-list="desktop"]').exists()).toBe(true)
    expect(wrapper.find('[data-admin-user-list="mobile"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-admin-user-row]')).toHaveLength(100)

    await setElementCheckbox(wrapper, 'select-user-1')
    matchMediaController.change(false)
    await wrapper.vm.$nextTick()

    expect(listAdminUsers).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-admin-user-list="desktop"]').exists()).toBe(false)
    expect(wrapper.find('[data-admin-user-list="mobile"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-admin-user-row]')).toHaveLength(100)
    expect((wrapper.get('[data-testid="select-user-mobile-1"]').get('input').element as HTMLInputElement).checked).toBe(true)

    matchMediaController.change(true)
    await wrapper.vm.$nextTick()
    await flushPromises()
    const remountedDesktopSelectAll = wrapper.get('[data-testid="select-all-users"]')
    expect((remountedDesktopSelectAll.get('input').element as HTMLInputElement).indeterminate).toBe(true)
    expect(remountedDesktopSelectAll.attributes('aria-checked')).toBe('mixed')

    await selectElementRadio(wrapper, 'admin-users-view-departments')
    await flushPromises()
    await selectElementRadio(wrapper, 'admin-users-view-users')
    await wrapper.vm.$nextTick()
    const viewRoundTripSelectAll = wrapper.get('[data-testid="select-all-users"]')
    expect((viewRoundTripSelectAll.get('input').element as HTMLInputElement).indeterminate).toBe(true)
    expect(viewRoundTripSelectAll.attributes('aria-checked')).toBe('mixed')

    wrapper.unmount()
    expect(matchMediaController.removeEventListener).toHaveBeenCalledTimes(1)
    expect(matchMediaController.removeEventListener).toHaveBeenCalledWith('change', listener)
  })

	  it('renders the primary admin users workflow in Chinese', async () => {
    setLocale('zh-CN')

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('用户与接入')
    expect(wrapper.text()).toContain('管理本地用户、relay 身份映射和凭据风险操作')
    expect(wrapper.text()).toContain('搜索')
    expect(wrapper.text()).toContain('本地用户')
    expect(wrapper.text()).toContain('复制明文')
  })

  it('searches from page one when the search button is clicked', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    expect(wrapper.get('[data-testid="admin-users-search"]').classes()).toContain('el-input__inner')
    expect(wrapper.get('[data-testid="admin-users-search-button"]').classes()).toContain('el-button')
    await wrapper.get('[data-testid="admin-users-search"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="admin-users-search-button"]').trigger('click')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: 'alice@example.com', page: 1, page_size: 20 })
  })

  it('debounces user search and sends only the latest value', async () => {
    vi.useFakeTimers()
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-users-search"]').setValue('ali')
    await wrapper.get('[data-testid="admin-users-search"]').setValue('alice')
    await vi.advanceTimersByTimeAsync(299)
    expect(listAdminUsers).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(listAdminUsers).toHaveBeenCalledTimes(2)
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: 'alice', page: 1, page_size: 20 })
  })

  it('updates page size and next page params', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    expect(wrapper.get('[data-testid="admin-users-page-size"]').classes()).toContain('el-select')
    await selectElementOption(wrapper, 'admin-users-page-size', 'admin-users-page-size-option-50')
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 1, page_size: 50 })

    await wrapper.get('[data-testid="admin-users-next-page"]').trigger('click')
    await flushPromises()
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 2, page_size: 50 })
  })

  it('restores and persists search pagination state in the URL query', async () => {
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView('/admin/users?q=alice&page=2&page_size=50')

    expect((listAdminUsers as any).mock.calls[0][0]).toEqual({ q: 'alice', page: 2, page_size: 50 })

    await selectElementOption(wrapper, 'admin-users-page-size', 'admin-users-page-size-option-20')

    expect(router.currentRoute.value.query.q).toBe('alice')
    expect(router.currentRoute.value.query.page_size).toBeUndefined()
  })

  it('copies encrypted ciphertext without calling reveal', async () => {
    const { wrapper } = await mountAdminUsersView()
    const { revealAdminUserRelayPassword } = await import('@/api/adminUsers')

    await wrapper.get('[data-testid="copy-encrypted-7"]').trigger('click')
    await flushPromises()

    expect(revealAdminUserRelayPassword).not.toHaveBeenCalled()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('encrypted-relay-password-ciphertext')
    expect(messageSuccess).toHaveBeenCalledWith('Copied encrypted')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('requires explicit confirmation before copying plaintext from reveal', async () => {
    const { revealAdminUserRelayPassword } = await import('@/api/adminUsers')
    ;(revealAdminUserRelayPassword as any).mockResolvedValue({
      data: { data: { password: 'test-password' } },
    })

    const { wrapper } = await mountAdminUsersView('/admin/users', undefined, true)
    await wrapper.get('[data-testid="copy-plaintext-7"]').trigger('click')
    await flushPromises()

    expect(revealAdminUserRelayPassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Plaintext relay passwords are sensitive')

    await wrapper.get('[data-testid="confirm-copy-plaintext-7"]').trigger('click')
    await flushPromises()

    expect(revealAdminUserRelayPassword).toHaveBeenCalledWith(7)
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('test-password')
    expect(wrapper.text()).toContain('Copied plaintext')
    expect(wrapper.text()).not.toContain('test-password')
  })

  it('moves focus into admin dialogs and closes them with Escape', async () => {
    const { wrapper } = await mountAdminUsersView('/admin/users', undefined, true)
    const trigger = wrapper.get('[data-testid="disable-access-7"]')
    ;(trigger.element as HTMLButtonElement).focus()

    await trigger.trigger('click')
    await flushPromises()

    const input = wrapper.get('[data-testid="disable-access-confirm-email-7"]')
    expect(document.activeElement).toBe(input.element)

    await wrapper.get('[data-testid="disable-access-dialog"]').trigger('keydown', { key: 'Escape' })
    await flushPromises()

    expect(wrapper.find('[data-testid="disable-access-dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
  })

  it('traps focus inside admin dialogs', async () => {
    const { wrapper } = await mountAdminUsersView('/admin/users', undefined, true)

    await wrapper.get('[data-testid="disable-access-7"]').trigger('click')
    await flushPromises()

    const closeButton = wrapper.get('[data-testid="disable-access-dialog-close"]')
    const cancelButton = wrapper.get('[data-testid="disable-access-dialog-cancel"]')
    ;(closeButton.element as HTMLButtonElement).focus()

    await wrapper.get('[data-testid="disable-access-dialog"]').trigger('keydown', { key: 'Tab', shiftKey: true })
    await flushPromises()

    expect(document.activeElement).toBe(cancelButton.element)
  })

  it('requires email confirmation before disabling user access', async () => {
    const { disableAdminUserAccess } = await import('@/api/adminUsers')
    ;(disableAdminUserAccess as any).mockResolvedValue({
      data: {
        data: {
          status: 'disabled',
          relay_user_id: 42,
          relay_disabled_at: '2026-07-01T12:00:00Z',
        },
      },
    })

    const { wrapper } = await mountAdminUsersView()
    await wrapper.get('[data-testid="disable-access-7"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
    expect(disableAdminUserAccess).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('After disabling, this user will no longer be able to access AI services')

    await wrapper.get('[data-testid="disable-access-confirm-email-7"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="confirm-disable-access-7"]').trigger('click')
    await flushPromises()

    expect(disableAdminUserAccess).toHaveBeenCalledWith(7, { confirm_email: 'alice@example.com' })
    expect(wrapper.text()).toContain('Disabled alice@example.com')
  })

  it('does not offer the disable action for already disabled users', async () => {
    const { wrapper } = await mountAdminUsersView('/admin/users', (params) => ({
      items: [
        {
          id: 7,
          username: 'alice',
          email: 'alice@example.com',
          role: 'user',
          auth_source: 'ldap',
          relay_user_id: 42,
          relay_auth_password: 'encrypted-relay-password-ciphertext',
          access_status: 'disabled',
          token_valid_after: '2026-07-01T12:00:00Z',
          created_at: '2026-05-26T00:00:00Z',
          updated_at: '2026-05-26T01:00:00Z',
        },
      ],
      total: 1,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 20,
    }))

    expect(wrapper.find('[data-testid="disable-access-7"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Disabled')
  })

  it('adds a subscription for one selected local user through a polled job workflow', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, manageAdminUserSubscriptions, startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'running', phase: 'processing', total_count: 1, processed_count: 0 }) },
    })
    ;(getAdminUserSubscriptionJob as any).mockResolvedValue({
      data: {
        data: subscriptionJob({
          status: 'completed',
          phase: 'completed',
          total_count: 1,
          processed_count: 1,
          success_count: 1,
          results: [{ user_id: 7, username: 'alice', email: 'alice@example.com', status: 'success' }],
        }),
      },
    })

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('Subscription management')
    await setElementCheckbox(wrapper, 'select-user-7')
    expect(wrapper.get('[data-testid="subscription-provider"]').classes()).toContain('el-select')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    await wrapper.get('[data-testid="subscription-days"]').setValue('60')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

	    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
	      scope: 'selected',
	      user_ids: [7],
	      operation: 'add',
	      provider_id: 3,
	      group_id: '42',
	      validity_days: 60,
	    })
	    expect(manageAdminUserSubscriptions).not.toHaveBeenCalled()
	    expect(wrapper.text()).toContain('Processing: 0 / 1')

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).toHaveBeenCalledWith(12)
	    expect(wrapper.text()).toContain('Completed: 1 succeeded, 0 skipped, 0 failed')
	    expect(wrapper.text()).toContain('alice')
	  })

	  it('passes department filters to current-filter subscription jobs', async () => {
	    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
	    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
	      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', scope: 'current_filter', total_count: 1, processed_count: 1, success_count: 1 }) },
		    })

		    const { wrapper } = await mountAdminUsersView()
		    await wrapper.get('[data-testid="admin-department-picker-trigger"]').trigger('click')
		    await flushPromises()
		    await wrapper.get('[data-testid="admin-department-picker-option-dept-alpha"]').trigger('click')
		    await flushPromises()
	    await selectElementOption(wrapper, 'subscription-scope', 'subscription-scope-option-current-filter')
	    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
	    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
	    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
	    await flushPromises()

	    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
	      scope: 'current_filter',
	      filters: { q: '', department_id: 'dept-alpha', access_status: '' },
	      operation: 'add',
	      provider_id: 3,
	      group_id: '42',
	      validity_days: 30,
	    })
	  })

  it('shows a real indeterminate select-all checkbox for partial visible selection', async () => {
    const { wrapper } = await mountAdminUsersView()
    const selectAllControl = wrapper.get('[data-testid="select-all-users"]')
    expect(selectAllControl.classes()).toContain('el-checkbox')
    const selectAll = selectAllControl.get('input').element as HTMLInputElement

    expect(selectAll.indeterminate).toBe(false)

    await setElementCheckbox(wrapper, 'select-user-7')

    expect(selectAll.checked).toBe(false)
    expect(selectAll.indeterminate).toBe(true)
    expect(selectAllControl.attributes('aria-checked')).toBe('mixed')

    await setElementCheckbox(wrapper, 'select-user-8')

    expect(selectAll.checked).toBe(true)
    expect(selectAll.indeterminate).toBe(false)
  })

  it('extends subscriptions for multiple selected local users', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', total_count: 2, processed_count: 2, success_count: 2 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await setElementCheckbox(wrapper, 'select-user-7')
    await setElementCheckbox(wrapper, 'select-user-8')
    await selectElementOption(wrapper, 'subscription-operation', 'subscription-operation-option-extend')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    await wrapper.get('[data-testid="subscription-days"]').setValue('14')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [7, 8],
      operation: 'extend',
      provider_id: 3,
      group_id: '42',
      days: 14,
    })
  })

  it('preserves selected users across pagination before submitting selected scope', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', total_count: 2, processed_count: 2, success_count: 2 }) },
    })

    const { wrapper } = await mountAdminUsersView('/admin/users', (params: any) => ({
      items: params?.page === 2
        ? [
            {
              id: 9,
              username: 'carol',
              email: 'carol@example.net',
              role: 'user',
              auth_source: 'ldap',
              relay_user_id: 101,
              relay_auth_password: '',
              created_at: '2026-05-26T00:00:00Z',
              updated_at: '2026-05-26T01:00:00Z',
            },
          ]
        : [
            {
              id: 7,
              username: 'alice',
              email: 'alice@example.com',
              role: 'user',
              auth_source: 'ldap',
              relay_user_id: 42,
              relay_auth_password: 'encrypted-relay-password-ciphertext',
              created_at: '2026-05-26T00:00:00Z',
              updated_at: '2026-05-26T01:00:00Z',
            },
          ],
      total: 40,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 20,
    }))

    await wrapper.get('[data-testid="admin-users-next-page"]').trigger('click')
    await flushPromises()
    await setElementCheckbox(wrapper, 'select-user-9')
    await wrapper.get('[data-testid="admin-users-prev-page"]').trigger('click')
    await flushPromises()
    await setElementCheckbox(wrapper, 'select-user-7')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [9, 7],
      operation: 'add',
      provider_id: 3,
      group_id: '42',
      validity_days: 30,
    })
  })

  it('removes subscriptions for all mapped users only after explicit confirmation', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', scope: 'all_mapped', operation: 'remove', total_count: 120, processed_count: 120, success_count: 120 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await selectElementOption(wrapper, 'subscription-scope', 'subscription-scope-option-all-mapped')
    await selectElementOption(wrapper, 'subscription-operation', 'subscription-operation-option-remove')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(true)

    await setElementCheckbox(wrapper, 'confirm-remove-subscription')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'all_mapped',
      operation: 'remove',
      provider_id: 3,
      group_id: '42',
    })
  })

  it('resets subscription quota for all mapped users only after explicit confirmation', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', scope: 'all_mapped', operation: 'reset_quota', total_count: 120, processed_count: 120, success_count: 120 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await selectElementOption(wrapper, 'subscription-scope', 'subscription-scope-option-all-mapped')
    await selectElementOption(wrapper, 'subscription-operation', 'subscription-operation-option-reset-quota')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(true)

    await setElementCheckbox(wrapper, 'confirm-reset-subscription-quota')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'all_mapped',
      operation: 'reset_quota',
      provider_id: 3,
      group_id: '42',
    })
  })

  it('resets subscription quota for one selected user', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', operation: 'reset_quota', total_count: 1, processed_count: 1, success_count: 1 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await setElementCheckbox(wrapper, 'select-user-7')
    await selectElementOption(wrapper, 'subscription-operation', 'subscription-operation-option-reset-quota')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    await setElementCheckbox(wrapper, 'confirm-reset-subscription-quota')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [7],
      operation: 'reset_quota',
      provider_id: 3,
      group_id: '42',
    })
  })

  it('requires a fresh reset quota confirmation after the target scope changes', async () => {
    const { wrapper } = await mountAdminUsersView()

    await setElementCheckbox(wrapper, 'select-user-7')
    await selectElementOption(wrapper, 'subscription-operation', 'subscription-operation-option-reset-quota')
    await selectElementOption(wrapper, 'subscription-provider', 'subscription-provider-option-3')
    await selectElementOption(wrapper, 'subscription-group', 'subscription-group-option-42')
    await setElementCheckbox(wrapper, 'confirm-reset-subscription-quota')
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(false)

    await selectElementOption(wrapper, 'subscription-scope', 'subscription-scope-option-all-mapped')
    await flushPromises()

    expect((wrapper.get('[data-testid="confirm-reset-subscription-quota"]').get('input').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('recovers the latest running subscription job on mount and keeps polling it', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, getLatestAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 44, status: 'running', phase: 'processing', total_count: 3, processed_count: 1 }) },
    })
    ;(getAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 44, status: 'completed', phase: 'completed', total_count: 3, processed_count: 3, success_count: 2, skipped_count: 1 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('Processing: 1 / 3')

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).toHaveBeenCalledWith(44)
    expect(wrapper.text()).toContain('Completed: 2 succeeded, 1 skipped, 0 failed')
  })

  it('shows the latest completed subscription job on mount without polling it', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, getLatestAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 46, status: 'completed', phase: 'completed', total_count: 2, processed_count: 2, success_count: 2 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('Completed: 2 succeeded, 0 skipped, 0 failed')
    expect(wrapper.text()).toContain('2 / 2')

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).not.toHaveBeenCalled()
  })

  it('disables selection controls while a subscription job is active and keeps polling', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, getLatestAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 45, status: 'running', phase: 'processing', total_count: 2, processed_count: 1 }) },
    })
    ;(getAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 45, status: 'running', phase: 'processing', total_count: 2, processed_count: 1 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    expect((wrapper.get('[data-testid="select-user-7"]').get('input').element as HTMLInputElement).disabled).toBe(true)
    expect((wrapper.get('[data-testid="select-all-users"]').get('input').element as HTMLInputElement).disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).toHaveBeenCalledWith(45)
    expect(wrapper.text()).toContain('Processing: 1 / 2')
  })
})
