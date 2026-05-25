import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import SettingsView from '@/views/SettingsView.vue'

const createDefaultProvidersResponse = () => ({
  data: {
    data: {
      items: [],
      total: 0,
    },
  },
})

const createDefaultRelayProvidersResponse = () => ({
  data: {
    data: [],
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

vi.mock('@/api/relayProvider', () => ({
  listRelayProviders: vi.fn(),
  createRelayProvider: vi.fn(),
  updateRelayProvider: vi.fn(),
  deleteRelayProvider: vi.fn(),
}))

vi.mock('@/api/credential', () => ({
  listCredentials: vi.fn(),
  createCredential: vi.fn(),
  updateCredential: vi.fn(),
  deleteCredential: vi.fn(),
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

  const relayProvider = await import('@/api/relayProvider') as any
  relayProvider.listRelayProviders.mockReset().mockResolvedValue(createDefaultRelayProvidersResponse())
  relayProvider.createRelayProvider.mockReset().mockResolvedValue({ data: { data: { id: 1 } } })
  relayProvider.updateRelayProvider.mockReset().mockResolvedValue({ data: { data: { id: 1 } } })
  relayProvider.deleteRelayProvider.mockReset().mockResolvedValue({ data: { data: null } })

  const credentialApi = await import('@/api/credential') as any
  credentialApi.listCredentials.mockReset().mockResolvedValue({
    data: {
      data: [
        {
          id: 12,
          name: 'GitHub PAT',
          description: '',
          kind: 'secret_text',
          usage_count: 0,
          summary: {},
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    },
  })
  credentialApi.createCredential.mockReset().mockResolvedValue({ data: { data: { id: 11 } } })
  credentialApi.updateCredential.mockReset().mockResolvedValue({ data: { data: { id: 11 } } })
  credentialApi.deleteCredential.mockReset().mockResolvedValue({ data: { data: null } })

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

async function mountSettings(overrides?: { providers?: any[]; relayProviders?: any[]; credentials?: any[]; deploymentStatus?: any }) {
  const { listProviders } = await import('@/api/scmProvider')
  const { listRelayProviders } = await import('@/api/relayProvider')
  const { listCredentials } = await import('@/api/credential')
  const { getDeploymentStatus } = await import('@/api/deployment')

  if (overrides?.providers) {
    ;(listProviders as any).mockResolvedValue({
      data: { data: { items: overrides.providers, total: overrides.providers.length } },
    })
  }
  if (overrides?.relayProviders) {
    ;(listRelayProviders as any).mockResolvedValue({ data: { data: overrides.relayProviders } })
  }
  if (overrides?.credentials) {
    ;(listCredentials as any).mockResolvedValue({ data: { data: overrides.credentials } })
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

  it('renders SCM providers, relay providers, and credentials sections', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.find('h1').text()).toBe('SCM Providers')
    expect(wrapper.text()).toContain('Credentials')
    expect(wrapper.text()).toContain('Relay Providers')
    expect(wrapper.text()).toContain('Add Relay Provider')
  })

  it('creates a secret text credential', async () => {
    const { createCredential } = await import('@/api/credential')
    const wrapper = await mountSettings()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Credential'))
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('input[name="credential-name"]').setValue('GitHub PAT')
    await wrapper.find('select[name="credential-kind"]').setValue('secret_text')
    await wrapper.find('textarea[name="credential-secret-text"]').setValue('ghp_test')

    const saveBtn = wrapper.findAll('button').find((b) => b.text().includes('Save Credential'))
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createCredential).toHaveBeenCalledWith({
      name: 'GitHub PAT',
      description: '',
      kind: 'secret_text',
      payload: { text: 'ghp_test' },
    })
  })

  it('sends credential ids when creating an SCM provider', async () => {
    const { createProvider } = await import('@/api/scmProvider')
    const wrapper = await mountSettings({
      credentials: [
        { id: 12, name: 'GitHub PAT', description: '', kind: 'secret_text', usage_count: 0, summary: {}, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
        { id: 13, name: 'Bitbucket SSH', description: '', kind: 'ssh_username_with_private_key', usage_count: 0, summary: {}, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      ],
    })

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Provider')
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('input[name="provider-name"]').setValue('GitHub Extensions')
    await wrapper.find('select[name="provider-api-credential"]').setValue('12')
    await wrapper.find('select[name="provider-clone-protocol"]').setValue('ssh')
    await wrapper.find('select[name="provider-clone-credential"]').setValue('13')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createProvider).toHaveBeenCalledWith({
      name: 'GitHub Extensions',
      type: 'github',
      base_url: 'https://api.github.com',
      api_credential_id: 12,
      clone_protocol: 'ssh',
      clone_credential_id: 13,
    })
  })

  it('renders relay providers returned from the backend', async () => {
    const wrapper = await mountSettings({
      relayProviders: [
        {
          id: 1,
          name: 'sub2api-main',
          display_name: 'Sub2API Main',
          base_url: 'https://sub2api.agoraio.cn',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
    })

    expect(wrapper.text()).toContain('Sub2API Main')
    expect(wrapper.text()).toContain('sub2api-main')
    expect(wrapper.text()).toContain('https://sub2api.agoraio.cn')
    expect(wrapper.text()).toContain('Primary')
    expect(wrapper.text()).toContain('Enabled')
  })

  it('does not expose relay provider testing from admin settings', async () => {
    const wrapper = await mountSettings({
      relayProviders: [
        {
          id: 1,
          name: 'sub2api-main',
          display_name: 'Sub2API Main',
          base_url: 'https://sub2api.example.com',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
    })

    expect(wrapper.find('[data-testid="relay-provider-test-1"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Test Relay Provider')
  })

  it('shows relay provider empty state', async () => {
    const wrapper = await mountSettings({ relayProviders: [] })
    expect(wrapper.text()).toContain('No relay providers configured')
  })

  it('opens relay provider dialog and creates a relay provider', async () => {
    const { createRelayProvider } = await import('@/api/relayProvider')
    const wrapper = await mountSettings()

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Relay Provider')
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('input[name="relay-provider-name"]').setValue('sub2api-main')
    await wrapper.find('input[name="relay-provider-display-name"]').setValue('Sub2API Main')
    await wrapper.find('input[name="relay-provider-base-url"]').setValue('https://sub2api.agoraio.cn')
    await wrapper.find('input[name="relay-provider-admin-api-key"]').setValue('admin-test-key')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create Relay Provider')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createRelayProvider).toHaveBeenCalledWith({
      name: 'sub2api-main',
      display_name: 'Sub2API Main',
      base_url: 'https://sub2api.agoraio.cn',
      admin_api_key: 'admin-test-key',
      is_primary: true,
      enabled: true,
    })
  })

  it('validates missing relay provider fields', async () => {
    const wrapper = await mountSettings()

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Relay Provider')
    await addBtn!.trigger('click')
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create Relay Provider')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Name is required')
  })

  it('opens edit dialog for an existing relay provider and updates it', async () => {
    const { updateRelayProvider } = await import('@/api/relayProvider')
    const wrapper = await mountSettings({
      relayProviders: [
        {
          id: 1,
          name: 'sub2api-main',
          display_name: 'Sub2API Main',
          base_url: 'https://sub2api.agoraio.cn',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
    })

    await wrapper.find('[data-testid="relay-provider-edit-1"]').trigger('click')
    await flushPromises()

    await wrapper.find('input[name="relay-provider-display-name"]').setValue('Sub2API Secondary')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Update Relay Provider')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(updateRelayProvider).toHaveBeenCalledWith(1, {
      display_name: 'Sub2API Secondary',
      base_url: 'https://sub2api.agoraio.cn',
      admin_api_key: undefined,
      is_primary: true,
      enabled: true,
    })
  })

  it('deletes a relay provider after confirmation', async () => {
    const { deleteRelayProvider } = await import('@/api/relayProvider')
    const wrapper = await mountSettings({
      relayProviders: [
        {
          id: 1,
          name: 'sub2api-main',
          display_name: 'Sub2API Main',
          base_url: 'https://sub2api.agoraio.cn',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
    })

    await wrapper.find('[data-testid="relay-provider-delete-1"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="relay-provider-confirm-delete-1"]').trigger('click')
    await flushPromises()

    expect(deleteRelayProvider).toHaveBeenCalledWith(1)
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

  it('calls restart deployment when restart control is clicked', async () => {
    const { restartDeployment } = await import('@/api/deployment')
    const { waitForServiceRecovery } = await import('@/utils/deploymentRecovery')
    ;(restartDeployment as any).mockResolvedValue({ data: { data: { phase: 'restart_requested' } } })

    const wrapper = await mountSettings()
    const button = wrapper.findAll('button').find((b) => b.text().includes('Restart Service'))
    await button!.trigger('click')
    await flushPromises()

    expect(restartDeployment).toHaveBeenCalled()
    expect(waitForServiceRecovery).toHaveBeenCalled()
  })

  it('shows loading state when SCM providers are still loading', async () => {
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
})
