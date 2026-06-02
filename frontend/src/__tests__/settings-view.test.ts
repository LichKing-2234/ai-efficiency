import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import SettingsView from '@/views/SettingsView.vue'
import { setLocale } from '@/i18n'

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

async function mountSettings(overrides?: { providers?: any[]; relayProviders?: any[]; credentials?: any[]; deploymentStatus?: any }, path = '/settings') {
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
  await router.push(path)
  await router.isReady()

  const wrapper = mount(SettingsView, {
    global: { plugins: [createPinia(), router] },
  })

  await flushPromises()
  await wrapper.vm.$nextTick()

  return wrapper
}

async function openSettingsSection(wrapper: any, section: string) {
  await wrapper.get(`[data-testid="settings-tab-${section}"]`).trigger('click')
  await flushPromises()
}

describe('SettingsView', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    setLocale('en-US')
    await resetApiMocks()
  })

  it('renders code platform, relay provider, and credential sections', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.find('h1').text()).toBe('Admin Console')
    expect(wrapper.text()).toContain('AI Services')
    expect(wrapper.text()).toContain('Code Platforms')
    expect(wrapper.text()).toContain('Organization & Login')
    expect(wrapper.text()).toContain('Deployment & Runtime')
    expect(wrapper.text()).toContain('Advanced Credentials')
    expect(wrapper.text()).toContain('Add Relay Provider')

    await openSettingsSection(wrapper, 'advanced-credentials')
    expect(wrapper.text()).toContain('Credential store')
    expect(wrapper.text()).toContain('Add Credential')
  })

  it('restores and persists active settings section in the URL query', async () => {
    const wrapper = await mountSettings(undefined, '/settings?section=code-platforms')

    expect(wrapper.text()).toContain('Code Platforms')
    expect(wrapper.find('[data-testid="settings-tab-code-platforms"]').attributes('aria-selected')).toBe('true')

    await openSettingsSection(wrapper, 'deployment-runtime')

    expect((wrapper.vm as any).$route.query.section).toBe('deployment-runtime')
  })

  it('switches admin console task zones to Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = await mountSettings()

    expect(wrapper.find('h1').text()).toBe('管理后台')
    expect(wrapper.text()).toContain('AI 服务配置')
    expect(wrapper.text()).toContain('代码平台配置')
    expect(wrapper.text()).toContain('组织与登录')
    expect(wrapper.text()).toContain('部署与运行')
    expect(wrapper.text()).toContain('高级凭据')
    expect(wrapper.text()).toContain('Relay 入口')
    expect(wrapper.text()).toContain('新增 Relay Provider')

    await openSettingsSection(wrapper, 'deployment-runtime')
    expect(wrapper.text()).toContain('当前版本')
    expect(wrapper.text()).toContain('检查更新')

    await openSettingsSection(wrapper, 'ai-services')
    const addRelayBtn = wrapper.findAll('button').find((b) => b.text() === '新增 Relay Provider')
    await addRelayBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('显示名称')
    expect(wrapper.text()).toContain('管理员 API Key')
    expect(wrapper.text()).toContain('加密存储在数据库')
  })

  it('creates a secret text credential', async () => {
    const { createCredential } = await import('@/api/credential')
    const wrapper = await mountSettings()
    await openSettingsSection(wrapper, 'advanced-credentials')

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

  it('sends credential ids when creating a code platform', async () => {
    const { createProvider } = await import('@/api/scmProvider')
    const wrapper = await mountSettings({
      credentials: [
        { id: 12, name: 'GitHub PAT', description: '', kind: 'secret_text', usage_count: 0, summary: {}, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
        { id: 13, name: 'Bitbucket SSH', description: '', kind: 'ssh_username_with_private_key', usage_count: 0, summary: {}, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      ],
    })
    await openSettingsSection(wrapper, 'code-platforms')

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Platform')
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
          name: 'relay-main',
          display_name: 'Relay Main',
          base_url: 'https://relay.example.com',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
    })

    expect(wrapper.text()).toContain('Relay Main')
    expect(wrapper.text()).toContain('relay-main')
    expect(wrapper.text()).toContain('https://relay.example.com')
    expect(wrapper.text()).toContain('Primary')
    expect(wrapper.text()).toContain('Enabled')
  })

  it('does not expose relay provider testing from admin settings', async () => {
    const wrapper = await mountSettings({
      relayProviders: [
        {
          id: 1,
          name: 'relay-main',
          display_name: 'Relay Main',
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

    await wrapper.find('input[name="relay-provider-name"]').setValue('relay-main')
    await wrapper.find('input[name="relay-provider-display-name"]').setValue('Relay Main')
    await wrapper.find('input[name="relay-provider-base-url"]').setValue('https://relay.example.com')
    await wrapper.find('input[name="relay-provider-admin-api-key"]').setValue('admin-test-key')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create Relay Provider')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createRelayProvider).toHaveBeenCalledWith({
      name: 'relay-main',
      display_name: 'Relay Main',
      base_url: 'https://relay.example.com',
      admin_api_key: 'admin-test-key',
      is_primary: true,
      enabled: true,
    })
  })

  it('closes relay provider dialog with Escape', async () => {
    const wrapper = await mountSettings()
    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Relay Provider')
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('Create Relay Provider')
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
          name: 'relay-main',
          display_name: 'Relay Main',
          base_url: 'https://relay.example.com',
          admin_api_key: '***',
          is_primary: true,
          enabled: true,
        },
      ],
    })

    await wrapper.find('[data-testid="relay-provider-edit-1"]').trigger('click')
    await flushPromises()

    await wrapper.find('input[name="relay-provider-display-name"]').setValue('Relay Secondary')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Update Relay Provider')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(updateRelayProvider).toHaveBeenCalledWith(1, {
      display_name: 'Relay Secondary',
      base_url: 'https://relay.example.com',
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
          name: 'relay-main',
          display_name: 'Relay Main',
          base_url: 'https://relay.example.com',
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
    await openSettingsSection(wrapper, 'deployment-runtime')
    expect(wrapper.text()).toContain('Deployment & Runtime')
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
    await openSettingsSection(wrapper, 'deployment-runtime')
    const button = wrapper.findAll('button').find((b) => b.text().includes('Restart Service'))
    await button!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Confirm restart service')
    expect(restartDeployment).not.toHaveBeenCalled()

    const confirm = wrapper.findAll('button').find((b) => b.text().includes('Confirm restart service'))
    await confirm!.trigger('click')
    await flushPromises()

    expect(restartDeployment).toHaveBeenCalled()
    expect(waitForServiceRecovery).toHaveBeenCalled()
  })

  it('shows loading state when code platforms are still loading', async () => {
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
