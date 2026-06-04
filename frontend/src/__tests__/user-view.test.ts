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
    expect(wrapper.text()).toContain('Set up AI tools and code repositories')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('Production')
    expect(wrapper.text()).toContain('Windows PowerShell')
    expect(wrapper.text()).toContain('install.ps1')
    expect(wrapper.text()).toContain('Advanced command reference')
    expect(wrapper.text()).toContain('Manual backfill / recovery')
    expect(wrapper.text()).toContain('discover --provider prod')
    expect(wrapper.text()).toContain('ae-cli hooks enable --global')
    expect(wrapper.text()).toContain('ae-cli init')
    expect(wrapper.text()).toContain('ae-cli doctor')
    expect(wrapper.text()).toContain('ae-cli sync')
    expect(wrapper.text()).toContain('ae-cli hooks status --uploads')
    expect(wrapper.text()).not.toContain('Paste ae-cli version output')
    expect(wrapper.text()).not.toContain('Paste ae-cli discover --dry-run output')
    expect(wrapper.text()).not.toContain('Paste ae-cli doctor output')
    expect(wrapper.text()).not.toContain('Review')
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
    expect(wrapper.text()).toContain('Advanced command reference')
    expect(wrapper.text()).not.toContain('Profile Summary')
    expect(wrapper.text()).not.toContain('Provider & Group Credential')
    expect(wrapper.text()).not.toContain('Credential state')
    expect(wrapper.text()).not.toContain('Current Secret')
  })

  it('gives actionable blocked-setup guidance instead of vague developer escalation', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).toContain('When setup is blocked')
    expect(wrapper.text()).toContain('Ask an admin')
    expect(wrapper.text()).toContain('Share diagnosis output')
    expect(wrapper.text()).toContain('Run ae-cli doctor in the affected repository')
    expect(wrapper.text()).not.toContain('Ask a developer')
    expect(wrapper.text()).not.toContain('repo init, hooks, or doctor output looks wrong')
  })

  it('renders a task-first setup flow and copies command blocks', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).toContain('Setup progress')
    expect(wrapper.text()).toContain("I'm a developer")
    expect(wrapper.text()).toContain("I'm not a developer")
    expect(wrapper.text()).toContain('Account verified')
    expect(wrapper.text()).toContain('Confirm AI access')
    expect(wrapper.text()).toContain('Install the CLI')
    expect(wrapper.text()).toContain('Check GitHub connectivity')
    expect(wrapper.text()).toContain('HTTPS_PROXY')
    expect(wrapper.text()).toContain('Configure local AI tools')
    expect(wrapper.text()).toContain('Enable automatic Git reporting')
    expect(wrapper.text()).toContain('Run setup diagnosis')
    expect(wrapper.text()).toContain('When setup is blocked')

    const copyButton = wrapper.findAll('button').find((button) => button.text() === 'Copy command')
    await copyButton!.trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expect.stringContaining('curl -fsSL'))
    expect(wrapper.text()).toContain('Copied')
  })

  it('lets non-developers switch to manual local configuration without ae-cli commands in the progress flow', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="setup-audience-non-developer"]').trigger('click')

    const progressText = wrapper.get('[data-testid="setup-progress"]').text()
    expect(progressText).toContain('Manual local configuration')
    expect(progressText).toContain('ae-cli is not required')
    expect(progressText).toContain('https://prod.example.com')
    expect(progressText).toContain('anthropic')
    expect(progressText).toContain('~/.claude/settings.json')
    expect(progressText).toContain('ANTHROPIC_BASE_URL')
    expect(progressText).toContain('ANTHROPIC_AUTH_TOKEN')
    expect(progressText).toContain('CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC')
    expect(progressText).toContain('CLAUDE_CODE_ATTRIBUTION_HEADER')
    expect(progressText).not.toContain('sk-existing-claude-123456')
    expect(progressText).not.toContain('ae-cli login')
    expect(progressText).not.toContain('ae-cli discover')
    expect(progressText).not.toContain('ae-cli hooks enable --global')
    expect(progressText).not.toContain('ae-cli init')
    expect(progressText).not.toContain('ae-cli doctor')
  })

  it('copies a complete non-developer manual snippet only after secret confirmation', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="setup-audience-non-developer"]').trigger('click')
    await wrapper.get('[data-testid="manual-config-copy-claude-settings"]').trigger('click')

    expect(wrapper.text()).toContain('Confirm copy configuration with API key')
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="confirm-manual-config-copy"]').trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expect.stringContaining('"ANTHROPIC_AUTH_TOKEN": "sk-existing-claude-123456"'))
  })

  it('renders Codex manual configuration for non-developer OpenAI groups', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="setup-audience-non-developer"]').trigger('click')
    await wrapper.get('[data-testid="group-44"]').trigger('click')
    await flushPromises()

    const progressText = wrapper.get('[data-testid="setup-progress"]').text()
    expect(progressText).toContain('~/.codex/config.toml')
    expect(progressText).toContain('model_provider = "prod"')
    expect(progressText).toContain('model = "gpt-5.4"')
    expect(progressText).toContain('model_reasoning_effort = "xhigh"')
    expect(progressText).toContain('disable_response_storage = true')
    expect(progressText).toContain('network_access = "enabled"')
    expect(progressText).toContain('[model_providers.prod]')
    expect(progressText).toContain('base_url = "https://prod.example.com"')
    expect(progressText).toContain('wire_api = "responses"')
    expect(progressText).toContain('requires_openai_auth = true')
    expect(progressText).toContain('~/.codex/auth.json')
    expect(progressText).toContain('OPENAI_API_KEY')
    expect(progressText).not.toContain('sk-existing-openai-123456')

    const codexConfigBlock = wrapper.findAll('pre').find((block) => block.text().includes('model_provider = "prod"'))
    expect(codexConfigBlock?.text()).toContain('model_auto_compact_token_limit = 900000\n\n[model_providers.prod]')
  })

  it('renders Gemini manual configuration and reload guidance for non-developer Gemini groups', async () => {
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="setup-audience-non-developer"]').trigger('click')
    await wrapper.get('[data-testid="group-45"]').trigger('click')
    await flushPromises()

    const progressText = wrapper.get('[data-testid="setup-progress"]').text()
    expect(progressText).toContain('~/.ae-cli/env.sh')
    expect(progressText).toContain('export GEMINI_API_KEY=')
    expect(progressText).toContain('export GOOGLE_GEMINI_BASE_URL="https://prod.example.com"')
    expect(progressText).toContain('case "${SHELL##*/}" in')
    expect(progressText).toContain('zsh) rc_file="$HOME/.zshrc" ;;')
    expect(progressText).toContain('bash) rc_file="$HOME/.bashrc" ;;')
    expect(progressText).toContain('*) rc_file="$HOME/.profile" ;;')
    expect(progressText).toContain('[ -f "$rc_file" ] && source "$rc_file"')
    expect(progressText).not.toContain('source "$HOME/.zshrc"source "$HOME/.bashrc"')
    expect(progressText).toContain('export GEMINI_MODEL="gemini-3.1-pro-preview"')
    expect(progressText).toContain('Do not manually switch models inside Gemini')
    expect(progressText).not.toContain('sk-existing-gemini-123456')
  })

  it('marks local-only setup steps as requiring a local check instead of pretending they are numbered progress', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).toContain('Needs local check')
    expect(wrapper.text()).toContain('Run this command on your machine to complete or verify this step.')
    expect(wrapper.text()).not.toContain('3Install the CLI')
  })

  it('recommends default CLI login in setup progress and leaves device login as a fallback reference', async () => {
    const { wrapper } = await mountUserView()

    const text = wrapper.text()
    const loginStepStart = text.indexOf('Sign in from the CLI')
    const configureStepStart = text.indexOf('Configure local AI tools')
    const loginStepText = text.slice(loginStepStart, configureStepStart)

    expect(loginStepText).toContain('ae-cli login')
    expect(loginStepText).not.toContain('ae-cli login --device')
    expect(text).toContain('ae-cli login --device')
  })

  it('keeps primary setup commands out of the advanced command reference duplicates', async () => {
    const { wrapper } = await mountUserView()

    const commandBlocks = wrapper.findAll('pre').map((block) => block.text())

    expect(commandBlocks.filter((command) => command === 'ae-cli discover --provider prod')).toHaveLength(1)
    expect(commandBlocks.filter((command) => command === 'ae-cli hooks enable --global')).toHaveLength(1)
    expect(commandBlocks.filter((command) => command === 'ae-cli init')).toHaveLength(1)
    expect(commandBlocks.filter((command) => command === 'ae-cli doctor')).toHaveLength(1)
    expect(commandBlocks).toContain('ae-cli login --device')
    expect(commandBlocks).toContain('ae-cli sync')
    expect(commandBlocks).toContain('ae-cli hooks status --uploads')
    expect(wrapper.text()).not.toContain('Machine Setup')
    expect(wrapper.text()).not.toContain('Per-Repo Setup')
  })

  it('switches providers and updates the discover command and group list', async () => {
    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="provider-1"]').trigger('click')
    expect(wrapper.text()).toContain('discover --provider staging')
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
