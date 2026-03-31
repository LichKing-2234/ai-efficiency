import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RepoListView from '@/views/repos/RepoListView.vue'

vi.mock('@/api/repo', () => ({
  listRepos: vi.fn().mockResolvedValue({ data: { data: { items: [], total: 0, page: 1, page_size: 20 } } }),
  createRepo: vi.fn(),
  createRepoDirect: vi.fn(),
  deleteRepo: vi.fn(),
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
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

const sampleRepos = [
  { id: 1, name: 'repo-a', full_name: 'org/repo-a', clone_url: 'https://github.com/org/repo-a.git', default_branch: 'main', ai_score: 85, status: 'active', last_scan_at: '2026-03-01T00:00:00Z', group_id: 0, created_at: '2026-01-01', edges: { scm_provider: { name: 'GitHub', type: 'github' } } },
  { id: 2, name: 'repo-b', full_name: 'org/repo-b', clone_url: 'https://github.com/org/repo-b.git', default_branch: 'main', ai_score: 35, status: 'active', last_scan_at: null, group_id: 0, created_at: '2026-01-01', edges: { scm_provider: { name: 'GitHub', type: 'github' } } },
  { id: 3, name: 'repo-c', full_name: 'team/repo-c', clone_url: 'https://bb.example.com/scm/team/repo-c.git', default_branch: 'main', ai_score: 55, status: 'active', last_scan_at: null, group_id: 0, created_at: '2026-01-01', edges: { scm_provider: { name: 'Bitbucket', type: 'bitbucket_server' } } },
]

async function mountRepoList(repos?: any[]) {
  const { listRepos } = await import('@/api/repo')
  if (repos) {
    ;(listRepos as any).mockResolvedValue({
      data: { data: { items: repos, total: repos.length, page: 1, page_size: 20 } },
    })
  }

  const router = createTestRouter()
  await router.push('/repos')
  await router.isReady()

  const wrapper = mount(RepoListView, {
    global: { plugins: [createPinia(), router] },
  })

  await flushPromises()
  await wrapper.vm.$nextTick()

  return { wrapper, router }
}

describe('RepoListView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders page title and add button', async () => {
    const { wrapper } = await mountRepoList()
    expect(wrapper.find('h1').text()).toBe('Repositories')
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    expect(addBtn).toBeTruthy()
  })

  it('shows empty state when no repos', async () => {
    const { wrapper } = await mountRepoList([])
    expect(wrapper.text()).toContain('No repositories found')
  })

  it('opens add dialog on button click', async () => {
    const { wrapper } = await mountRepoList()
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Repo'))
    await addBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Add Repository')
    expect(wrapper.find('input[placeholder*="github.com"]').exists()).toBe(true)
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

    expect(wrapper.text()).toContain('repo-a')
    expect(wrapper.text()).toContain('repo-b')
    expect(wrapper.text()).toContain('repo-c')
    expect(wrapper.text()).toContain('org')
    expect(wrapper.text()).toContain('team')
    expect(wrapper.text()).toContain('85')
    expect(wrapper.text()).toContain('35')
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
    const select = wrapper.find('select')
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
    expect(wrapper.text()).toContain('No SCM providers found')
  })

  it('formats date for last_scan_at', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    // repo-a has last_scan_at, repo-b has null
    // null should show dash
    expect(wrapper.text()).toContain('—')
  })

  it('shows score color coding', async () => {
    const { wrapper } = await mountRepoList(sampleRepos)

    // Score 85 should have green class
    const scoreSpans = wrapper.findAll('span.rounded-full')
    const greenScore = scoreSpans.find((s) => s.text() === '85')
    expect(greenScore?.classes()).toContain('bg-green-100')

    // Score 35 should have red class
    const redScore = scoreSpans.find((s) => s.text() === '35')
    expect(redScore?.classes()).toContain('bg-red-100')

    // Score 55 should have yellow class
    const yellowScore = scoreSpans.find((s) => s.text() === '55')
    expect(yellowScore?.classes()).toContain('bg-yellow-100')
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
    expect(wrapper.text()).not.toContain('No SCM providers found')
  })
})
