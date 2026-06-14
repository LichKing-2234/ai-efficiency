import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import UserView from '@/views/UserView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
  createGroupCredential: vi.fn(),
  regenerateGroupCredential: vi.fn(),
  getUserProviderModels: vi.fn(),
  testUserProvider: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/user', component: UserView },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

async function mountUserView() {
  const { getUserProviders, getUserProviderModels } = await import('@/api/user')
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 1,
            name: 'staging',
            display_name: 'Staging',
            base_url: 'https://staging.example.com',
            default_model: 'claude-sonnet',
            is_primary: false,
            groups: [
              {
                group_id: '42',
                group_name: 'OpenAI-Staging',
                platform: 'openai',
                credential: { state: 'missing' },
              },
            ],
          },
          {
            id: 2,
            name: 'prod',
            display_name: 'Production',
            base_url: 'https://prod.example.com',
            default_model: 'claude-sonnet',
            is_primary: true,
            groups: [
              {
                group_id: '43',
                group_name: 'Group Beta',
                platform: 'anthropic',
                credential: { state: 'existing_hidden', api_key_id: 22, name: 'alice', status: 'active', key: 'sk-existing-claude-123456' },
              },
              {
                group_id: '42',
                group_name: 'Group Alpha',
                platform: 'openai',
                credential: { state: 'missing' },
              },
              {
                group_id: '44',
                group_name: 'Group Gamma',
                platform: 'openai',
                credential: { state: 'existing_hidden', api_key_id: 23, name: 'alice', status: 'active', key: 'sk-existing-openai-123456' },
              },
              {
                group_id: '45',
                group_name: 'Group Delta',
                platform: 'gemini',
                credential: { state: 'existing_hidden', api_key_id: 24, name: 'alice', status: 'active', key: 'sk-existing-gemini-123456' },
              },
            ],
          },
        ],
        message: '',
      },
    },
  })
  ;(getUserProviderModels as any).mockImplementation((_providerId: number, _groupId: string, platform: string) => {
    const models = platform === 'gemini'
      ? [
          { id: 'gemini-3.1-pro-preview', display_name: 'Gemini 3.1 Pro Preview' },
        ]
      : platform === 'openai'
      ? [
          { id: 'gpt-5.4', display_name: 'GPT-5.4' },
          { id: 'gpt-5.5', display_name: 'GPT-5.5' },
        ]
      : [
          { id: 'claude-sonnet-4-6', display_name: 'Claude Sonnet 4.6' },
          { id: 'claude-opus-4-6', display_name: 'Claude Opus 4.6' },
        ]
    return Promise.resolve({ data: { data: { models } } })
  })

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'sso' }

  const router = createTestRouter()
  await router.push('/user')
  await router.isReady()

  const wrapper = mount(UserView, {
    global: {
      plugins: [pinia, router],
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
      },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('UserView', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.stubGlobal('confirm', vi.fn(() => true))
  })

  it('loads profile and provider data, selects primary provider by default, and renders group info', async () => {
    const { wrapper } = await mountUserView()
    expect(wrapper.text()).toContain('My Setup')
    expect(wrapper.text()).toContain('Choose an access group, create your API key')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('Production')
    expect(wrapper.text()).not.toContain('Advanced command reference')
    expect(wrapper.text()).not.toContain('Manual backfill / recovery')
    expect(wrapper.text()).not.toContain('ae-cli sync')
    expect(wrapper.text()).not.toContain('ae-cli hooks status --uploads')
    expect(wrapper.text()).toContain('Create or manage my API key')
    expect(wrapper.text()).not.toContain("I'm a developer")
    expect(wrapper.text()).not.toContain("I'm not a developer")
    expect(wrapper.text()).toContain('Group Beta')
    expect(wrapper.text()).toContain('Platform: anthropic')
    expect(wrapper.text()).not.toContain('default_model')
  })

  it('uses user-facing setup labels instead of raw credential labels', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).toContain('Your account')
    expect(wrapper.text()).toContain('AI access')
    expect(wrapper.text()).toContain('Access group')
    expect(wrapper.text()).toContain('Ready to use')
    expect(wrapper.text()).toContain('API key and connection test')
    expect(wrapper.text()).not.toContain('Profile Summary')
    expect(wrapper.text()).not.toContain('Provider & Group Credential')
    expect(wrapper.text()).not.toContain('Credential state')
    expect(wrapper.text()).not.toContain('Current Secret')
  })

  it('shows create my api key as the primary action when the selected group has no key', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="group-42"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="primary-onboarding-action"]').text()).toBe('Create my API key')
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain("I'm a developer")
    expect(wrapper.text()).not.toContain("I'm not a developer")
  })

  it('reveals configuration methods as soon as a key is available', async () => {
    const { createGroupCredential, testUserProvider } = await import('@/api/user')
    ;(createGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-openai' } },
    })
    ;(testUserProvider as any).mockResolvedValue({
      data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="group-42"]').trigger('click')
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="configuration-methods"]').text()).toContain('Manual configuration')
    expect(wrapper.get('[data-testid="configuration-methods"]').text()).toContain('Automatic configuration')
    expect(wrapper.get('[data-testid="configuration-methods"]').text()).toContain('CC Switch configuration')

    await wrapper.get('[data-testid="user-provider-test-model"]').setValue('gpt-5.4')
    await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Connection successful')
  })

  it('clears the successful test state when switching groups or regenerating the key', async () => {
    const { testUserProvider, regenerateGroupCredential } = await import('@/api/user')
    ;(testUserProvider as any).mockResolvedValue({
      data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
    })
    ;(regenerateGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 99, name: 'alice', status: 'active', secret: 'sk-regenerated' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="user-provider-test-model"]').setValue('claude-sonnet-4-6')
    await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Connection successful')

    await wrapper.get('[data-testid="group-44"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Connection successful')

    await wrapper.get('[data-testid="group-43"]').trigger('click')
    await wrapper.get('[data-testid="regenerate-key"]').trigger('click')
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Connection successful')
  })

  it('shows only the matching CC Switch import target for the selected group platform', async () => {
    const { wrapper } = await mountUserView()

    const methods = wrapper.get('[data-testid="configuration-methods"]').text()
    expect(methods).toContain('CC Switch configuration')
    expect(methods).not.toContain('Import to Claude')
    expect(methods).not.toContain('Import to Codex')
    expect(methods).not.toContain('Import to Gemini')

    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const ccswitchPanel = wrapper.text()
    expect(ccswitchPanel).toContain('Import to Claude')
    expect(ccswitchPanel).not.toContain('Import to Codex')
    expect(ccswitchPanel).not.toContain('Import to Gemini')
    expect(ccswitchPanel).toContain('Download CC Switch')
  })

  it('passes the selected Claude model in the CC Switch import link', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const claudeImport = wrapper.get('[data-testid="ccswitch-import-claude"]')
    expect(claudeImport.attributes('href')).toContain('app=claude')
    expect(claudeImport.attributes('href')).toContain('model=claude-sonnet-4-6')
  })

  it('passes an explicit Codex model in the OpenAI CC Switch import link', async () => {
    const { createGroupCredential } = await import('@/api/user')
    ;(createGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-openai' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="group-42"]').trigger('click')
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const codexImport = wrapper.get('[data-testid="ccswitch-import-codex"]')
    expect(codexImport.attributes('href')).toContain('app=codex')
    expect(codexImport.attributes('href')).toContain('model=gpt-5.4')
  })

  it('passes the selected Gemini model in the CC Switch import link', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="group-45"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const geminiImport = wrapper.get('[data-testid="ccswitch-import-gemini"]')
    expect(geminiImport.attributes('href')).toContain('app=gemini')
    expect(geminiImport.attributes('href')).toContain('model=gemini-3.1-pro-preview')
  })

  it('shows advanced command reference only inside automatic configuration', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).not.toContain('Advanced command reference')
    await wrapper.get('[data-testid="config-method-automatic"]').trigger('click')
    expect(wrapper.text()).toContain('Advanced command reference')
    expect(wrapper.text()).toContain('ae-cli discover --provider prod')
    expect(wrapper.text()).toContain('ae-cli hooks enable --global')
    expect(wrapper.text()).toContain('ae-cli init')
    expect(wrapper.text()).toContain('ae-cli doctor')
  })

  it('shows audience guidance on each configuration method card', async () => {
    const { wrapper } = await mountUserView()

    const methods = wrapper.get('[data-testid="configuration-methods"]').text()
    expect(methods).toContain('Best for non-developers, independent agents')
    expect(methods).toContain('Best for engineering teams')
    expect(methods).toContain('Best for non-developers who want a managed desktop configuration flow')
  })

  it('switches providers and updates the discover command and group list', async () => {
    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="provider-1"]').trigger('click')
    expect(wrapper.text()).toContain('https://staging.example.com')
    expect(wrapper.text()).toContain('Create or manage my API key')
    expect(wrapper.text()).toContain('OpenAI-Staging')
  })

  it('calls createGroupCredential for the selected provider and group', async () => {
    const { createGroupCredential, getUserProviderModels } = await import('@/api/user')
    ;(createGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-new' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="group-42"]').trigger('click')
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()

    expect(createGroupCredential).toHaveBeenCalledWith(2, '42')
    expect(getUserProviderModels).toHaveBeenCalledWith(2, '42', 'openai')
  })

  it('disables create key while the request is in flight', async () => {
    const { createGroupCredential } = await import('@/api/user')
    let resolveCreate: (value: any) => void = () => {}
    ;(createGroupCredential as any).mockImplementation(() => new Promise((resolve) => {
      resolveCreate = resolve
    }))

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="group-42"]').trigger('click')

    const button = wrapper.get('[data-testid="create-key"]')
    await button.trigger('click')
    await flushPromises()

    expect(createGroupCredential).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="create-key"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="create-key"]').trigger('click')
    expect(createGroupCredential).toHaveBeenCalledTimes(1)

    resolveCreate({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-new' } },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="create-key"]').exists()).toBe(false)
  })

  it('retains separate in-memory secrets per provider and group', async () => {
    const { createGroupCredential, regenerateGroupCredential } = await import('@/api/user')
    ;(createGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-openai' } },
    })
    ;(regenerateGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 8, name: 'alice', status: 'active', secret: 'sk-claude' } },
    })

    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="group-42"]').trigger('click')
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="reveal-key"]').trigger('click')
    expect(wrapper.text()).toContain('Confirm reveal API key')
    expect(wrapper.text()).not.toContain('sk-openai')
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    expect(wrapper.text()).toContain('sk-openai')

    await wrapper.get('[data-testid="group-43"]').trigger('click')
    await wrapper.get('[data-testid="regenerate-key"]').trigger('click')
    expect(wrapper.text()).toContain('Confirm regenerate API key')
    expect(regenerateGroupCredential).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('sk-claude')

    await wrapper.get('[data-testid="group-42"]').trigger('click')
    expect(wrapper.text()).toContain('sk-openai')
    expect(wrapper.text()).not.toContain('sk-claude')
  })

  it('shows an existing key partially and copies the full key', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).toContain('sk-exi...3456')
    expect(wrapper.text()).not.toContain('sk-existing-claude-123456')

    await wrapper.get('[data-testid="copy-key"]').trigger('click')
    expect(wrapper.text()).toContain('Confirm copy API key')
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('sk-existing-claude-123456')
  })

  it('tests the selected provider with the current group platform', async () => {
    const { testUserProvider } = await import('@/api/user')
    ;(testUserProvider as any).mockResolvedValue({
      data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="user-provider-test-model"]').setValue('claude-sonnet-4-6')
    await wrapper.get('[data-testid="user-provider-test-prompt"]').setValue('Say hello')
    await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
    await flushPromises()

    expect(testUserProvider).toHaveBeenCalledWith(2, {
      platform: 'anthropic',
      group_id: '43',
      model: 'claude-sonnet-4-6',
      prompt: 'Say hello',
    })
    expect(wrapper.text()).toContain('Connection successful')
    expect(wrapper.text()).toContain('pong')
  })

  it('loads model choices for the selected group platform', async () => {
    const { getUserProviderModels } = await import('@/api/user')
    const { wrapper } = await mountUserView()

    const modelSelect = wrapper.get('[data-testid="user-provider-test-model"]')
    expect(modelSelect.element.tagName).toBe('SELECT')
    expect(modelSelect.findAll('option').map((option) => option.text()).some((text) => text.includes('Claude Sonnet 4.6'))).toBe(true)
    expect((modelSelect.element as HTMLSelectElement).value).toBe('claude-sonnet-4-6')
    expect(getUserProviderModels).toHaveBeenCalledWith(2, '43', 'anthropic')

    await wrapper.get('[data-testid="group-44"]').trigger('click')
    await flushPromises()

    expect(getUserProviderModels).toHaveBeenCalledWith(2, '44', 'openai')
    const refreshedSelect = wrapper.get('[data-testid="user-provider-test-model"]')
    expect(refreshedSelect.findAll('option').map((option) => option.text()).some((text) => text.includes('GPT-5.4'))).toBe(true)
    expect((refreshedSelect.element as HTMLSelectElement).value).toBe('gpt-5.4')
  })

  it('disables provider test when the selected group has no API key', async () => {
    const { testUserProvider } = await import('@/api/user')
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="group-42"]').trigger('click')
    await wrapper.get('[data-testid="user-provider-test-model"]').setValue('gpt-5.4')

    const runButton = wrapper.get('[data-testid="user-provider-test-run"]')
    expect(runButton.attributes('disabled')).toBeDefined()

    await runButton.trigger('click')
    expect(testUserProvider).not.toHaveBeenCalled()
  })
})
