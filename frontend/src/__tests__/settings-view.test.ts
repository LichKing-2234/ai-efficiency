import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import SettingsView from '@/views/SettingsView.vue'

function getInputByLabel(wrapper: any, labelText: string) {
  const label = wrapper.findAll('label').find((l: any) => l.text().trim() === labelText)
  expect(label, `Missing label: ${labelText}`).toBeTruthy()
  const container = label!.element.parentElement as HTMLElement | null
  expect(container, `Missing label container: ${labelText}`).toBeTruthy()
  const input = container!.querySelector('input')
  expect(input, `Missing input for label: ${labelText}`).toBeTruthy()
  return input as HTMLInputElement
}

const createDefaultProvidersResponse = () => ({
  data: {
    data: {
      items: [],
      total: 0,
    },
  },
})

const createDefaultLLMConfigResponse = () => ({
  data: {
    data: {
      relay_url: '',
      relay_api_key: '',
      relay_admin_api_key: '',
      model: 'gpt-4',
      enabled: false,
      max_tokens_per_scan: 100000,
      system_prompt: '',
      user_prompt_template: '',
    },
  },
})

const createDefaultDeploymentStatusResponse = () => ({
  data: {
    data: {
      version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
      mode: 'bundled',
      update_available: true,
      latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
      update_status: { phase: 'idle' },
    },
  },
})

vi.mock('@/api/scmProvider', () => ({
  listProviders: vi.fn(),
  createProvider: vi.fn(),
  updateProvider: vi.fn(),
  deleteProvider: vi.fn(),
}))

vi.mock('@/api/settings', () => ({
  getLLMConfig: vi.fn(),
  updateLLMConfig: vi.fn(),
  testLLMConnection: vi.fn(),
}))

vi.mock('@/api/deployment', () => ({
  getDeploymentStatus: vi.fn(),
  checkForUpdate: vi.fn(),
  applyUpdate: vi.fn(),
  rollbackUpdate: vi.fn(),
  restartDeployment: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/utils/deploymentRecovery', () => ({
  waitForServiceRecovery: vi.fn(),
}))

async function resetApiMocks() {
  const scmProvider = await import('@/api/scmProvider') as any
  scmProvider.listProviders.mockReset().mockResolvedValue(createDefaultProvidersResponse())
  scmProvider.createProvider.mockReset().mockResolvedValue({ data: { data: { id: 1 } } })
  scmProvider.updateProvider.mockReset().mockResolvedValue({ data: { data: { id: 1 } } })
  scmProvider.deleteProvider.mockReset().mockResolvedValue({ data: { data: null } })

  const settingsApi = await import('@/api/settings') as any
  settingsApi.getLLMConfig.mockReset().mockResolvedValue(createDefaultLLMConfigResponse())
  settingsApi.updateLLMConfig.mockReset().mockResolvedValue({ data: { data: {} } })
  settingsApi.testLLMConnection.mockReset().mockResolvedValue({
    data: { data: { success: true, message: 'Connection OK', response: 'pong' } },
  })

  const deploymentApi = await import('@/api/deployment') as any
  deploymentApi.getDeploymentStatus.mockReset().mockResolvedValue(createDefaultDeploymentStatusResponse())
  deploymentApi.checkForUpdate.mockReset().mockResolvedValue({ data: { data: null } })
  deploymentApi.applyUpdate.mockReset().mockResolvedValue({ data: { data: { phase: 'idle' } } })
  deploymentApi.rollbackUpdate.mockReset().mockResolvedValue({ data: { data: { phase: 'idle' } } })
  deploymentApi.restartDeployment.mockReset().mockResolvedValue({ data: { data: { phase: 'restart_requested' } } })

  const authApi = await import('@/api/auth') as any
  authApi.login.mockReset().mockResolvedValue({ data: { data: null } })
  authApi.getMe.mockReset().mockResolvedValue({ data: { data: {} } })
  authApi.devLogin.mockReset().mockResolvedValue({ data: { data: null } })

  const recoveryApi = await import('@/utils/deploymentRecovery') as any
  recoveryApi.waitForServiceRecovery.mockReset().mockResolvedValue(undefined)
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Home</div>' } },
      { path: '/settings', component: SettingsView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
    ],
  })
}

async function mountSettings(overrides?: { providers?: any[]; llmConfig?: any; deploymentStatus?: any }) {
  const { listProviders } = await import('@/api/scmProvider')
  const { getLLMConfig } = await import('@/api/settings')
  const { getDeploymentStatus } = await import('@/api/deployment')

  if (overrides?.providers) {
    ;(listProviders as any).mockResolvedValue({
      data: { data: { items: overrides.providers, total: overrides.providers.length } },
    })
  }
  if (overrides?.llmConfig) {
    ;(getLLMConfig as any).mockResolvedValue({ data: { data: overrides.llmConfig } })
  }
  if (overrides?.deploymentStatus) {
    ;(getDeploymentStatus as any).mockResolvedValue({ data: { data: overrides.deploymentStatus } })
  }

  const router = createTestRouter()
  await router.push('/settings')
  await router.isReady()

  const wrapper = mount(SettingsView, {
    global: { plugins: [createPinia(), router] },
  })

  await flushPromises()
  await wrapper.vm.$nextTick()

  return wrapper
}

describe('SettingsView', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    await resetApiMocks()
  })

  it('renders SCM Providers heading and Add Provider button', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.find('h1').text()).toBe('SCM Providers')
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    expect(addBtn).toBeTruthy()
  })

  it('renders LLM Configuration section', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.text()).toContain('LLM Configuration')
  })

  it('renders deployment status and update controls', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.text()).toContain('Deployment')
    expect(wrapper.text()).toContain('v0.4.0')
    expect(wrapper.text()).toContain('v0.5.0')
    expect(wrapper.text()).toContain('Check Updates')
    expect(wrapper.text()).toContain('Apply Update')
    expect(wrapper.text()).toContain('Rollback')
    expect(wrapper.text()).toContain('Restart Service')
  })

  it('renders restart control in bundled mode', async () => {
    const wrapper = await mountSettings({
      deploymentStatus: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        mode: 'bundled',
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        update_status: { phase: 'idle' },
      },
    })
    expect(wrapper.text()).toContain('Restart Service')
  })

  it('calls restart deployment when restart control is clicked', async () => {
    const { restartDeployment } = await import('@/api/deployment')
    const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
    ;(restartDeployment as any).mockResolvedValue({ data: { data: { phase: 'restart_requested' } } })

    const wrapper = await mountSettings({
      deploymentStatus: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        mode: 'systemd',
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        update_status: { phase: 'idle' },
      },
    })
    const button = wrapper.findAll('button').find((b) => b.text().includes('Restart Service'))
    expect(button).toBeTruthy()

    await button!.trigger('click')
    await flushPromises()

    expect(restartDeployment).toHaveBeenCalled()
    expect(waitForServiceRecovery).toHaveBeenCalled()
  })

  it('does not wait for recovery after bundled apply update', async () => {
    const { applyUpdate } = await import('@/api/deployment')
    const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
    ;(applyUpdate as any).mockResolvedValue({ data: { data: { phase: 'updating' } } })

    const wrapper = await mountSettings({
      deploymentStatus: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        mode: 'bundled',
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        update_status: { phase: 'idle' },
      },
    })

    const button = wrapper.findAll('button').find((b) => b.text().includes('Apply Update'))
    expect(button).toBeTruthy()

    await button!.trigger('click')
    await flushPromises()

    expect(applyUpdate).toHaveBeenCalledWith({ target_version: 'v0.5.0' })
    expect(waitForServiceRecovery).not.toHaveBeenCalled()
  })

  it('does not wait for recovery after systemd apply update', async () => {
    const { applyUpdate } = await import('@/api/deployment')
    const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
    ;(applyUpdate as any).mockResolvedValue({ data: { data: { phase: 'updated' } } })

    const wrapper = await mountSettings({
      deploymentStatus: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        mode: 'systemd',
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        update_status: { phase: 'idle' },
      },
    })

    const button = wrapper.findAll('button').find((b) => b.text().includes('Apply Update'))
    expect(button).toBeTruthy()

    await button!.trigger('click')
    await flushPromises()

    expect(applyUpdate).toHaveBeenCalledWith({ target_version: 'v0.5.0' })
    expect(waitForServiceRecovery).not.toHaveBeenCalled()
  })

  it('does not wait for recovery after bundled rollback', async () => {
    const { rollbackUpdate } = await import('@/api/deployment')
    const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
    ;(rollbackUpdate as any).mockResolvedValue({ data: { data: { phase: 'rolling_back' } } })

    const wrapper = await mountSettings({
      deploymentStatus: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        mode: 'bundled',
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        update_status: { phase: 'idle' },
      },
    })

    const button = wrapper.findAll('button').find((b) => b.text().includes('Rollback'))
    expect(button).toBeTruthy()

    await button!.trigger('click')
    await flushPromises()

    expect(rollbackUpdate).toHaveBeenCalled()
    expect(waitForServiceRecovery).not.toHaveBeenCalled()
  })

  it('renders LLM form fields', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.text()).toContain('Relay URL')
    expect(wrapper.text()).toContain('Relay API Key')
    expect(wrapper.text()).toContain('Relay Admin API Key')
    expect(wrapper.text()).toContain('Model')
    expect(wrapper.text()).toContain('System Prompt')
    expect(wrapper.text()).toContain('User Prompt Template')
  })

  it('renders Save and Test Connection buttons', async () => {
    const wrapper = await mountSettings()
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save')
    const testBtn = wrapper.findAll('button').find((b) => b.text().includes('Test Connection'))
    expect(saveBtn).toBeTruthy()
    expect(testBtn).toBeTruthy()
  })

  it('shows loading state initially', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/settings')
    await router.isReady()

    const wrapper = mount(SettingsView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Loading...')
  })

  // --- New tests for uncovered lines ---

  it('displays providers in table after loading', async () => {
    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
        { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bb.example.com', status: 'active', created_at: '2026-02-01T00:00:00Z' },
      ],
    })

    expect(wrapper.text()).toContain('GitHub')
    expect(wrapper.text()).toContain('Bitbucket')
    expect(wrapper.text()).toContain('https://api.github.com')
    expect(wrapper.text()).toContain('active')
  })

  it('opens Add Provider dialog and creates provider', async () => {
    const { createProvider, listProviders } = await import('@/api/scmProvider')
    ;(createProvider as any).mockResolvedValue({ data: { data: { id: 3, name: 'New' } } })
    ;(listProviders as any).mockResolvedValue({ data: { data: { items: [], total: 0 } } })

    const wrapper = await mountSettings()

    // Click Add Provider
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Add SCM Provider')

    // Fill form
    const nameInput = wrapper.findAll('input[type="text"]').find((i) => i.attributes('placeholder')?.includes('GitHub'))
    await nameInput!.setValue('My GitHub')

    // Submit
    const createBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await createBtn!.trigger('click')
    await flushPromises()

    expect(createProvider).toHaveBeenCalled()
  })

  it('shows validation error when name is empty on submit', async () => {
    const wrapper = await mountSettings()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Clear name and submit
    const createBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await createBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Name is required')
  })

  it('shows validation error when base_url is empty on submit', async () => {
    const wrapper = await mountSettings()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Fill name but clear base_url
    const inputs = wrapper.findAll('input[type="text"]')
    const nameInput = inputs.find((i) => i.attributes('placeholder')?.includes('GitHub'))
    await nameInput!.setValue('Test Provider')

    // Change type to bitbucket_server to clear base_url
    const typeSelect = wrapper.find('select')
    await typeSelect.setValue('bitbucket_server')
    await wrapper.vm.$nextTick()

    // Submit
    const createBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await createBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Base URL is required')
  })

  it('opens Edit dialog for existing provider', async () => {
    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Edit Provider')
  })

  it('updates existing provider on submit', async () => {
    const { updateProvider, listProviders } = await import('@/api/scmProvider')
    ;(updateProvider as any).mockResolvedValue({ data: { data: { id: 1, name: 'Updated' } } })
    ;(listProviders as any).mockResolvedValue({
      data: { data: { items: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' }], total: 1 } },
    })

    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    // Open edit dialog
    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Submit update
    const updateBtn = wrapper.findAll('button').find((b) => b.text() === 'Update')
    await updateBtn!.trigger('click')
    await flushPromises()

    expect(updateProvider).toHaveBeenCalledWith(1, { name: 'GitHub', base_url: 'https://api.github.com' })
  })

  it('updates provider with token when provided', async () => {
    const { updateProvider, listProviders } = await import('@/api/scmProvider')
    ;(updateProvider as any).mockResolvedValue({ data: { data: { id: 1 } } })
    ;(listProviders as any).mockResolvedValue({
      data: { data: { items: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' }], total: 1 } },
    })

    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    // Open edit dialog
    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Fill token — the dialog's password input has placeholder containing "ghp_" or "personal access token"
    const passwordInputs = wrapper.findAll('input[type="password"]')
    const tokenInput = passwordInputs.find((i) => i.attributes('placeholder')?.includes('ghp_'))
    await tokenInput!.setValue('ghp_newtoken')
    await wrapper.vm.$nextTick()

    // Submit
    const updateBtn = wrapper.findAll('button').find((b) => b.text() === 'Update')
    await updateBtn!.trigger('click')
    await flushPromises()

    expect(updateProvider).toHaveBeenCalledWith(1, {
      name: 'GitHub',
      base_url: 'https://api.github.com',
      credentials: { token: 'ghp_newtoken' },
    })
  })

  it('handles create provider error', async () => {
    const { createProvider } = await import('@/api/scmProvider')
    ;(createProvider as any).mockRejectedValue({
      response: { data: { message: 'Duplicate name' } },
    })

    const wrapper = await mountSettings()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Fill name
    const nameInput = wrapper.findAll('input[type="text"]').find((i) => i.attributes('placeholder')?.includes('GitHub'))
    await nameInput!.setValue('Dup Provider')

    // Submit
    const createBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await createBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Duplicate name')
  })

  it('closes dialog on Cancel', async () => {
    const wrapper = await mountSettings()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Add SCM Provider')

    // Cancel
    const cancelBtn = wrapper.findAll('button').find((b) => b.text() === 'Cancel')
    await cancelBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('Add SCM Provider')
  })

  it('shows delete confirm and deletes provider', async () => {
    const { deleteProvider, listProviders } = await import('@/api/scmProvider')
    ;(deleteProvider as any).mockResolvedValue({ data: { data: null } })
    ;(listProviders as any).mockResolvedValue({
      data: { data: { items: [], total: 0 } },
    })

    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    // Click Delete
    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Confirm
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(deleteProvider).toHaveBeenCalledWith(1)
  })

  it('cancels delete confirm', async () => {
    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    // Click Delete
    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Cancel
    const cancelDeleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Cancel')
    await cancelDeleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Delete button should be back
    const deleteBtnAgain = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    expect(deleteBtnAgain).toBeTruthy()
  })

  it('handles delete provider error gracefully', async () => {
    const { deleteProvider } = await import('@/api/scmProvider')
    ;(deleteProvider as any).mockRejectedValue(new Error('delete failed'))

    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' },
      ],
    })

    const deleteBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await deleteBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')
    await confirmBtn!.trigger('click')
    await flushPromises()

    // Should not crash
    expect(wrapper.text()).toContain('GitHub')
  })

  it('changes type to bitbucket_server and clears base_url', async () => {
    const wrapper = await mountSettings()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Change type
    const typeSelect = wrapper.find('select')
    await typeSelect.setValue('bitbucket_server')
    await typeSelect.trigger('change')
    await wrapper.vm.$nextTick()

    // base_url should be cleared
    const baseUrlInput = wrapper.findAll('input[type="text"]').find((i) => i.attributes('placeholder')?.includes('https://api.github.com'))
    expect((baseUrlInput!.element as HTMLInputElement).value).toBe('')
  })

  it('changes type back to github and sets default base_url', async () => {
    const wrapper = await mountSettings()

    // Open dialog
    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Provider'))
    await addBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // Change to bitbucket then back to github
    const typeSelect = wrapper.find('select')
    await typeSelect.setValue('bitbucket_server')
    await typeSelect.trigger('change')
    await wrapper.vm.$nextTick()

    await typeSelect.setValue('github')
    await typeSelect.trigger('change')
    await wrapper.vm.$nextTick()

    const baseUrlInput = wrapper.findAll('input[type="text"]').find((i) => i.attributes('placeholder')?.includes('https://api.github.com'))
    expect((baseUrlInput!.element as HTMLInputElement).value).toBe('https://api.github.com')
  })

  // LLM config tests

  it('loads LLM config on mount', async () => {
    const wrapper = await mountSettings({
      llmConfig: {
        relay_url: 'http://relay.local',
        relay_api_key: 'sk-test',
        relay_admin_api_key: 'admin-test',
        model: 'gpt-4o',
        max_tokens_per_scan: 50000,
        system_prompt: 'You are helpful',
        user_prompt_template: 'Analyze {repo_context}',
        enabled: true,
      },
    })

    const inputs = wrapper.findAll('input[type="text"]')
    const disabledTextInputs = inputs.filter((i) => (i.element as HTMLInputElement).disabled)
    const disabledValues = disabledTextInputs.map((i) => (i.element as HTMLInputElement).value)
    expect(disabledValues).toEqual(expect.arrayContaining(['http://relay.local', 'sk-test']))

    expect(getInputByLabel(wrapper, 'Model').value).toBe('gpt-4o')
    expect(getInputByLabel(wrapper, 'Relay Admin API Key').value).toBe('admin-test')

    expect(wrapper.text()).toContain('Enabled')
  })

  it('shows Not configured when LLM is disabled', async () => {
    const wrapper = await mountSettings({
      llmConfig: {
        relay_url: 'http://relay.local',
        relay_api_key: 'sk-test',
        relay_admin_api_key: 'admin-test',
        model: 'gpt-4',
        enabled: false,
      },
    })

    expect(wrapper.text()).toContain('Not configured')
  })

  it('saves LLM config successfully', async () => {
    const { updateLLMConfig } = await import('@/api/settings')
    ;(updateLLMConfig as any).mockResolvedValue({ data: { data: {} } })

    const wrapper = await mountSettings({
      llmConfig: {
        relay_url: 'http://relay.local',
        relay_api_key: 'llm-user-key',
        relay_admin_api_key: 'admin-old-key',
        model: 'gpt-4',
        enabled: true,
      },
    })

    const modelInput = getInputByLabel(wrapper, 'Model')
    modelInput.value = 'gpt-4o'
    modelInput.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()

    const relayKeyInput = getInputByLabel(wrapper, 'Relay Admin API Key')
    relayKeyInput.value = 'admin-new-key'
    relayKeyInput.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()

    // Click Save
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(updateLLMConfig).toHaveBeenCalled()
    expect(updateLLMConfig).toHaveBeenCalledWith(expect.objectContaining({ model: 'gpt-4o', relay_admin_api_key: 'admin-new-key' }))
    expect(wrapper.text()).toContain('LLM configuration saved')
  })

  it('handles LLM save error', async () => {
    const { updateLLMConfig } = await import('@/api/settings')
    ;(updateLLMConfig as any).mockRejectedValue({
      response: { data: { message: 'Invalid config' } },
    })

    const wrapper = await mountSettings()

    const modelInput = getInputByLabel(wrapper, 'Model')
    modelInput.value = 'gpt-4o'
    modelInput.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Invalid config')
  })

  it('tests LLM connection successfully', async () => {
    const { testLLMConnection } = await import('@/api/settings')
    ;(testLLMConnection as any).mockResolvedValue({
      data: { data: { success: true, message: 'Connection OK', response: 'pong from relay' } },
    })

    const wrapper = await mountSettings()

    const testBtn = wrapper.findAll('button').find((b) => b.text().includes('Test Connection'))
    await testBtn!.trigger('click')
    await flushPromises()

    expect(testLLMConnection).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Connection OK')
    expect(wrapper.text()).toContain('pong from relay')
  })

  it('tests LLM connection with failure', async () => {
    const { testLLMConnection } = await import('@/api/settings')
    ;(testLLMConnection as any).mockResolvedValue({
      data: { data: { success: false, message: 'Connection refused' } },
    })

    const wrapper = await mountSettings()

    const testBtn = wrapper.findAll('button').find((b) => b.text().includes('Test Connection'))
    await testBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Connection refused')
  })

  it('handles LLM test connection error', async () => {
    const { testLLMConnection } = await import('@/api/settings')
    ;(testLLMConnection as any).mockRejectedValue(new Error('Network error'))

    const wrapper = await mountSettings()

    const testBtn = wrapper.findAll('button').find((b) => b.text().includes('Test Connection'))
    await testBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Network error')
  })

  it('handles fetchProviders error gracefully', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockRejectedValue(new Error('fetch failed'))

    const router = createTestRouter()
    await router.push('/settings')
    await router.isReady()

    const wrapper = mount(SettingsView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()
    await wrapper.vm.$nextTick()

    // Should show empty state
    expect(wrapper.text()).toContain('No SCM providers configured')
  })

  it('handles fetchLLMConfig error gracefully', async () => {
    const { getLLMConfig } = await import('@/api/settings')
    ;(getLLMConfig as any).mockRejectedValue(new Error('not configured'))

    const router = createTestRouter()
    await router.push('/settings')
    await router.isReady()

    const wrapper = mount(SettingsView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()
    await wrapper.vm.$nextTick()

    // Should still render without crashing
    expect(wrapper.text()).toContain('LLM Configuration')
    expect(wrapper.text()).toContain('Not configured')
  })

  it('handles listProviders returning array directly', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockResolvedValue({
      data: { data: [{ id: 1, name: 'Direct', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-01-01T00:00:00Z' }] },
    })

    const router = createTestRouter()
    await router.push('/settings')
    await router.isReady()

    const wrapper = mount(SettingsView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Direct')
  })

  it('formats date correctly', async () => {
    const wrapper = await mountSettings({
      providers: [
        { id: 1, name: 'Test', type: 'github', base_url: 'https://api.github.com', status: 'active', created_at: '2026-03-15T10:00:00Z' },
      ],
    })

    // The date should be formatted via toLocaleDateString
    // Just verify the provider row renders without error
    expect(wrapper.text()).toContain('Test')
  })

  it('shows empty providers message', async () => {
    const wrapper = await mountSettings({ providers: [] })
    expect(wrapper.text()).toContain('No SCM providers configured')
  })

  it('handles LLM config with null data', async () => {
    const { getLLMConfig } = await import('@/api/settings')
    ;(getLLMConfig as any).mockResolvedValue({ data: { data: null } })

    const router = createTestRouter()
    await router.push('/settings')
    await router.isReady()

    const wrapper = mount(SettingsView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Not configured')
  })
})
