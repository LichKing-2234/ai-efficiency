import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ElDialog } from 'element-plus'
import SettingsView from '@/views/SettingsView.vue'
import { setLocale } from '@/i18n'
import { cleanupTeleportedContent, withTeleportedContent } from './helpers/teleport'

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

const createDefaultSystemVersionResponse = () => ({
  data: {
    data: {
      version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
      check_enabled: true,
      latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
      update_available: false,
    },
  },
})

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
  const matchMedia = vi.fn((_query: string) => mediaQuery)
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: matchMedia,
  })

  return {
    matchMedia,
    addEventListener,
    removeEventListener,
    change(matches: boolean) {
      mediaQuery.matches = matches
      for (const listener of Array.from(listeners)) {
        listener({ matches, media: mediaQuery.media })
      }
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

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

vi.mock('@/api/system', () => ({
  getSystemVersion: vi.fn(),
  checkSystemUpdate: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn(),
    post: vi.fn(),
  },
}))

vi.mock('@/api/directory', () => ({
  listDirectorySources: vi.fn(),
  listDirectoryDepartments: vi.fn(),
  createDirectorySource: vi.fn(),
  updateDirectorySource: vi.fn(),
  validateDirectorySource: vi.fn(),
  listDirectoryRuns: vi.fn(),
  previewDirectorySource: vi.fn(),
  startDirectoryRun: vi.fn(),
  getDirectoryRun: vi.fn(),
}))

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetApproverConfigs: vi.fn(),
  listQuotaResetApproverCandidates: vi.fn(),
  saveQuotaResetApproverConfigs: vi.fn(),
  getQuotaResetNotificationSettings: vi.fn(),
  updateQuotaResetNotificationSettings: vi.fn(),
  testQuotaResetNotificationSettings: vi.fn(),
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

  const systemApi = await import('@/api/system') as any
  systemApi.getSystemVersion.mockReset().mockResolvedValue(createDefaultSystemVersionResponse())
  systemApi.checkSystemUpdate.mockReset().mockResolvedValue({
    data: {
      data: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        check_enabled: true,
        checked: true,
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
      },
    },
  })

  const directoryApi = await import('@/api/directory') as any
  directoryApi.listDirectorySources.mockReset().mockResolvedValue({ data: { data: { items: [] } } })
  directoryApi.listDirectoryDepartments.mockReset().mockResolvedValue({ data: { data: { items: [] } } })
  directoryApi.createDirectorySource.mockReset().mockResolvedValue({ data: { data: { id: 1 } } })
  directoryApi.updateDirectorySource.mockReset().mockResolvedValue({ data: { data: { id: 1 } } })
  directoryApi.validateDirectorySource.mockReset().mockResolvedValue({ data: { data: { valid: true, issues: [] } } })
  directoryApi.listDirectoryRuns.mockReset().mockResolvedValue({
    data: {
      data: {
        items: [],
        total: 0,
        page: 0,
        page_size: 20,
        latest_active_run: null,
      },
    },
  })
  directoryApi.previewDirectorySource.mockReset().mockResolvedValue({ data: { data: { id: 1, status: 'completed' } } })
  directoryApi.startDirectoryRun.mockReset().mockResolvedValue({ data: { data: { id: 2, status: 'completed' } } })
  directoryApi.getDirectoryRun.mockReset().mockResolvedValue({ data: { data: null } })

  const client = (await import('@/api/client')).default as any
  client.get.mockReset().mockResolvedValue({
    data: { data: { url: '', base_dn: '', bind_dn: '', user_filter: '', tls: false } },
  })
  client.put.mockReset().mockResolvedValue({ data: { data: {} } })
  client.post.mockReset().mockResolvedValue({ data: { data: {} } })

  const quotaResetApi = await import('@/api/quotaReset') as any
  quotaResetApi.getQuotaResetApproverConfigs.mockReset().mockResolvedValue({ data: { data: { items: [] } } })
  quotaResetApi.listQuotaResetApproverCandidates.mockReset().mockResolvedValue({ data: { data: { items: [], unmatched_representatives: [] } } })
  quotaResetApi.saveQuotaResetApproverConfigs.mockReset().mockResolvedValue({ data: { data: { items: [] } } })
  quotaResetApi.getQuotaResetNotificationSettings.mockReset().mockResolvedValue({ data: { data: { enabled: false, url: '', auth_type: 'none' } } })
  quotaResetApi.updateQuotaResetNotificationSettings.mockReset().mockResolvedValue({ data: { data: { enabled: false, url: '', auth_type: 'none' } } })
  quotaResetApi.testQuotaResetNotificationSettings.mockReset().mockResolvedValue({ data: { data: { message: 'ok' } } })

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

async function mountSettings(overrides?: { providers?: any[]; relayProviders?: any[]; credentials?: any[]; systemVersion?: any }, path = '/settings') {
  const { listProviders } = await import('@/api/scmProvider')
  const { listRelayProviders } = await import('@/api/relayProvider')
  const { listCredentials } = await import('@/api/credential')
  const { getSystemVersion } = await import('@/api/system')

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
  if (overrides?.systemVersion) {
    ;(getSystemVersion as any).mockResolvedValue({ data: { data: overrides.systemVersion } })
  }

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = withTeleportedContent(mount(SettingsView, {
    global: { plugins: [createPinia(), router] },
  }))

  await vi.dynamicImportSettled()
  await flushPromises()
  await wrapper.vm.$nextTick()

  return wrapper
}

async function openSettingsSection(wrapper: any, section: string) {
  const tab = wrapper.find(`[data-testid="settings-tab-${section}"]`)
  if (tab.exists()) {
    await tab.trigger('click')
  } else {
    const labels: Record<string, string> = {
      'ai-services': 'AI Services',
      'code-platforms': 'Code Platforms',
      'organization-login': 'Organization & Login',
      'deployment-runtime': 'Deployment & Runtime',
      'advanced-credentials': 'Advanced Credentials',
    }
    await selectElementPlusOption(wrapper, 'settings-section-select', labels[section])
  }
  await vi.dynamicImportSettled()
  await flushPromises()
}

async function selectElementPlusOption(wrapper: any, testId: string, label: string) {
  await wrapper.get(`[data-testid="${testId}"] .el-select__wrapper`).trigger('click')
  await flushPromises()
  const teleportedOptions = Array.from(document.body.querySelectorAll<HTMLElement>('.el-select-dropdown__item'))
    .filter((item) => item.textContent === label)
  const option = wrapper.findAll('.el-select-dropdown__item').find((item: any) => item.text() === label)
    ?? teleportedOptions[teleportedOptions.length - 1]
  if (!option) throw new Error(`Element Plus option ${label} was not rendered`)
  if ('trigger' in option) {
    await option.trigger('click')
  } else {
    option.click()
  }
  await flushPromises()
}

describe('SettingsView', () => {
  beforeEach(async () => {
    cleanupTeleportedContent()
    installMatchMedia(true)
    setActivePinia(createPinia())
    setLocale('en-US')
    await resetApiMocks()
  })

  it('loads only Relay providers for the default section', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    const { listRelayProviders } = await import('@/api/relayProvider')
    const { listCredentials } = await import('@/api/credential')
    const { getSystemVersion } = await import('@/api/system')
    const { listDirectorySources } = await import('@/api/directory')
    const { getQuotaResetApproverConfigs } = await import('@/api/quotaReset')
    const client = (await import('@/api/client')).default as any

    await mountSettings()

    expect(listRelayProviders).toHaveBeenCalledTimes(1)
    expect(listProviders).not.toHaveBeenCalled()
    expect(listCredentials).not.toHaveBeenCalled()
    expect(getSystemVersion).not.toHaveBeenCalled()
    expect(listDirectorySources).not.toHaveBeenCalled()
    expect(getQuotaResetApproverConfigs).not.toHaveBeenCalled()
    expect(client.get).not.toHaveBeenCalledWith('/admin/settings/ldap')
  })

  it('falls back to AI Services for prototype-key section queries', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    const { listRelayProviders } = await import('@/api/relayProvider')

    const wrapper = await mountSettings(undefined, '/settings?section=constructor')

    expect(wrapper.get('[data-testid="settings-tab-ai-services"]').element.closest('[role="tab"]')?.getAttribute('aria-selected')).toBe('true')
    expect(wrapper.text()).toContain('Add Service Endpoint')
    expect(wrapper.text()).not.toContain('Relay Provider')
    expect(wrapper.text()).not.toContain('DB-backed relay')
    expect(listRelayProviders).toHaveBeenCalledTimes(1)
    expect(listProviders).not.toHaveBeenCalled()
  })

  it('loads only the directly linked section and its owned requests', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    const { listRelayProviders } = await import('@/api/relayProvider')
    const { listCredentials } = await import('@/api/credential')
    const { getSystemVersion } = await import('@/api/system')
    const { listDirectorySources } = await import('@/api/directory')
    const { getQuotaResetApproverConfigs, getQuotaResetNotificationSettings } = await import('@/api/quotaReset')
    const client = (await import('@/api/client')).default as any

    await mountSettings(undefined, '/settings?section=code-platforms')
    expect(listProviders).toHaveBeenCalledTimes(1)
    expect(listCredentials).not.toHaveBeenCalled()
    expect(listRelayProviders).not.toHaveBeenCalled()
    expect(getSystemVersion).not.toHaveBeenCalled()

    await resetApiMocks()
    await mountSettings(undefined, '/settings?section=deployment-runtime')
    expect(getSystemVersion).toHaveBeenCalledTimes(1)
    expect(listProviders).not.toHaveBeenCalled()
    expect(listRelayProviders).not.toHaveBeenCalled()
    expect(listCredentials).not.toHaveBeenCalled()

    await resetApiMocks()
    await mountSettings(undefined, '/settings?section=advanced-credentials')
    expect(listCredentials).toHaveBeenCalledTimes(1)
    expect(listProviders).not.toHaveBeenCalled()
    expect(listRelayProviders).not.toHaveBeenCalled()
    expect(getSystemVersion).not.toHaveBeenCalled()

    await resetApiMocks()
    await mountSettings(undefined, '/settings?section=organization-login')
    expect(client.get).toHaveBeenCalledWith('/admin/settings/ldap')
    expect(listCredentials).toHaveBeenCalledTimes(1)
    expect(listDirectorySources).toHaveBeenCalledTimes(1)
    expect(getQuotaResetApproverConfigs).toHaveBeenCalledTimes(1)
    expect(getQuotaResetNotificationSettings).toHaveBeenCalledTimes(1)
    expect(listProviders).not.toHaveBeenCalled()
    expect(listRelayProviders).not.toHaveBeenCalled()
    expect(getSystemVersion).not.toHaveBeenCalled()
  })

  it('loads code platform credentials only when its add dialog opens', async () => {
    const { listCredentials } = await import('@/api/credential')
    const wrapper = await mountSettings(undefined, '/settings?section=code-platforms')

    expect(listCredentials).not.toHaveBeenCalled()
    const addBtn = wrapper.findAll('button').find((button) => button.text() === 'Add Platform')
    await addBtn!.trigger('click')
    await flushPromises()

    expect(listCredentials).toHaveBeenCalledTimes(1)
  })

  it('reuses credentials across Advanced and Organization sections', async () => {
    const { listCredentials } = await import('@/api/credential')
    const wrapper = await mountSettings(undefined, '/settings?section=advanced-credentials')
    expect(listCredentials).toHaveBeenCalledTimes(1)

    await openSettingsSection(wrapper, 'organization-login')
    expect(listCredentials).toHaveBeenCalledTimes(1)
  })

  it('reuses directory sources after the Organization section remounts', async () => {
    const { listDirectorySources } = await import('@/api/directory')
    const wrapper = await mountSettings(undefined, '/settings?section=organization-login')
    expect(listDirectorySources).toHaveBeenCalledTimes(1)

    await openSettingsSection(wrapper, 'ai-services')
    await openSettingsSection(wrapper, 'organization-login')
    expect(listDirectorySources).toHaveBeenCalledTimes(1)
  })

  it('renders code platform, relay provider, and credential sections', async () => {
    const wrapper = await mountSettings()
    expect(wrapper.find('h1').text()).toBe('Admin Console')
    expect(wrapper.text()).toContain('AI Services')
    expect(wrapper.text()).toContain('Code Platforms')
    expect(wrapper.text()).toContain('Organization & Login')
    expect(wrapper.text()).toContain('Deployment & Runtime')
    expect(wrapper.text()).toContain('Advanced Credentials')
    expect(wrapper.text()).toContain('Add Service Endpoint')

    await openSettingsSection(wrapper, 'advanced-credentials')
    expect(wrapper.text()).toContain('Credential store')
    expect(wrapper.text()).toContain('Add Credential')
  })

  it('renders compact label-only settings tabs on wide screens', async () => {
    const wrapper = await mountSettings()
    const tabs = wrapper.getComponent({ name: 'ElTabs' })

    expect(tabs.props('stretch')).toBe(false)
    expect(wrapper.findAllComponents({ name: 'ElTabPane' })).toHaveLength(5)
    expect(wrapper.get('[data-testid="settings-tab-ai-services"]').text()).toBe('AI Services')
    expect(wrapper.get('[data-testid="settings-tab-code-platforms"]').text()).toBe('Code Platforms')
    expect(wrapper.get('[data-testid="settings-tab-advanced-credentials"]').text()).toBe('Advanced Credentials')
  })

  it('uses a mobile section selector without mounting the desktop tabs', async () => {
    const media = installMatchMedia(false)
    const wrapper = await mountSettings()

    expect(wrapper.find('.el-tabs').exists()).toBe(false)
    expect(wrapper.get('[data-testid="settings-section-select"]').classes()).toContain('el-select')
    expect(media.matchMedia).toHaveBeenCalledWith('(min-width: 1280px)')

    await selectElementPlusOption(wrapper, 'settings-section-select', 'Code Platforms')
    await vi.dynamicImportSettled()
    await flushPromises()

    expect((wrapper.vm as any).$route.query.section).toBe('code-platforms')
    expect(wrapper.find('#settings-panel-ai-services').exists()).toBe(false)
    expect(wrapper.get('#settings-panel-code-platforms').text()).toContain('Add Platform')
  })

  it('mounts only the active responsive list for all provider and credential sections', async () => {
    const media = installMatchMedia(true)
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
      providers: [
        {
          id: 7,
          name: 'GitHub',
          type: 'github',
          base_url: 'https://api.github.com',
          ssh_host: 'github.com',
          status: 'active',
          created_at: '2026-01-01T00:00:00Z',
        },
      ],
    })

    const assertDesktopOnly = (panel: any) => {
      expect(panel.find('table').exists()).toBe(true)
      expect(panel.find('article').exists()).toBe(false)
    }
    const assertMobileOnly = (panel: any) => {
      expect(panel.find('table').exists()).toBe(false)
      expect(panel.find('article').exists()).toBe(true)
    }

    assertDesktopOnly(wrapper.get('#settings-panel-ai-services'))
    await openSettingsSection(wrapper, 'code-platforms')
    assertDesktopOnly(wrapper.get('#settings-panel-code-platforms'))
    await openSettingsSection(wrapper, 'advanced-credentials')
    assertDesktopOnly(wrapper.get('#settings-panel-advanced-credentials'))

    media.change(false)
    await wrapper.vm.$nextTick()
    assertMobileOnly(wrapper.get('#settings-panel-advanced-credentials'))
    await openSettingsSection(wrapper, 'code-platforms')
    assertMobileOnly(wrapper.get('#settings-panel-code-platforms'))
    await openSettingsSection(wrapper, 'ai-services')
    assertMobileOnly(wrapper.get('#settings-panel-ai-services'))

    expect(media.matchMedia).toHaveBeenCalledWith('(min-width: 1280px)')
    expect(media.matchMedia.mock.calls.filter(([query]) => query === '(min-width: 768px)')).toHaveLength(1)
    expect(media.addEventListener).toHaveBeenCalled()
    wrapper.unmount()
    expect(media.removeEventListener).toHaveBeenCalled()
  })

  it('renders operator-facing code platform and credential metadata', async () => {
    const platforms = await mountSettings({
      providers: [{
        id: 7,
        name: 'Bitbucket',
        type: 'bitbucket_server',
        base_url: 'https://bitbucket.example.com',
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
      }],
    }, '/settings?section=code-platforms')

    expect(platforms.text()).toContain('Bitbucket Server')
    expect(platforms.text()).toContain('Active')
    expect(platforms.text()).not.toContain('bitbucket_server')

    const credentials = await mountSettings({
      credentials: [
        {
          id: 12,
          name: 'GitHub PAT',
          description: '',
          kind: 'secret_text',
          usage_count: 1,
          summary: { preview: 'gh...gy' },
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 13,
          name: 'Deploy account',
          description: '',
          kind: 'username_password',
          usage_count: 1,
          summary: { username: 'alice', password_preview: 'te****rd' },
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 14,
          name: 'Deploy key',
          description: '',
          kind: 'ssh_username_with_private_key',
          usage_count: 1,
          summary: { username: 'git', private_key_preview: 'configured', has_passphrase: true },
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
    }, '/settings?section=advanced-credentials')

    expect(credentials.text()).toContain('Secret text')
    expect(credentials.text()).toContain('gh...gy')
    expect(credentials.text()).toContain('Username: alice')
    expect(credentials.text()).toContain('Password: te****rd')
    expect(credentials.text()).toContain('Username: git')
    expect(credentials.text()).toContain('Private key configured')
    expect(credentials.text()).toContain('Passphrase configured')
    expect(credentials.text()).not.toContain('secret_text')
    expect(credentials.text()).not.toContain('{"preview"')
  })

  it('uses Element Plus tables for every populated desktop settings list', async () => {
    const wrapper = await mountSettings({
      relayProviders: [{
        id: 1,
        name: 'relay-main',
        display_name: 'Primary service',
        base_url: 'https://relay.example.com',
        admin_api_key: '***',
        is_primary: true,
        enabled: true,
      }],
      providers: [{
        id: 7,
        name: 'Bitbucket',
        type: 'bitbucket_server',
        base_url: 'https://bitbucket.example.com',
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
      }],
      credentials: [{
        id: 12,
        name: 'GitHub PAT',
        description: '',
        kind: 'secret_text',
        usage_count: 1,
        summary: { preview: 'gh...gy' },
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      }],
    })

    expect(wrapper.get('#settings-panel-ai-services').findComponent({ name: 'ElTable' }).exists()).toBe(true)

    await openSettingsSection(wrapper, 'code-platforms')
    expect(wrapper.get('#settings-panel-code-platforms').findComponent({ name: 'ElTable' }).exists()).toBe(true)

    await openSettingsSection(wrapper, 'advanced-credentials')
    expect(wrapper.get('#settings-panel-advanced-credentials').findComponent({ name: 'ElTable' }).exists()).toBe(true)
  })

  it('renders directory sync inside organization login settings', async () => {
    const wrapper = await mountSettings()

    await openSettingsSection(wrapper, 'organization-login')

    expect(wrapper.text()).toContain('Quota Reset Approval')
    expect(wrapper.text()).toContain('Directory Sync')
    expect(wrapper.text()).toContain('Departments then members')
    expect(wrapper.text()).toContain('Copy AI Prompt')
  })

  it('renders the LDAP save action with Element Plus', async () => {
    const wrapper = await mountSettings()
    await openSettingsSection(wrapper, 'organization-login')

    const saveButton = wrapper.findAll('button').find((button) => button.text() === 'Save')
    expect(saveButton?.classes()).toContain('el-button')
  })

  it('restores and persists active settings section in the URL query', async () => {
    const wrapper = await mountSettings(undefined, '/settings?section=code-platforms')

    expect(wrapper.text()).toContain('Code Platforms')
    expect(wrapper.get('[data-testid="settings-tab-code-platforms"]').element.closest('[role="tab"]')?.getAttribute('aria-selected')).toBe('true')

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
    expect(wrapper.text()).toContain('AI 服务入口')
    expect(wrapper.text()).toContain('新增服务入口')
    expect(wrapper.text()).not.toContain('DB relay')
    expect(wrapper.text()).not.toContain('Relay Provider')

    await openSettingsSection(wrapper, 'deployment-runtime')
    expect(wrapper.text()).toContain('当前版本')
    expect(wrapper.text()).toContain('检查更新')

    await openSettingsSection(wrapper, 'ai-services')
    const addRelayBtn = wrapper.findAll('button').find((b) => b.text() === '新增服务入口')
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

  it('deletes a stored credential through Element Plus confirmation', async () => {
    const { deleteCredential } = await import('@/api/credential')
    const wrapper = await mountSettings(undefined, '/settings?section=advanced-credentials')

    await wrapper.get('[data-testid="credential-delete-12"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-popconfirm').exists()).toBe(true)
    await wrapper.get('[data-testid="credential-confirm-delete-12"]').trigger('click')
    await flushPromises()
    expect(deleteCredential).toHaveBeenCalledWith(12)
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
    await selectElementPlusOption(wrapper, 'provider-api-credential', 'GitHub PAT (Secret text)')
    await selectElementPlusOption(wrapper, 'provider-clone-protocol', 'ssh')
    await selectElementPlusOption(wrapper, 'provider-clone-credential', 'Bitbucket SSH')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createProvider).toHaveBeenCalledWith({
      name: 'GitHub Extensions',
      type: 'github',
      base_url: 'https://api.github.com',
      ssh_host: 'github.com',
      api_credential_id: 12,
      clone_protocol: 'ssh',
      clone_credential_id: 13,
    })
  })

  it('defaults GitHub code platform ssh host to github.com', async () => {
    const { createProvider } = await import('@/api/scmProvider')
    const wrapper = await mountSettings()
    await openSettingsSection(wrapper, 'code-platforms')

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Platform')
    await addBtn!.trigger('click')
    await flushPromises()

    const sshHostInput = wrapper.find('input[name="provider-ssh-host"]')
    expect((sshHostInput.element as HTMLInputElement).value).toBe('github.com')

    await wrapper.find('input[name="provider-name"]').setValue('GitHub')
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createProvider).toHaveBeenCalledWith(expect.objectContaining({
      type: 'github',
      base_url: 'https://api.github.com',
      ssh_host: 'github.com',
    }))
  })

  it('sends ssh host when creating a code platform', async () => {
    const { createProvider } = await import('@/api/scmProvider')
    const wrapper = await mountSettings()
    await openSettingsSection(wrapper, 'code-platforms')

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Platform')
    await addBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('input[name="provider-name"]').setValue('Bitbucket')
    await wrapper.find('input[placeholder="https://api.github.com"]').setValue('https://bitbucket-api.example.com')
    await wrapper.find('input[name="provider-ssh-host"]').setValue('git.example.com')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(createProvider).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Bitbucket',
      base_url: 'https://bitbucket-api.example.com',
      ssh_host: 'git.example.com',
    }))
  })

  it('deletes a code platform through Element Plus confirmation', async () => {
    const { deleteProvider } = await import('@/api/scmProvider')
    const wrapper = await mountSettings({
      providers: [
        {
          id: 7,
          name: 'GitHub',
          type: 'github',
          base_url: 'https://api.github.com',
          ssh_host: 'github.com',
          status: 'active',
          created_at: '2026-01-01T00:00:00Z',
        },
      ],
    }, '/settings?section=code-platforms')

    await wrapper.get('[data-testid="provider-delete-7"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-popconfirm').exists()).toBe(true)
    await wrapper.get('[data-testid="provider-confirm-delete-7"]').trigger('click')
    await flushPromises()
    expect(deleteProvider).toHaveBeenCalledWith(7)
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
    expect(wrapper.text()).toContain('No AI service endpoints configured')
  })

  it('renders the Relay provider action with Element Plus', async () => {
    const wrapper = await mountSettings()

    const addButton = wrapper.findAll('button').find((button) => button.text() === 'Add Service Endpoint')
    expect(addButton?.classes()).toContain('el-button')
  })

  it('opens relay provider dialog and creates a relay provider', async () => {
    const { createRelayProvider } = await import('@/api/relayProvider')
    const wrapper = await mountSettings()

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Service Endpoint')
    await addBtn!.trigger('click')
    await flushPromises()

    const apiKeyControl = wrapper.findAllComponents({ name: 'ElInput' })
      .find((component) => component.find('input[name="relay-provider-admin-api-key"]').exists())
    expect(apiKeyControl).toBeDefined()
    expect(apiKeyControl!.props('showPassword')).not.toBe(true)

    await wrapper.find('input[name="relay-provider-name"]').setValue('relay-main')
    await wrapper.find('input[name="relay-provider-display-name"]').setValue('Relay Main')
    await wrapper.find('input[name="relay-provider-base-url"]').setValue('https://relay.example.com')
    await wrapper.find('input[name="relay-provider-admin-api-key"]').setValue('admin-test-key')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create Service Endpoint')
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
    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Service Endpoint')
    await addBtn!.trigger('click')
    await flushPromises()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true }))
    await flushPromises()

    expect(wrapper.get('[data-testid="relay-provider-dialog"]').isVisible()).toBe(false)
  })

  it('validates missing relay provider fields', async () => {
    const wrapper = await mountSettings()

    const addBtn = wrapper.findAll('button').find((b) => b.text() === 'Add Service Endpoint')
    await addBtn!.trigger('click')
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Create Service Endpoint')
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

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Update Service Endpoint')
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

    await wrapper.get('[data-testid="relay-provider-delete-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-popconfirm').exists()).toBe(true)
    await wrapper.get('[data-testid="relay-provider-confirm-delete-1"]').trigger('click')
    await flushPromises()

    expect(deleteRelayProvider).toHaveBeenCalledWith(1)
  })

  it('prevents duplicate relay deletes while confirmation is in flight', async () => {
    const { deleteRelayProvider } = await import('@/api/relayProvider')
    const pendingDelete = deferred<{ data: { data: null } }>()
    ;(deleteRelayProvider as any).mockReturnValueOnce(pendingDelete.promise)
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

    await wrapper.get('[data-testid="relay-provider-delete-1"]').trigger('click')
    await flushPromises()
    const confirmButton = wrapper.get('[data-testid="relay-provider-confirm-delete-1"]')
    await confirmButton.trigger('click')
    await wrapper.vm.$nextTick()

    expect(deleteRelayProvider).toHaveBeenCalledTimes(1)
    expect(confirmButton.attributes('disabled')).toBeDefined()
    expect(confirmButton.classes()).toContain('is-loading')
    await confirmButton.trigger('click')
    expect(deleteRelayProvider).toHaveBeenCalledTimes(1)

    pendingDelete.resolve({ data: { data: null } })
    await flushPromises()
  })

  it('renders system version and update check without binary upgrade controls', async () => {
    const { checkSystemUpdate } = await import('@/api/system')
    const wrapper = await mountSettings()
    await openSettingsSection(wrapper, 'deployment-runtime')
    expect(wrapper.text()).toContain('Deployment & Runtime')
    expect(wrapper.text()).toContain('v0.4.0')
    expect(wrapper.text()).toContain('v0.5.0')
    expect(wrapper.get('[data-testid="deployment-update-check-status"]').text()).toContain('Update checks enabled')
    expect(wrapper.text()).toContain('Check Updates')
    expect(wrapper.text()).not.toContain('Apply Update')
    expect(wrapper.text()).not.toContain('Rollback')
    expect(wrapper.text()).not.toContain('Restart Service')

    const checkButton = wrapper.findAll('button').find((b) => b.text() === 'Check Updates')
    await checkButton!.trigger('click')
    await flushPromises()

    expect(checkSystemUpdate).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Update available')
  })

  it('replaces empty settings tables with actionable Element Plus empty states', async () => {
    const ai = await mountSettings({ relayProviders: [] }, '/settings?section=ai-services')
    expect(ai.get('[data-testid="settings-empty-ai-services"]').classes()).toContain('el-empty')
    expect(ai.find('table').exists()).toBe(false)
    await ai.get('[data-testid="settings-empty-add-relay"]').trigger('click')
    const aiDialog = ai.findComponent(ElDialog)
    expect(ai.get('[data-testid="relay-provider-dialog"]').isVisible()).toBe(true)
    expect(aiDialog.props('appendToBody')).toBe(true)

    const platforms = await mountSettings({ providers: [] }, '/settings?section=code-platforms')
    expect(platforms.get('[data-testid="settings-empty-code-platforms"]').classes()).toContain('el-empty')
    expect(platforms.find('table').exists()).toBe(false)
    await platforms.get('[data-testid="settings-empty-add-platform"]').trigger('click')
    await flushPromises()
    const platformDialog = platforms.findComponent(ElDialog)
    expect(platforms.get('[data-testid="code-platform-dialog"]').isVisible()).toBe(true)
    expect(platformDialog.props('appendToBody')).toBe(true)

    const credentials = await mountSettings({ credentials: [] }, '/settings?section=advanced-credentials')
    expect(credentials.get('[data-testid="settings-empty-credentials"]').classes()).toContain('el-empty')
    expect(credentials.find('table').exists()).toBe(false)
    await credentials.get('[data-testid="settings-empty-add-credential"]').trigger('click')
    await flushPromises()
    const credentialDialog = credentials.findComponent(ElDialog)
    expect(credentials.get('[data-testid="credential-dialog"]').isVisible()).toBe(true)
    expect(credentialDialog.props('appendToBody')).toBe(true)
  })

  it('shows version check unavailable when latest-release checks are disabled', async () => {
    const { checkSystemUpdate } = await import('@/api/system')
    const wrapper = await mountSettings({
      systemVersion: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        check_enabled: false,
        update_available: false,
      },
    })
    await openSettingsSection(wrapper, 'deployment-runtime')

    expect(wrapper.text()).toContain('Version check unavailable')
    const checkButton = wrapper.findAll('button').find((b) => b.text() === 'Check Updates')
    expect(checkButton!.attributes('disabled')).toBeDefined()

    await checkButton!.trigger('click')
    await flushPromises()

    expect(checkSystemUpdate).not.toHaveBeenCalled()
  })

  it('shows check errors instead of already current for non-comparable versions', async () => {
    const { checkSystemUpdate } = await import('@/api/system')
    ;(checkSystemUpdate as any).mockResolvedValue({
      data: {
        data: {
          version: { version: 'dev', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
          check_enabled: true,
          checked: true,
          check_error: 'current version is not semver',
          update_available: false,
          latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        },
      },
    })
    const wrapper = await mountSettings({
      systemVersion: {
        version: { version: 'dev', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        check_enabled: true,
        update_available: false,
      },
    })
    await openSettingsSection(wrapper, 'deployment-runtime')

    const checkButton = wrapper.findAll('button').find((b) => b.text() === 'Check Updates')
    await checkButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('current version is not semver')
    expect(wrapper.text()).not.toContain('Already current')
  })

  it('shows loading state when code platforms are still loading', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/settings?section=code-platforms')
    await router.isReady()

    const wrapper = mount(SettingsView, {
      global: { plugins: [createPinia(), router] },
    })

    await vi.dynamicImportSettled()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Loading...')
  })
})
