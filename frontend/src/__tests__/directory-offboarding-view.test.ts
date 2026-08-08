import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import DirectoryOffboardingView from '@/views/admin/DirectoryOffboardingView.vue'
import { setLocale } from '@/i18n'
import { useWorkItemsStore } from '@/stores/workItems'

vi.mock('@/api/directory', () => ({
  listDirectoryOffboardingCandidates: vi.fn(),
  disableDirectoryRelayUser: vi.fn(),
}))

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

function candidate(userID: number, username: string, email: string) {
  return {
    user_id: userID,
    username,
    email,
    auth_source: 'ldap',
    relay_user_id: userID + 90,
    reason: 'missing_from_latest_full_company_directory',
    directory_run_id: 3,
  }
}

function candidatePage(items: ReturnType<typeof candidate>[], page = 1, total = items.length) {
  return {
    data: {
      data: {
        items,
        page,
        page_size: 20,
        total,
      },
    },
  }
}

function countsResponse(total: number) {
  return {
    data: {
      data: {
        quota_reset_approval_count: 0,
        quota_reset_admin_count: 0,
        ai_access_setup_count: 0,
        offboarding_count: total,
        total_count: total,
      },
    },
  }
}

function createRouterForOffboarding() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Home</div>' } },
      { path: '/admin/directory/offboarding', component: DirectoryOffboardingView },
    ],
  })
}

async function mountOffboarding(configureMocks?: (api: any) => void) {
  const api = await import('@/api/directory') as any
  api.listDirectoryOffboardingCandidates.mockResolvedValue(candidatePage([
    candidate(7, 'bob', 'bob@example.org'),
  ]))
  api.disableDirectoryRelayUser.mockResolvedValue({ data: { data: { id: 8, status: 'succeeded' } } })
  configureMocks?.(api)

  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouterForOffboarding()
  await router.push('/admin/directory/offboarding')
  await router.isReady()
  const wrapper = mount(DirectoryOffboardingView, {
    global: {
      plugins: [pinia, router],
      stubs: { AppLayout: { template: '<div><slot /></div>' } },
    },
  })
  await flushPromises()
  return { wrapper, api, workItems: useWorkItemsStore(pinia) }
}

describe('DirectoryOffboardingView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    setLocale('en-US')
  })

  it('renders the candidate search with Element Plus', async () => {
    const { wrapper } = await mountOffboarding()

    expect(wrapper.find('.el-input').exists()).toBe(true)
  })

  it('renders the search action with Element Plus', async () => {
    const { wrapper } = await mountOffboarding()

    expect(wrapper.get('[data-testid="offboarding-search"]').classes()).toContain('el-button')
  })

  it('renders the offboarding warning with Element Plus feedback', async () => {
    const { wrapper } = await mountOffboarding()

    expect(wrapper.get('[data-testid="offboarding-warning"]').find('.el-alert').exists()).toBe(true)
  })

  it('renders candidate failures with Element Plus feedback', async () => {
    const { wrapper } = await mountOffboarding((api) => {
      api.listDirectoryOffboardingCandidates.mockRejectedValue(new Error('load failed'))
    })

    expect(wrapper.get('[data-testid="offboarding-error"]').find('.el-alert').exists()).toBe(true)
  })

  it('renders an empty candidate result with Element Plus', async () => {
    const { wrapper } = await mountOffboarding((api) => {
      api.listDirectoryOffboardingCandidates.mockResolvedValue(candidatePage([]))
    })

    expect(wrapper.find('.el-empty').exists()).toBe(true)
  })

  it('renders candidate loading with Element Plus', async () => {
    const pending = deferred<any>()
    const { wrapper } = await mountOffboarding((api) => {
      api.listDirectoryOffboardingCandidates.mockReturnValue(pending.promise)
    })

    expect(wrapper.find('.el-skeleton').exists()).toBe(true)
  })

  it('renders candidates as responsive Element Plus cards', async () => {
    const { wrapper } = await mountOffboarding()

    expect(wrapper.find('.el-card').exists()).toBe(true)
  })

  it('renders candidate source status with Element Plus', async () => {
    const { wrapper } = await mountOffboarding()

    expect(wrapper.find('.el-tag').text()).toBe('ldap')
  })

  it('requires email confirmation before disabling relay user', async () => {
    const { wrapper, api } = await mountOffboarding()

    expect(api.listDirectoryOffboardingCandidates).toHaveBeenCalledWith({ q: '', page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('Directory Offboarding')
    expect(wrapper.text()).toContain('bob@example.org')
    expect(wrapper.text()).toContain('After disabling, this user will no longer be able to access AI services')
    expect(wrapper.find('input[type="number"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="disable-relay-user-7"]').classes()).toContain('el-button')
    expect(api.disableDirectoryRelayUser).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="disable-relay-user-7"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
    expect(wrapper.text()).toContain('Existing subscriptions are not removed automatically')
    expect(wrapper.get('[data-testid="confirm-email-7"]').element.closest('.el-input')).not.toBeNull()
    expect(wrapper.get('[data-testid="confirm-disable-relay-user-7"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="confirm-email-7"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="confirm-disable-relay-user-7"]').trigger('click')
    await flushPromises()

    expect(api.disableDirectoryRelayUser).toHaveBeenCalledWith(7, {
      confirm_email: 'bob@example.org',
      reason: 'missing_from_latest_full_company_directory',
    })
    expect(wrapper.get('[data-testid="offboarding-success"]').find('.el-alert').exists()).toBe(true)
  })

  it('uses total-aware pagination and resets to page one on search', async () => {
    const firstPage = candidate(7, 'bob', 'bob@example.org')
    const secondPage = candidate(8, 'carol', 'carol@example.com')
    const searchResult = candidate(9, 'alice', 'alice@example.com')
    const { wrapper, api } = await mountOffboarding((api) => {
      api.listDirectoryOffboardingCandidates.mockImplementation(({ q, page }: { q: string; page: number }) => {
        if (q === 'alice') return Promise.resolve(candidatePage([searchResult], 1, 1))
        if (page === 2) return Promise.resolve(candidatePage([secondPage], 2, 21))
        return Promise.resolve(candidatePage([firstPage], 1, 21))
      })
    })

    expect([
      wrapper.get('[data-testid="offboarding-prev-page"]'),
      wrapper.get('[data-testid="offboarding-next-page"]'),
    ].every((button) => button.classes().includes('el-button'))).toBe(true)
    expect(wrapper.get('[data-testid="offboarding-prev-page"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="offboarding-next-page"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="offboarding-page-status"]').text()).toContain('Page 1 / 2')
    expect(wrapper.text()).toContain('21 total')

    await wrapper.get('[data-testid="offboarding-next-page"]').trigger('click')
    await flushPromises()

    expect(api.listDirectoryOffboardingCandidates).toHaveBeenLastCalledWith({ q: '', page: 2, page_size: 20 })
    expect(wrapper.get('[data-testid="offboarding-prev-page"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="offboarding-next-page"]').attributes('disabled')).toBeDefined()

    await wrapper.get('input[type="search"]').setValue(' alice ')
    await wrapper.get('[data-testid="offboarding-search"]').trigger('click')
    await flushPromises()

    expect(api.listDirectoryOffboardingCandidates).toHaveBeenLastCalledWith({ q: 'alice', page: 1, page_size: 20 })
    expect(wrapper.get('[data-testid="offboarding-page-status"]').text()).toContain('Page 1 / 1')
    expect(wrapper.text()).toContain('alice@example.com')
  })

  it('clamps the last page and waits for one generation-safe count refresh after disable', async () => {
    const firstPage = candidate(7, 'bob', 'bob@example.org')
    const lastCandidate = candidate(8, 'carol', 'carol@example.com')
    const reloadedPage = deferred<any>()
    const previousCounts = deferred<any>()
    const freshCounts = deferred<any>()
    const workItemsApi = await import('@/api/workItems') as any
    workItemsApi.getWorkItemCounts
      .mockResolvedValueOnce(countsResponse(1))
      .mockReturnValueOnce(previousCounts.promise)
      .mockReturnValueOnce(freshCounts.promise)
    const { wrapper, api, workItems } = await mountOffboarding((api) => {
      api.listDirectoryOffboardingCandidates
        .mockResolvedValueOnce(candidatePage([firstPage], 1, 21))
        .mockResolvedValueOnce(candidatePage([lastCandidate], 2, 21))
        .mockReturnValueOnce(reloadedPage.promise)
    })

    await workItems.loadCounts()
    const previousLoad = workItems.loadCounts({ force: true })
    await wrapper.get('[data-testid="offboarding-next-page"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="disable-relay-user-8"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="confirm-email-8"]').setValue('  CAROL@example.com ')
    await wrapper.get('[data-testid="confirm-disable-relay-user-8"]').trigger('click')
    await flushPromises()

    expect(api.disableDirectoryRelayUser).toHaveBeenCalledWith(8, {
      confirm_email: 'CAROL@example.com',
      reason: 'missing_from_latest_full_company_directory',
    })
    expect(api.listDirectoryOffboardingCandidates).toHaveBeenLastCalledWith({ q: '', page: 1, page_size: 20 })
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)

    previousCounts.resolve(countsResponse(9))
    await previousLoad
    expect(workItems.totalCount).toBe(1)
    expect(workItems.loading).toBe(true)

    reloadedPage.resolve(candidatePage([firstPage], 1, 20))
    await flushPromises()
    expect(wrapper.text()).toContain('bob@example.org')
    expect(workItems.loading).toBe(true)

    freshCounts.resolve(countsResponse(0))
    await flushPromises()

    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(3)
    expect(workItems.totalCount).toBe(0)
    expect(workItems.loading).toBe(false)
  })

  it('prevents duplicate offboarding actions while disable is in flight', async () => {
    const pending = deferred<any>()
    const { wrapper, api } = await mountOffboarding((api) => {
      api.disableDirectoryRelayUser.mockReturnValue(pending.promise)
    })

    await wrapper.get('[data-testid="disable-relay-user-7"]').trigger('click')
    await wrapper.get('[data-testid="confirm-email-7"]').setValue('bob@example.org')
    const confirm = wrapper.get('[data-testid="confirm-disable-relay-user-7"]')
    await confirm.trigger('click')
    await confirm.trigger('click')

    expect(api.disableDirectoryRelayUser).toHaveBeenCalledTimes(1)
    expect(confirm.classes()).toContain('is-loading')
  })

  it('switches offboarding copy to Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountOffboarding()

    expect(wrapper.text()).toContain('组织架构离职处理')
    expect(wrapper.text()).toContain('禁用后，该用户将无法继续使用 AI 接入')
    expect(wrapper.text()).toContain('禁用 AI 接入')

    await wrapper.get('[data-testid="disable-relay-user-7"]').trigger('click')
    expect(wrapper.text()).toContain('不会自动移除现有订阅')
  })
})
