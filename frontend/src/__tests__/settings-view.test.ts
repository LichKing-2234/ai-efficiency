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
  testRelayProvider: vi.fn(),
}))

vi.mock('@/api/credential', () => ({
  listCredentials: vi.fn(),
  createCredential: vi.fn(),
  updateCredential: vi.fn(),
  deleteCredential: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
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
  relayProvider.testRelayProvider.mockReset().mockResolvedValue({ data: { data: { success: true, message: 'Connection successful', response: 'pong' } } })

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

  const userApi = await import('@/api/user') as any
  userApi.getUserProviders.mockReset().mockResolvedValue({
    data: {
      data: {
        providers: [],
      },
    },
  })

  const authApi = await import('@/api/auth') as any
  authApi.login.mockReset().mockResolvedValue({ data: { data: null } })
  authApi.getMe.mockReset().mockResolvedValue({ data: { data: {} } })
  authApi.devLogin.mockReset().mockResolvedValue({ data: { data: null } })
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

async function mountSettings(overrides?: { providers?: any[]; relayProviders?: any[]; userProviders?: any[]; credentials?: any[] }) {
  const { listProviders } = await import('@/api/scmProvider')
  const { listRelayProviders } = await import('@/api/relayProvider')
  const { getUserProviders } = await import('@/api/user')
  const { listCredentials } = await import('@/api/credential')

  if (overrides?.providers) {
    ;(listProviders as any).mockResolvedValue({
      data: { data: { items: overrides.providers, total: overrides.providers.length } },
    })
  }
  if (overrides?.relayProviders) {
    ;(listRelayProviders as any).mockResolvedValue({ data: { data: overrides.relayProviders } })
  }
  if (overrides?.userProviders) {
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: overrides.userProviders } } })
  }
  if (overrides?.credentials) {
    ;(listCredentials as any).mockResolvedValue({ data: { data: overrides.credentials } })
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
          admin_url: 'https://sub2api.agoraio.cn',
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
    await wrapper.find('input[name="relay-provider-admin-url"]').setValue('https://sub2api.agoraio.cn')
    await wrapper.find('input[name="relay-provider-admin-api-key"]').setValue('admin-test-key')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create Relay Provider')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createRelayProvider).toHaveBeenCalledWith({
      name: 'sub2api-main',
      display_name: 'Sub2API Main',
      base_url: 'https://sub2api.agoraio.cn',
      admin_url: 'https://sub2api.agoraio.cn',
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
          admin_url: 'https://sub2api.agoraio.cn',
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
      admin_url: 'https://sub2api.agoraio.cn',
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
          admin_url: 'https://sub2api.agoraio.cn',
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

  it('tests a relay provider with a custom prompt', async () => {
    const { testRelayProvider } = await import('@/api/relayProvider')
    const wrapper = await mountSettings({
      relayProviders: [
        {
          id: 1,
          name: 'sub2api-main',
          display_name: 'Sub2API Main',
          base_url: 'https://sub2api.agoraio.cn',
          admin_url: 'https://sub2api.agoraio.cn',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
      userProviders: [
        {
          id: 1,
          name: 'sub2api-main',
          display_name: 'Sub2API Main',
          base_url: 'https://sub2api.agoraio.cn',
          default_model: 'gpt-5.4',
          is_primary: true,
          groups: [
            {
              group_id: '42',
              group_name: 'Group Alpha',
              platform: 'openai',
              credential: { state: 'existing_hidden', api_key_id: 1, name: 'alice', status: 'active' },
            },
          ],
        },
      ],
    })

    await wrapper.find('[data-testid="relay-provider-test-1"]').trigger('click')
    await flushPromises()

    const selects = wrapper.findAll('select')
    await selects[selects.length - 1].setValue('openai')
    await wrapper.find('input[placeholder="gpt-5.4"]').setValue('gpt-5.4')
    await wrapper.find('input[placeholder="Hi"]').setValue('Say hello from relay provider test')

    const runTestBtn = wrapper.findAll('button').find((b) => b.text() === 'Run Test')
    await runTestBtn!.trigger('click')
    await flushPromises()

    expect(testRelayProvider).toHaveBeenCalledWith(1, { platform: 'openai', model: 'gpt-5.4', prompt: 'Say hello from relay provider test' })
    expect(wrapper.text()).toContain('Connection successful')
    expect(wrapper.text()).toContain('pong')
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
