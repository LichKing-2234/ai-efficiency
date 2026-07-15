import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RepoListView from '@/views/repos/RepoListView.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/repo', () => ({
  listRepos: vi.fn().mockResolvedValue({ data: { data: { items: [], total: 0, page: 1, page_size: 20 } } }),
  getRepoInventory: vi.fn().mockResolvedValue({ data: { data: [] } }),
  createRepo: vi.fn(),
  createRepoDirect: vi.fn(),
  deleteRepo: vi.fn(),
  autoBindUnboundRepos: vi.fn(),
  repairFailedWebhooks: vi.fn(),
}))

vi.mock('@/api/scmProvider', () => ({
  listProviders: vi.fn().mockResolvedValue({
    data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
  }),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Home</div>' } },
      { path: '/repos', component: RepoListView },
      { path: '/repos/:id', component: { template: '<div>Detail</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

const sampleRepos = [
  { id: 1, repo_key: 'github.com/org/repo-a', name: 'repo-a', full_name: 'org/repo-a', clone_url: 'https://github.com/org/repo-a.git', default_branch: 'main', status: 'active', binding_state: 'bound', group_id: 0, created_at: '2026-01-01', edges: { scm_provider: { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' } } },
  { id: 2, repo_key: 'github.com/org/repo-b', name: 'repo-b', full_name: 'org/repo-b', clone_url: 'https://github.com/org/repo-b.git', default_branch: 'main', status: 'active', binding_state: 'bound', group_id: 0, created_at: '2026-01-01', edges: { scm_provider: { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' } } },
  { id: 3, repo_key: 'bb.example.com/team/repo-c', name: 'repo-c', full_name: 'team/repo-c', clone_url: 'https://bb.example.com/scm/team/repo-c.git', default_branch: 'main', status: 'active', binding_state: 'bound', group_id: 0, created_at: '2026-01-01', edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bb.example.com', status: 'active' } } },
]

function buildInventory(repos: any[]) {
  const providers = new Map<string, any>()
  for (const repo of repos) {
    const scm = repo.edges?.scm_provider
    const providerKey = scm ? `scm_provider:${scm.id}` : 'unbound'
    if (!providers.has(providerKey)) {
      providers.set(providerKey, {
        provider_key: providerKey,
        provider_id: scm?.id,
        name: scm?.name ?? 'Needs platform binding',
        type: scm?.type ?? 'unbound',
        base_url: scm?.base_url ?? '',
        total_repos: 0,
        bound_repos: 0,
        unbound_repos: 0,
        active_repos: 0,
        webhook_failed_repos: 0,
        scopes: [],
      })
    }
    const provider = providers.get(providerKey)
    const scopeName = repo.full_name.split('/')[0] || repo.name
    let scope = provider.scopes.find((item: any) => item.scope === scopeName)
    if (!scope) {
      scope = { scope: scopeName, total_repos: 0, bound_repos: 0, unbound_repos: 0, active_repos: 0, webhook_failed_repos: 0 }
      provider.scopes.push(scope)
    }

    provider.total_repos += 1
    scope.total_repos += 1
    if (repo.binding_state === 'bound') {
      provider.bound_repos += 1
      scope.bound_repos += 1
    } else {
      provider.unbound_repos += 1
      scope.unbound_repos += 1
    }
    if (repo.status === 'active') {
      provider.active_repos += 1
      scope.active_repos += 1
    }
    if (repo.status === 'webhook_failed') {
      provider.webhook_failed_repos += 1
      scope.webhook_failed_repos += 1
    }
  }
  return Array.from(providers.values())
}

function filterReposForParams(repos: any[], params: any = {}) {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 20
  let effectiveParams = { ...params }
  let selection: any
  if (!params.scmProviderId && !params.scope && !params.bindingState) {
    const inventory = buildInventory(repos).sort((a, b) => {
      const rank = (provider: any) => provider.type === 'github' ? 0 : provider.type === 'bitbucket_server' ? 1 : provider.provider_key === 'unbound' ? 3 : 2
      return rank(a) - rank(b) || a.name.localeCompare(b.name) || a.provider_key.localeCompare(b.provider_key)
    })
    const provider = inventory[0]
    const scope = provider?.scopes.slice().sort((a: any, b: any) => a.scope.localeCompare(b.scope))[0]
    if (provider && scope) {
      selection = {
        provider_key: provider.provider_key,
        provider_id: provider.provider_id,
        provider_name: provider.name,
        provider_type: provider.type,
        scope: scope.scope,
        binding_state: provider.provider_key === 'unbound' ? 'unbound' : 'bound',
      }
      effectiveParams = {
        ...effectiveParams,
        scmProviderId: provider.provider_id,
        scope: scope.scope,
        bindingState: selection.binding_state,
      }
    }
  }
  let items = [...repos]

  if (effectiveParams.scmProviderId) {
    items = items.filter((repo) => repo.edges?.scm_provider?.id === effectiveParams.scmProviderId || repo.scm_provider_id === effectiveParams.scmProviderId)
  }
  if (effectiveParams.scope) {
    items = items.filter((repo) => repo.full_name === effectiveParams.scope || repo.full_name.startsWith(`${effectiveParams.scope}/`))
  }
  if (effectiveParams.bindingState) {
    items = items.filter((repo) => repo.binding_state === effectiveParams.bindingState)
  }

  const total = items.length
  const start = (page - 1) * pageSize
  return { items: items.slice(start, start + pageSize), total, page, page_size: pageSize, ...(selection ? { selection } : {}) }
}

async function mountRepoList(repos?: any[], path = '/repos', options?: { admin?: boolean; useCurrentMocks?: boolean }) {
  const { listRepos, getRepoInventory } = await import('@/api/repo')
  const repoItems = repos ?? []
  if (!options?.useCurrentMocks) {
    ;(getRepoInventory as any).mockResolvedValue({ data: { data: buildInventory(repoItems) } })
    ;(listRepos as any).mockImplementation((params: any = {}) =>
      Promise.resolve({ data: { data: filterReposForParams(repoItems, params) } })
    )
  }

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.user = {
    id: 1,
    username: options?.admin ? 'admin' : 'alice',
    email: options?.admin ? 'admin@example.com' : 'alice@example.com',
    role: options?.admin ? 'admin' : 'user',
    auth_source: 'sso',
  }

  const wrapper = mount(RepoListView, {
    global: { plugins: [pinia, router] },
  })

  await flushPromises()
  await wrapper.vm.$nextTick()

  return { wrapper, router }
}

describe('RepoListView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('renders page title and add button', async () => {
    const { wrapper } = await mountRepoList()
    expect(wrapper.find('h1').text()).toBe('Code Repositories')
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    expect(addBtn).toBeTruthy()
  })

  it('starts list and inventory together and renders server-selected rows before inventory', async () => {
    const { listRepos, getRepoInventory } = await import('@/api/repo')
    const { listProviders } = await import('@/api/scmProvider')
    let resolveInventory!: (value: any) => void
    const inventoryPromise = new Promise((resolve) => { resolveInventory = resolve })
    ;(getRepoInventory as any).mockReturnValue(inventoryPromise)
    ;(listRepos as any).mockResolvedValue({
      data: {
        data: {
          items: [sampleRepos[0]],
          total: 1,
          page: 1,
          page_size: 20,
          selection: {
            provider_key: 'scm_provider:1',
            provider_id: 1,
            provider_name: 'GitHub',
            provider_type: 'github',
            scope: 'org',
            binding_state: 'bound',
          },
        },
      },
    })

    const { wrapper } = await mountRepoList(undefined, '/repos', { useCurrentMocks: true })

    expect(listRepos).toHaveBeenCalledTimes(1)
    expect(getRepoInventory).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('[data-testid="repo-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('repo-a')
    expect(listProviders).not.toHaveBeenCalled()

    resolveInventory({ data: { data: buildInventory([sampleRepos[0]]) } })
    await flushPromises()
    expect(listRepos).toHaveBeenCalledTimes(1)
  })

  it('sends explicit route selection immediately without waiting for inventory', async () => {
    const { listRepos, getRepoInventory } = await import('@/api/repo')
    ;(getRepoInventory as any).mockReturnValue(new Promise(() => {}))
    ;(listRepos as any).mockResolvedValue({ data: { data: { items: [sampleRepos[2]], total: 1, page: 2, page_size: 10 } } })

    const { wrapper } = await mountRepoList(undefined, '/repos?provider=scm_provider:2&scope=team&binding=bound&page=2&page_size=10', { useCurrentMocks: true })

    expect(listRepos).toHaveBeenCalledWith({
      page: 2,
      pageSize: 10,
      scmProviderId: 2,
      scope: 'team',
      bindingState: 'bound',
    })
    expect(wrapper.text()).toContain('repo-c')
  })

  it('keeps list rows visible when inventory fails', async () => {
    const { listRepos, getRepoInventory } = await import('@/api/repo')
    ;(getRepoInventory as any).mockRejectedValue(new Error('inventory timeout'))
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: [sampleRepos[0]], total: 1, page: 1, page_size: 20, selection: { provider_key: 'scm_provider:1', provider_id: 1, provider_name: 'GitHub', provider_type: 'github', scope: 'org', binding_state: 'bound' } } },
    })

    const { wrapper } = await mountRepoList(undefined, '/repos', { useCurrentMocks: true })
    expect(wrapper.findAll('[data-testid="repo-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('repo-a')
  })

  it('mounts exactly one responsive row subtree per repository', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)
    expect(wrapper.findAll('[data-testid="repo-row"]')).toHaveLength(2)
  })

  it('switches repository workbench labels to Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountRepoList(sampleRepos)

    expect(wrapper.text()).toContain('代码仓库')
    expect(wrapper.text()).toContain('仓库健康度')
    expect(wrapper.text()).toContain('全部绑定状态')
    expect(wrapper.text()).toContain('新增仓库')
    expect(wrapper.text()).toContain('仓库总数')
    expect(wrapper.text()).toContain('已绑定仓库')
    expect(wrapper.text()).toContain('待绑定')

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('新增仓库'))
    await addBtn!.trigger('click')
    await flushPromises()
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('完整名称')
    expect(wrapper.text()).toContain('名称')
  })

  it('shows empty state when no repos', async () => {
    const { wrapper } = await mountRepoList([])
    expect(wrapper.text()).toContain('No repositories found')
  })

  it('shows an Unbound badge for repos without scm_provider', async () => {
    const { wrapper } = await mountRepoList([
      {
        id: 7,
        repo_key: 'github.com/acme/repo-unbound',
        name: 'repo-unbound',
        full_name: 'acme/repo-unbound',
        clone_url: 'https://github.com/acme/repo-unbound.git',
        default_branch: 'main',
        status: 'active',
        binding_state: 'unbound',
        group_id: 0,
        created_at: '2026-01-01T00:00:00Z',
        edges: {},
      },
    ])

    expect(wrapper.text()).toContain('Unbound')
  })

  it('shows auto-bind action only for admins', async () => {
    const admin = await mountRepoList(sampleRepos, '/repos', { admin: true })
    expect(admin.wrapper.find('[data-testid="repo-auto-bind-button"]').exists()).toBe(true)

    const user = await mountRepoList(sampleRepos, '/repos', { admin: false })
    expect(user.wrapper.find('[data-testid="repo-auto-bind-button"]').exists()).toBe(false)
  })

  it('runs auto-bind and shows a summary', async () => {
    const { autoBindUnboundRepos, listRepos } = await import('@/api/repo')
    ;(autoBindUnboundRepos as any).mockResolvedValue({
      data: {
        data: {
          summary: {
            scanned: 3,
            bound: 1,
            already_bound: 0,
            skipped_no_match: 1,
            skipped_ambiguous: 1,
            webhook_failed: 0,
            errors: 0,
          },
          items: [],
        },
      },
    })

    const { wrapper } = await mountRepoList(sampleRepos, '/repos', { admin: true })
    await wrapper.get('[data-testid="repo-auto-bind-button"]').trigger('click')
    await flushPromises()

    expect(autoBindUnboundRepos).toHaveBeenCalledTimes(1)
    expect(listRepos).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Auto-bind complete')
    expect(wrapper.text()).toContain('1 bound')
    expect(wrapper.text()).toContain('1 no match')
    expect(wrapper.text()).toContain('1 ambiguous')
  })

  it('shows batch webhook repair only for admins', async () => {
    const repos = [
      {
        id: 11,
        repo_key: 'bitbucket.example.com/PROJ/repo',
        name: 'repo',
        full_name: 'PROJ/repo',
        clone_url: 'https://bitbucket.example.com/scm/proj/repo.git',
        default_branch: 'main',
        status: 'webhook_failed',
        binding_state: 'bound',
        group_id: 0,
        created_at: '2026-06-06T00:00:00Z',
        edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' } },
      },
    ]

    const admin = await mountRepoList(repos, '/repos', { admin: true })
    expect(admin.wrapper.find('[data-testid="repo-repair-webhooks-button"]').exists()).toBe(true)
    expect(admin.wrapper.text()).toContain('Repair failed webhooks')

    const user = await mountRepoList(repos, '/repos', { admin: false })
    expect(user.wrapper.find('[data-testid="repo-repair-webhooks-button"]').exists()).toBe(false)
    expect(user.wrapper.text()).not.toContain('Repair failed webhooks')
  })

  it('runs batch webhook repair and refreshes repo list', async () => {
    const { repairFailedWebhooks, listRepos } = await import('@/api/repo')
    ;(repairFailedWebhooks as any).mockResolvedValue({
      data: {
        data: {
          summary: { scanned: 2, repaired: 1, already_registered: 0, failed: 1 },
          items: [],
        },
      },
    })

    const { wrapper } = await mountRepoList(sampleRepos, '/repos', { admin: true })
    await wrapper.get('[data-testid="repo-repair-webhooks-button"]').trigger('click')
    await flushPromises()

    expect(repairFailedWebhooks).toHaveBeenCalledWith({ force: false })
    expect(listRepos).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Webhook repair complete')
    expect(wrapper.text()).toContain('1 repaired')
  })

  it('opens add dialog on button click', async () => {
    const { wrapper } = await mountRepoList()
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Add Repository')
    expect(wrapper.find('input[placeholder*="github.com"]').exists()).toBe(true)
  })

  it('closes add repository dialog with Escape', async () => {
    const { wrapper } = await mountRepoList()
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('Add Repository')
  })

  it('auto-fills name and clone_url from GitHub URL', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('myorg/myrepo')
    expect(wrapper.text()).toContain('myrepo')
  })

  it('auto-fills from Bitbucket Server URL', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://bitbucket.example.com/projects/MYPROJ/repos/my-repo/browse')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('MYPROJ/my-repo')
    expect(wrapper.text()).toContain('my-repo')
  })

  it('closes dialog on cancel', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Add Repository')

    const cancelBtn = wrapper.findAll('button').find((b) => b.text() === 'Cancel')
    await cancelBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('Add Repository')
  })

  it('shows validation error when full_name is empty', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    const submitBtn = wrapper.findAll('button').find((b) => b.text() === 'Add')
    await submitBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Please enter a valid repo URL')
  })

  it('renders platform tabs, scope index, and the selected scope table', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    expect(wrapper.text()).toContain('Repository health')
    expect(wrapper.text()).toContain('Total repositories')
    expect(wrapper.text()).toContain('Bound repositories')
    expect(wrapper.text()).toContain('Needs binding')
    expect(wrapper.text()).toContain('Platform')
    expect(wrapper.text()).toContain('Org / Project')
    expect(wrapper.text()).toContain('GitHub')
    expect(wrapper.text()).toContain('Bitbucket')
    expect(wrapper.text()).toContain('org')
    expect(wrapper.text()).toContain('repo-a')
    expect(wrapper.text()).toContain('repo-b')
    expect(wrapper.text()).not.toContain('repo-c')
    expect(wrapper.text()).toContain('active')
  })

  it('filters repositories by binding state from the health workbench', async () => {
    const { wrapper } = await mountRepoList([
      ...sampleRepos,
      {
        id: 4,
        repo_key: 'github.com/org/repo-unbound',
        name: 'repo-unbound',
        full_name: 'org/repo-unbound',
        clone_url: 'https://github.com/org/repo-unbound.git',
        default_branch: 'main',
        status: 'active',
        binding_state: 'unbound',
        group_id: 0,
        created_at: '2026-01-01',
        edges: {},
      },
    ])

    expect(wrapper.text()).toContain('Auto-discovered repositories need a code platform binding before PR sync can run.')

    await wrapper.find('[data-testid="repo-binding-filter"]').setValue('unbound')
    await flushPromises()

    expect(wrapper.text()).toContain('repo-unbound')
    expect(wrapper.text()).not.toContain('repo-a')
  })

  it('restores and persists the binding filter in the URL query', async () => {
    const { wrapper, router } = await mountRepoList([
      ...sampleRepos,
      {
        id: 4,
        repo_key: 'github.com/org/repo-unbound',
        name: 'repo-unbound',
        full_name: 'org/repo-unbound',
        clone_url: 'https://github.com/org/repo-unbound.git',
        default_branch: 'main',
        status: 'active',
        binding_state: 'unbound',
        group_id: 0,
        created_at: '2026-01-01',
        edges: {},
      },
    ], '/repos?binding=unbound')

    expect(wrapper.text()).toContain('repo-unbound')
    expect(wrapper.text()).not.toContain('repo-a')

    await wrapper.find('[data-testid="repo-binding-filter"]').setValue('all')
    await flushPromises()

    expect(router.currentRoute.value.query.binding).toBeUndefined()
  })

  it('navigates to repo detail on row click', async () => {
    const { wrapper, router } = await mountRepoList(sampleRepos)

    const rows = wrapper.findAll('[data-testid="repo-row"]')
    expect(rows.length).toBeGreaterThan(0)
    await rows[0].trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/repos/1')
  })

  it('switches platform tabs without mixing scopes', async () => {
    const { listRepos } = await import('@/api/repo')
    const { wrapper } = await mountRepoList(sampleRepos)

    const bitbucketTab = wrapper.findAll('[data-testid="repo-platform-tab"]').find((button) => button.text().includes('Bitbucket'))
    await bitbucketTab!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('team')
    expect(wrapper.text()).toContain('repo-c')
    expect(wrapper.text()).not.toContain('repo-a')
    expect(listRepos).toHaveBeenLastCalledWith({
      page: 1,
      pageSize: 20,
      scmProviderId: 2,
      scope: 'team',
    })
  })

  it('paginates only the selected platform scope', async () => {
    const { listRepos } = await import('@/api/repo')
    const manyOrgRepos = Array.from({ length: 25 }, (_, index) => ({
      id: index + 1,
      repo_key: `github.com/org/repo-${index + 1}`,
      name: `repo-${index + 1}`,
      full_name: `org/repo-${index + 1}`,
      clone_url: `https://github.com/org/repo-${index + 1}.git`,
      default_branch: 'main',
      status: 'active',
      binding_state: 'bound',
      group_id: 0,
      created_at: '2026-01-01',
      edges: { scm_provider: { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' } },
    }))

    const { wrapper } = await mountRepoList([
      ...manyOrgRepos,
      { ...sampleRepos[2], id: 99, name: 'repo-outside-scope', full_name: 'team/repo-outside-scope' },
    ])

    expect(wrapper.text()).toContain('repo-1')
    expect(wrapper.text()).not.toContain('repo-25')

    await wrapper.get('[data-testid="repo-next-page"]').trigger('click')
    await flushPromises()

    expect(listRepos).toHaveBeenLastCalledWith({
      page: 2,
      pageSize: 20,
      scmProviderId: 1,
      scope: 'org',
    })
    expect(wrapper.text()).toContain('repo-25')
    expect(wrapper.text()).not.toContain('repo-outside-scope')
  })

  it('shows delete confirm and deletes repo', async () => {
    const { deleteRepo } = await import('@/api/repo')
    ;(deleteRepo as any).mockResolvedValue({ data: { data: null } })

    const { wrapper } = await mountRepoList(sampleRepos)

    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Click Confirm
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(deleteRepo).toHaveBeenCalledWith(1)
  })

  it('cancels delete confirm', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    const cancelBtn = wrapper.findAll('button').find((b) => b.text() === 'Cancel')
    await cancelBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Delete button should be back
    const deleteBtnAgain = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    expect(deleteBtnAgain).toBeTruthy()
  })

  it('submits add repo form successfully', async () => {
    const { createRepoDirect, listRepos } = await import('@/api/repo')
    ;(createRepoDirect as any).mockResolvedValue({ data: { data: { id: 10, name: 'new-repo' } } })
    ;(listRepos as any).mockResolvedValue({ data: { data: { items: [], total: 0, page: 1, page_size: 20 } } })

    const { wrapper } = await mountRepoList()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Submit
    const submitBtn = wrapper.findAll('button').find((b) => b.text() === 'Add')
    await submitBtn!.trigger('click')
    await flushPromises()

    expect(createRepoDirect).toHaveBeenCalled()
  })

  it('handles add repo error', async () => {
    const { createRepoDirect } = await import('@/api/repo')
    ;(createRepoDirect as any).mockRejectedValue({
      response: { data: { message: 'Repo already exists' } },
    })

    const { wrapper } = await mountRepoList()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Submit
    const submitBtn = wrapper.findAll('button').find((b) => b.text() === 'Add')
    await submitBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Repo already exists')
  })

  it('switches clone protocol to SSH for GitHub', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill GitHub URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Click SSH button
    const sshBtn = wrapper.findAll('button').find((b) => b.text() === 'SSH')
    await sshBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Clone URL should be SSH format
    const cloneUrlInput = wrapper.find('input.font-mono')
    expect((cloneUrlInput.element as HTMLInputElement).value).toContain('git@github.com:myorg/myrepo.git')
  })

  it('switches clone protocol to HTTP for GitHub', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill GitHub URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Switch to SSH then back to HTTP
    const sshBtn = wrapper.findAll('button').find((b) => b.text() === 'SSH')
    await sshBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    const httpBtn = wrapper.findAll('button').find((b) => b.text() === 'HTTP')
    await httpBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    const cloneUrlInput = wrapper.find('input.font-mono')
    expect((cloneUrlInput.element as HTMLInputElement).value).toContain('https://github.com/myorg/myrepo.git')
  })

  it('switches clone protocol to SSH for Bitbucket', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill Bitbucket URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://bitbucket.example.com/projects/PROJ/repos/my-repo/browse')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Click SSH button
    const sshBtn = wrapper.findAll('button').find((b) => b.text() === 'SSH')
    await sshBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    const cloneUrlInput = wrapper.find('input.font-mono')
    expect((cloneUrlInput.element as HTMLInputElement).value).toContain('ssh://git@')
  })

  it('updates SSH host for Bitbucket', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill Bitbucket URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://bitbucket.example.com/projects/PROJ/repos/my-repo/browse')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Switch to SSH
    const sshBtn = wrapper.findAll('button').find((b) => b.text() === 'SSH')
    await sshBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Fill SSH host
    const sshHostInput = wrapper.find('input[placeholder*="SSH host"]')
    await sshHostInput.setValue('git.example.com')
    await sshHostInput.trigger('input')
    await wrapper.vm.$nextTick()

    const cloneUrlInput = wrapper.find('input.font-mono')
    expect((cloneUrlInput.element as HTMLInputElement).value).toContain('git@git.example.com')
  })

  it('handles Bitbucket HTTP clone URL', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://bitbucket.example.com/projects/PROJ/repos/my-repo/browse')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    const cloneUrlInput = wrapper.find('input.font-mono')
    expect((cloneUrlInput.element as HTMLInputElement).value).toContain('/scm/proj/my-repo.git')
  })

  it('clears form when URL is emptied', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill URL
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://github.com/myorg/myrepo')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('myorg/myrepo')

    // Clear URL
    await repoUrlInput.setValue('')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Derived info should be gone
    expect(wrapper.text()).not.toContain('myorg/myrepo')
  })

  it('handles invalid URL gracefully', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('not-a-valid-url')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // Should not crash, no parsed info shown
    expect(wrapper.find('input.font-mono').exists()).toBe(false)
  })

  it('auto-selects provider matching URL origin', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockResolvedValue({
      data: {
        data: [
          { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' },
          { id: 2, name: 'BB', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' },
        ],
      },
    })

    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Fill Bitbucket URL - should auto-select BB provider
    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://bitbucket.example.com/projects/PROJ/repos/my-repo/browse')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // The select should have the BB provider selected
    const selects = wrapper.findAll('select')
    const select = selects[selects.length - 1]
    expect((select.element as HTMLSelectElement).value).toBe('2')
  })

  it('handles listProviders error when opening add dialog', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockRejectedValue(new Error('network error'))

    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Dialog should still open
    expect(wrapper.text()).toContain('Add Repository')
    expect(wrapper.text()).toContain('No code platforms found')
  })

  it('renders repo rows without the retired last scan column', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    expect(wrapper.text()).not.toContain('Last Scan')
    expect(wrapper.text()).toContain('repo-a')
    expect(wrapper.text()).toContain('repo-b')
  })

  it('handles URL that does not match any pattern', async () => {
    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    const repoUrlInput = wrapper.find('input[placeholder*="github.com"]')
    await repoUrlInput.setValue('https://example.com/some/random/path')
    await repoUrlInput.trigger('input')
    await wrapper.vm.$nextTick()

    // No parsed info should be shown
    expect(wrapper.find('input.font-mono').exists()).toBe(false)
  })

  it('handles providers returned as array directly', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockResolvedValue({
      data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
    })

    const { wrapper } = await mountRepoList()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()

    // Should have the provider in the select
    expect(wrapper.text()).not.toContain('No code platforms found')
  })
})
