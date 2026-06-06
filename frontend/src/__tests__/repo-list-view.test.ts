import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RepoListView from '@/views/repos/RepoListView.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/repo', () => ({
  listRepos: vi.fn().mockResolvedValue({ data: { data: { items: [], total: 0, page: 1, page_size: 20 } } }),
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

async function mountRepoList(repos?: any[], path = '/repos', options?: { admin?: boolean }) {
  const { listRepos } = await import('@/api/repo')
  if (repos) {
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: repos, total: repos.length, page: 1, page_size: 20 } },
    })
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

  it('displays repos in grouped table', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    expect(wrapper.text()).toContain('Repository health')
    expect(wrapper.text()).toContain('Total repositories')
    expect(wrapper.text()).toContain('Bound repositories')
    expect(wrapper.text()).toContain('Needs binding')
    expect(wrapper.text()).toContain('repo-a')
    expect(wrapper.text()).toContain('repo-b')
    expect(wrapper.text()).toContain('repo-c')
    expect(wrapper.text()).toContain('org')
    expect(wrapper.text()).toContain('team')
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
    await wrapper.vm.$nextTick()

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

    const rows = wrapper.findAll('tr.cursor-pointer')
    expect(rows.length).toBeGreaterThan(0)
    await rows[0].trigger('click')
    await flushPromises()

    // Groups are sorted alphabetically: Bitbucket::team (repo-c id=3) comes before GitHub::org
    expect(router.currentRoute.value.path).toBe('/repos/3')
  })

  it('toggles group collapse', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    // Find group header buttons
    const groupHeaders = wrapper.findAll('button.flex.w-full')
    expect(groupHeaders.length).toBeGreaterThan(0)

    // Click to collapse
    await groupHeaders[0].trigger('click')
    await wrapper.vm.$nextTick()

    // Click again to expand
    await groupHeaders[0].trigger('click')
    await wrapper.vm.$nextTick()

    // Should still show repos
    expect(wrapper.text()).toContain('repo-a')
  })

  it('shows delete confirm and deletes repo', async () => {
    const { deleteRepo } = await import('@/api/repo')
    ;(deleteRepo as any).mockResolvedValue({ data: { data: null } })

    const { wrapper } = await mountRepoList(sampleRepos)

    // Click first Delete button (Bitbucket::team group comes first alphabetically, repo-c id=3)
    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Click Confirm
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(deleteRepo).toHaveBeenCalledWith(3)
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
