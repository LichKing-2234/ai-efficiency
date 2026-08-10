import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import UserView from '@/views/UserView.vue'
import { setLocale } from '@/i18n'

const messageSuccess = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as any)
const messageError = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as any)

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
  createGroupCredential: vi.fn(),
  regenerateGroupCredential: vi.fn(),
  getUserProviderModels: vi.fn(),
  testUserProvider: vi.fn(),
}))

vi.mock('@/api/attribution', () => ({
  getReportingReadiness: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

function decodeCCSwitchConfig(link: string) {
  const url = new URL(link)
  const config = url.searchParams.get('config')
  if (!config) {
    throw new Error('missing config parameter')
  }
  return JSON.parse(Buffer.from(config, 'base64').toString('utf8'))
}

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
  const providers = [
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
        {
          group_id: '46',
          group_name: 'Agentopenai',
          platform: 'openai',
          credential: { state: 'existing_hidden', api_key_id: 25, name: 'alice', status: 'active', key: 'sk-existing-agent-openai-123456' },
        },
        {
          group_id: '47',
          group_name: 'Agentanthropic',
          platform: 'anthropic',
          credential: { state: 'existing_hidden', api_key_id: 26, name: 'alice', status: 'active', key: 'sk-existing-agent-anthropic-123456' },
        },
        {
          group_id: '48',
          group_name: 'Agentgemini',
          platform: 'gemini',
          credential: { state: 'existing_hidden', api_key_id: 27, name: 'alice', status: 'active', key: 'sk-existing-agent-gemini-123456' },
        },
      ],
    },
  ]
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers,
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
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'relay_sso' }

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

async function mountUserViewWithProviders(providers: any[]) {
  const { getUserProviders, getUserProviderModels } = await import('@/api/user')
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers,
        message: '',
      },
    },
  })
  ;(getUserProviderModels as any).mockResolvedValue({ data: { data: { models: [] } } })

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'relay_sso' }

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

async function selectProviderModel(wrapper: any, label: string, value = label) {
  const select = wrapper.get('[data-testid="user-provider-test-model"]')
  if (select.element.tagName === 'INPUT') {
    await select.setValue(value)
    return
  }
  expect(select.text()).toContain(label)
  const component = wrapper.findAllComponents({ name: 'ElSelect' })
    .find((candidate: any) => candidate.attributes('data-testid') === 'user-provider-test-model')
  expect(component).toBeTruthy()
  component!.vm.$emit('update:modelValue', value)
  component!.vm.$emit('change', value)
  await flushPromises()
}

async function openOnboardingStep(wrapper: any, step: 0 | 1 | 2) {
  await wrapper.get(`[data-testid="onboarding-step-button-${step}"]`).trigger('click')
  await flushPromises()
}

async function selectAccessGroup(wrapper: any, groupID: string) {
  await openOnboardingStep(wrapper, 0)
  await wrapper.get(`[data-testid="group-${groupID}"]`).trigger('click')
  await flushPromises()
}

async function openConfigurationMethods(wrapper: any) {
  await openOnboardingStep(wrapper, 2)
  return wrapper.get('[data-testid="configuration-methods"]')
}

describe('UserView', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.stubGlobal('confirm', vi.fn(() => true))
  })

  it('separates AI tool configuration from Codex Activity reporting', async () => {
    const attributionApi = await import('@/api/attribution')
    vi.mocked(attributionApi.getReportingReadiness).mockResolvedValue({
      data: {
        data: {
          state: 'not_enrolled',
          installation_count: 0,
          enabled_installation_count: 0,
        },
      },
    } as any)

    const { wrapper } = await mountUserView()
    expect(wrapper.text()).toContain('AI tool configuration')
    expect(wrapper.text()).toContain('AI Coding Activity reporting')
    const reporting = wrapper.get('[data-testid="reporting-readiness-guide"]')
    expect(reporting.text()).toContain('Reporting is activated automatically when you sign in or run discover')
    expect(reporting.text()).toContain('Install CLI')
    expect(reporting.text()).toContain('ae-cli login')
    expect(reporting.text()).toContain('ae-cli discover')

    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-automatic"]').trigger('click')
    expect(wrapper.text()).toContain('ae-cli discover --provider prod')
    expect(wrapper.text()).not.toContain('ae-cli attribution enable')
    expect(wrapper.text()).not.toContain('ae-cli hooks enable --global')
    expect(wrapper.text()).not.toContain('ae-cli init')
    expect(wrapper.text()).not.toContain('ae-cli sync')
    expect(wrapper.text()).not.toContain('ae-cli hooks status --uploads')

    const diagnostics = reporting.get('[data-testid="reporting-diagnostics"]')
    expect(diagnostics.text()).toContain('ae-cli attribution status')
    expect(diagnostics.text()).toContain('ae-cli doctor')
  })

  it('loads profile and provider data, selects primary provider by default, and renders group info', async () => {
    const { wrapper } = await mountUserView()
    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    expect(refresh?.classes()).toContain('el-button')
    expect(wrapper.text()).toContain('AI tool configuration')
    expect(wrapper.text()).toContain('Choose an access group, create an API key')
    expect(wrapper.text()).toContain('Finish setup in 3 steps')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('Production')
    expect(wrapper.text()).not.toContain('Advanced command reference')
    expect(wrapper.text()).not.toContain('Manual backfill / recovery')
    expect(wrapper.text()).not.toContain('ae-cli sync')
    expect(wrapper.text()).not.toContain('ae-cli hooks status --uploads')
    expect(wrapper.text()).not.toContain('Create or manage my API key')
    expect(wrapper.text()).not.toContain("I'm a developer")
    expect(wrapper.text()).not.toContain("I'm not a developer")
    await openOnboardingStep(wrapper, 0)
    expect(wrapper.text()).toContain('Group Beta')
    expect(wrapper.text()).toContain('Platform: anthropic')
    expect(wrapper.text()).not.toContain('default_model')
  })

  it('keeps Element Plus step titles and lets the step icon switch the viewed step', async () => {
    const { wrapper } = await mountUserView()

    const stepButtons = [0, 1, 2].map((step) => wrapper.get(`[data-testid="onboarding-step-button-${step}"]`))
    expect(stepButtons.every((button) => button.classes().includes('el-button'))).toBe(true)
    expect(stepButtons.map((button) => button.text())).toEqual([
      '1. Choose an access group',
      '2. Create API key',
      '3. Configure tools',
    ])

    await wrapper.get('[data-testid="onboarding-step-trigger-0"] .el-step__icon').trigger('click')
    expect(wrapper.find('[data-testid="onboarding-step-0"]').exists()).toBe(true)

    await wrapper.get('[data-testid="onboarding-step-trigger-2"] .el-step__icon').trigger('click')
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(true)
  })

  it('uses bordered Element Plus radio options for access-provider selection', async () => {
    const { wrapper } = await mountUserView()

    const providerOptions = wrapper.findAllComponents({ name: 'ElRadio' })
      .filter((component) => component.attributes('data-testid')?.startsWith('provider-'))
    expect(providerOptions).toHaveLength(2)
    expect(providerOptions.every((component) => component.props('border') === true)).toBe(true)
  })

  it('renders access groups as radio choices without primary-command styling', async () => {
    const { wrapper } = await mountUserView()
    await openOnboardingStep(wrapper, 0)

    const groupOptions = wrapper.findAllComponents({ name: 'ElRadioButton' })
      .filter((component) => component.attributes('data-testid')?.startsWith('group-'))
    expect(groupOptions).toHaveLength(7)

    const accessGroup = wrapper.findAllComponents({ name: 'ElRadioGroup' })
      .find((component) => component.props('fill') === 'var(--el-color-primary-light-9)')
    expect(accessGroup?.props('textColor')).toBe('var(--el-color-primary)')

    const selectedIndicator = wrapper.get('[data-testid="group-indicator-43"]')
    const alternateIndicator = wrapper.get('[data-testid="group-indicator-42"]')
    expect(selectedIndicator.attributes('data-selected')).toBe('true')
    expect(selectedIndicator.find('.bg-gray-900').exists()).toBe(true)
    expect(alternateIndicator.attributes('data-selected')).toBe('false')
    expect(alternateIndicator.find('.bg-gray-900').exists()).toBe(false)
    expect(alternateIndicator.classes()).toEqual(
      expect.arrayContaining(['h-5', 'w-5', 'rounded-full', 'border']),
    )
  })

  it('keeps the summary and primary action stacked until the wide-content breakpoint', async () => {
    const { wrapper } = await mountUserView()
    await selectAccessGroup(wrapper, '42')

    const pageGrid = wrapper.get('[data-testid="user-page-grid"]')
    const summaryColumn = wrapper.get('[data-testid="user-summary-column"]')
    const onboardingColumn = wrapper.get('[data-testid="user-onboarding-column"]')
    const stepHeader = wrapper.get('[data-testid="onboarding-step-header"]')
    const primaryAction = wrapper.get('[data-testid="primary-onboarding-action"]')

    expect(pageGrid.classes()).toContain('xl:grid-cols-[320px_minmax(0,1fr)]')
    expect(pageGrid.classes()).not.toContain('lg:grid-cols-[320px_minmax(0,1fr)]')
    expect(summaryColumn.classes()).toContain('xl:order-1')
    expect(onboardingColumn.classes()).toContain('xl:order-2')
    expect(stepHeader.classes()).toContain('xl:flex-row')
    expect(primaryAction.classes()).toContain('w-full')
    expect(primaryAction.classes()).toContain('xl:w-auto')
  })

  it('derives the initial step direction from content width without ResizeObserver', async () => {
    const originalGetComputedStyle = window.getComputedStyle
    const boundingBoxSpy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockImplementation(function (this: HTMLElement) {
        const width = this.dataset.testid === 'primary-onboarding-flow' ? 720 : 0
        return {
          x: 0,
          y: 0,
          width,
          height: 0,
          top: 0,
          right: width,
          bottom: 0,
          left: 0,
          toJSON: () => ({}),
        }
      })
    const computedStyleSpy = vi.spyOn(window, 'getComputedStyle')
      .mockImplementation((element: Element, pseudoElement?: string | null) => {
        if ((element as HTMLElement).dataset.testid === 'primary-onboarding-flow') {
          return {
            paddingLeft: '20px',
            paddingRight: '20px',
            borderLeftWidth: '1px',
            borderRightWidth: '1px',
          } as CSSStyleDeclaration
        }
        return originalGetComputedStyle(element, pseudoElement)
      })

    try {
      const { wrapper } = await mountUserView()

      expect(wrapper.get('[data-testid="onboarding-steps"]').attributes('data-direction')).toBe('vertical')
    } finally {
      boundingBoxSpy.mockRestore()
      computedStyleSpy.mockRestore()
    }
  })

  it('uses bordered Element Plus radio options for configuration-method selection', async () => {
    const { wrapper } = await mountUserView()
    await openConfigurationMethods(wrapper)
    const manualMethod = wrapper.findAllComponents({ name: 'ElRadio' })
      .find((component) => component.attributes('data-testid') === 'config-method-manual')

    expect(manualMethod).toBeDefined()
    expect(manualMethod!.props('border')).toBe(true)
    await manualMethod!.trigger('click')
    expect(wrapper.text()).toContain('Manual configuration values')
  })

  it('keeps configuration methods equal-height and recommends CC Switch by default', async () => {
    const { wrapper } = await mountUserView()
    await openConfigurationMethods(wrapper)

    const methodGroup = wrapper.findAllComponents({ name: 'ElRadioGroup' })
      .find((component) => component.classes().includes('lg:grid-cols-3'))
    const methodOptions = wrapper.findAllComponents({ name: 'ElRadio' })
      .filter((component) => component.attributes('data-testid')?.startsWith('config-method-'))

    expect(methodGroup).toBeDefined()
    expect(methodGroup!.classes()).toContain('!items-stretch')
    expect(methodGroup!.props('modelValue')).toBe('ccswitch')
    expect(methodOptions).toHaveLength(3)
    expect(methodOptions.every((component) => component.classes().includes('!h-full'))).toBe(true)
    expect(wrapper.get('[data-testid="config-method-recommended"]').text()).toBe('Recommended')
  })

  it('uses user-facing setup labels instead of raw credential labels', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).toContain('Your account')
    expect(wrapper.text()).toContain('Relay SSO')
    expect(wrapper.text()).toContain('AI access')
    expect(wrapper.text()).toContain('1. Choose an access group')
    expect(wrapper.text()).toContain('Ready to use')
    expect(wrapper.text()).toContain('2. Create API key')
    expect(wrapper.text()).toContain('Connection test')
    expect(wrapper.text()).toContain('3. Configure tools')
    expect(wrapper.text()).not.toContain('Profile Summary')
    expect(wrapper.text()).not.toContain('Provider & Group Credential')
    expect(wrapper.text()).not.toContain('Credential state')
    expect(wrapper.text()).not.toContain('Current Secret')
    expect(wrapper.text()).not.toContain('relay_sso')
    expect(wrapper.text()).not.toContain('relay response')
  })

  it('shows create my api key as the primary action when the selected group has no key', async () => {
    const { wrapper } = await mountUserView()

    await selectAccessGroup(wrapper, '42')

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
    await selectAccessGroup(wrapper, '42')
    await wrapper.get('[data-testid="primary-onboarding-action"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="user-provider-test-run"]').text()).toBe('Run connection test')
    const methods = await openConfigurationMethods(wrapper)
    expect(methods.text()).toContain('Manual configuration')
    expect(methods.text()).toContain('Automatic configuration')
    expect(methods.text()).toContain('CC Switch configuration')

    await openOnboardingStep(wrapper, 1)
    await selectProviderModel(wrapper, 'GPT-5.4', 'gpt-5.4')
    await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="onboarding-step-1"]').exists()).toBe(true)
    expect((testUserProvider as any).mock.calls).toHaveLength(1)
    expect(wrapper.text()).toContain('Connection successful')

    await openConfigurationMethods(wrapper)
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(true)
    await openOnboardingStep(wrapper, 1)
    await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
    await flushPromises()
    expect((testUserProvider as any).mock.calls).toHaveLength(2)
  })

  it('shows a clear empty state when no access groups are available', async () => {
    const { wrapper } = await mountUserViewWithProviders([
      {
        id: 9,
        name: 'empty',
        display_name: 'Empty',
        base_url: 'https://empty.example.com',
        default_model: 'claude-sonnet',
        is_primary: true,
        groups: [],
      },
    ])

    expect(wrapper.text()).toContain('No access group available yet')
    expect(wrapper.text()).toContain('Ask an admin to grant an access group before continuing')
    expect(wrapper.find('[data-testid="create-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)
  })

  it('prefers the first provider with groups when the primary provider has none', async () => {
    const { wrapper } = await mountUserViewWithProviders([
      {
        id: 9,
        name: 'empty',
        display_name: 'Empty',
        base_url: 'https://empty.example.com',
        default_model: 'claude-sonnet',
        is_primary: true,
        groups: [],
      },
      {
        id: 10,
        name: 'usable',
        display_name: 'Usable',
        base_url: 'https://usable.example.com',
        default_model: 'claude-sonnet',
        is_primary: false,
        groups: [
          {
            group_id: '77',
            group_name: 'Usable Group',
            platform: 'anthropic',
            credential: { state: 'existing_hidden', api_key_id: 31, name: 'alice', status: 'active', key: 'sk-usable' },
          },
        ],
      },
    ])

    await openOnboardingStep(wrapper, 0)
    expect(wrapper.text()).toContain('Usable Group')
    expect(wrapper.text()).not.toContain('This access source has no groups available')
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
    await selectProviderModel(wrapper, 'Claude Sonnet 4.6', 'claude-sonnet-4-6')
    await wrapper.get('[data-testid="user-provider-test-run"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="onboarding-step-1"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Connection successful')

    await selectAccessGroup(wrapper, '44')
    expect(wrapper.find('[data-testid="configuration-methods"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Connection successful')

    await selectAccessGroup(wrapper, '43')
    await openOnboardingStep(wrapper, 1)
    await wrapper.get('[data-testid="regenerate-key"]').trigger('click')
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="onboarding-step-1"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Connection successful')
  })

  it('shows only the matching CC Switch import target for the selected group platform', async () => {
    const { wrapper } = await mountUserView()

    const methods = (await openConfigurationMethods(wrapper)).text()
    expect(methods).toContain('CC Switch configuration')
    expect(methods).toContain('Import to Claude')
    expect(methods).not.toContain('Import to Codex')
    expect(methods).not.toContain('Import to Gemini')
    expect(methods).toContain('Download CC Switch')
  })

  it('does not map the selected connection-test model in the Claude CC Switch import link', async () => {
    const { wrapper } = await mountUserView()

    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const claudeImport = wrapper.get('[data-testid="ccswitch-import-claude"]')
    const href = claudeImport.attributes('href') ?? ''
    expect(href).toContain('app=claude')
    expect(href).toContain('configFormat=json')
    expect(href).not.toContain('model=claude-sonnet-4-6')

    expect(decodeCCSwitchConfig(href)).toEqual({
      env: {
        ANTHROPIC_BASE_URL: 'https://prod.example.com',
        ANTHROPIC_AUTH_TOKEN: 'sk-existing-claude-123456',
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
        CLAUDE_CODE_ATTRIBUTION_HEADER: '0',
      },
    })
  })

  it('passes an explicit Codex model in the OpenAI CC Switch import link', async () => {
    const { createGroupCredential } = await import('@/api/user')
    ;(createGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-openai' } },
    })

    const { wrapper } = await mountUserView()
    await selectAccessGroup(wrapper, '42')
    await wrapper.get('[data-testid="primary-onboarding-action"]').trigger('click')
    await flushPromises()

    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const codexImport = wrapper.get('[data-testid="ccswitch-import-codex"]')
    const href = codexImport.attributes('href') ?? ''
    expect(href).toContain('app=codex')
    expect(href).toContain('configFormat=json')
    expect(href).not.toContain('model=gpt-5.4')

    expect(decodeCCSwitchConfig(href)).toEqual({
      auth: {
        OPENAI_API_KEY: 'sk-openai',
      },
      config: [
        'model_provider = "custom"',
        'model = "gpt-5.4"',
        'review_model = "gpt-5.4"',
        'model_reasoning_effort = "xhigh"',
        'disable_response_storage = true',
        'network_access = "enabled"',
        'windows_wsl_setup_acknowledged = true',
        'model_context_window = 1000000',
        'model_auto_compact_token_limit = 900000',
        '',
        '[model_providers.custom]',
        'name = "Production / Group Alpha"',
        'base_url = "https://prod.example.com"',
        'wire_api = "responses"',
        'requires_openai_auth = true',
      ].join('\n'),
    })
  })

  it('passes the selected Gemini model in the CC Switch import link', async () => {
    const { wrapper } = await mountUserView()

    await selectAccessGroup(wrapper, '45')

    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    const geminiImport = wrapper.get('[data-testid="ccswitch-import-gemini"]')
    const href = geminiImport.attributes('href') ?? ''
    expect(href).toContain('app=gemini')
    expect(href).toContain('configFormat=json')
    expect(href).not.toContain('model=gemini-3.1-pro-preview')

    expect(decodeCCSwitchConfig(href)).toEqual({
      GEMINI_API_KEY: 'sk-existing-gemini-123456',
      GOOGLE_GEMINI_BASE_URL: 'https://prod.example.com',
      GEMINI_MODEL: 'gemini-3.1-pro-preview',
    })
  })

  it('shows Agent-only configuration methods and manual snippets for Agent groups', async () => {
    const { wrapper } = await mountUserView()

    await selectAccessGroup(wrapper, '46')

    const methods = (await openConfigurationMethods(wrapper)).text()
    expect(methods).toContain('Manual configuration')
    expect(methods).toContain('App import')
    expect(methods).not.toContain('Automatic configuration')
    expect(wrapper.find('[data-testid="config-method-automatic"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="config-method-ccswitch"]').exists()).toBe(true)

    await wrapper.get('[data-testid="config-method-manual"]').trigger('click')
    const manualText = wrapper.text()
    expect(manualText).toContain('Hermes Agent')
    expect(manualText).toContain('OpenClaw')
    expect(manualText).toContain('Custom Agent')
    expect(manualText).not.toContain('Codex config')
    expect(manualText).not.toContain('Codex auth')
    expect(manualText).not.toContain('Claude settings')
    expect(manualText).not.toContain('Gemini env')
  })

  it('shows only Hermes and OpenClaw imports for Agent groups', async () => {
    const { wrapper } = await mountUserView()

    await selectAccessGroup(wrapper, '46')
    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')

    const panelText = wrapper.text()
    expect(panelText).toContain('Import to Hermes Agent')
    expect(panelText).toContain('Import to OpenClaw')
    expect(panelText).not.toContain('Import to Codex')
    expect(panelText).not.toContain('Import to Claude')
    expect(panelText).not.toContain('Import to Gemini')

    const hermesHref = wrapper.get('[data-testid="ccswitch-import-hermes"]').attributes('href') ?? ''
    const openclawHref = wrapper.get('[data-testid="ccswitch-import-openclaw"]').attributes('href') ?? ''
    expect(hermesHref).toContain('app=hermes')
    expect(hermesHref).toContain('endpoint=https%3A%2F%2Fprod.example.com%2Fv1')
    expect(hermesHref).toContain('apiKey=sk-existing-agent-openai-123456')
    expect(hermesHref).not.toContain('configFormat=json')
    expect(openclawHref).toContain('app=openclaw')
    expect(openclawHref).toContain('endpoint=https%3A%2F%2Fprod.example.com%2Fv1')
    expect(openclawHref).toContain('apiKey=sk-existing-agent-openai-123456')
    expect(openclawHref).not.toContain('configFormat=json')
  })

  it('explains Agent imports use OpenAI-compatible v1 endpoints', async () => {
    const { wrapper } = await mountUserView()

    await selectAccessGroup(wrapper, '47')
    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    expect(wrapper.text()).toContain('Agent imports use the OpenAI-compatible /v1 endpoint')
    expect(wrapper.text()).toContain('Hermes Agent and OpenClaw use Chat Completions providers')
    expect(wrapper.find('[data-testid="agent-import-v1-notice"]').exists()).toBe(true)

    await selectAccessGroup(wrapper, '48')
    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-ccswitch"]').trigger('click')
    expect(wrapper.text()).toContain('Agent imports use the OpenAI-compatible /v1 endpoint')
    expect(wrapper.find('[data-testid="agent-import-v1-notice"]').exists()).toBe(true)
  })

  it('keeps reporting commands out of automatic tool configuration', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.text()).not.toContain('Advanced command reference')
    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-automatic"]').trigger('click')
    expect(wrapper.text()).toContain('Configure the selected provider')
    expect(wrapper.text()).toContain('ae-cli discover --provider prod')
    expect(wrapper.text()).not.toContain('ae-cli hooks enable --global')
    expect(wrapper.text()).not.toContain('ae-cli init')
    expect(wrapper.text()).not.toContain('ae-cli sync')
    expect(wrapper.text()).not.toContain('ae-cli hooks status --uploads')
    expect(wrapper.text()).not.toContain('Check GitHub connectivity')

    expect(wrapper.find('[data-testid="auto-install-fallback"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="auto-login-fallback"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="reporting-diagnostics"]').text()).toContain('ae-cli doctor')
  })

  it('shows a collapsed platform-specific discover fallback only inside automatic configuration', async () => {
    const { wrapper } = await mountUserView()

    expect(wrapper.find('[data-testid="auto-discover-fallback"]').exists()).toBe(false)

    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-automatic"]').trigger('click')

    let fallback = wrapper.get('[data-testid="auto-discover-fallback"]')
    expect(fallback.attributes('open')).toBeUndefined()
    expect(fallback.element.previousElementSibling?.textContent).toBe('ae-cli discover --provider prod')
    expect(fallback.text()).toContain('Explicit tool fallback')
    expect(fallback.text()).toContain('automatic discover skips the selected tool')
    expect(fallback.text()).toContain('ae-cli discover --provider prod --tool claude')
    expect(fallback.text()).toContain('--tool bypasses installation detection only')
    expect(fallback.text()).toContain('codex, claude, and gemini')
    expect(fallback.text()).toContain('Repeat --tool or pass comma-separated values')

    await wrapper.get('[data-testid="config-method-manual"]').trigger('click')
    expect(wrapper.find('[data-testid="auto-discover-fallback"]').exists()).toBe(false)

    await selectAccessGroup(wrapper, '44')
    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-automatic"]').trigger('click')
    fallback = wrapper.get('[data-testid="auto-discover-fallback"]')
    expect(fallback.element.previousElementSibling?.textContent).toBe('ae-cli discover --provider prod')
    expect(fallback.text()).toContain('ae-cli discover --provider prod --tool codex')

    await selectAccessGroup(wrapper, '45')
    await openConfigurationMethods(wrapper)
    await wrapper.get('[data-testid="config-method-automatic"]').trigger('click')
    fallback = wrapper.get('[data-testid="auto-discover-fallback"]')
    expect(fallback.element.previousElementSibling?.textContent).toBe('ae-cli discover --provider prod')
    expect(fallback.text()).toContain('ae-cli discover --provider prod --tool gemini')
  })

  it('shows audience guidance on each configuration method card', async () => {
    const { wrapper } = await mountUserView()

    const methods = (await openConfigurationMethods(wrapper)).text()
    expect(methods).toContain('Best for non-developers, independent agents')
    expect(methods).toContain('Best for engineering teams')
    expect(methods).toContain('Best for non-developers who prefer a desktop app to manage tool configuration')
  })

  it('switches providers and updates the discover command and group list', async () => {
    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="provider-1"]').trigger('click')
    expect(wrapper.text()).toContain('https://staging.example.com')
    expect(wrapper.text()).toContain('Finish setup in 3 steps')
    expect(wrapper.text()).toContain('OpenAI-Staging')
  })

  it('calls createGroupCredential for the selected provider and group', async () => {
    const { createGroupCredential, getUserProviderModels } = await import('@/api/user')
    ;(createGroupCredential as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-new' } },
    })

    const { wrapper } = await mountUserView()
    await selectAccessGroup(wrapper, '42')
    await wrapper.get('[data-testid="primary-onboarding-action"]').trigger('click')
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
    await selectAccessGroup(wrapper, '42')

    const button = wrapper.get('[data-testid="primary-onboarding-action"]')
    await button.trigger('click')
    await flushPromises()

    expect(createGroupCredential).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="primary-onboarding-action"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="primary-onboarding-action"]').trigger('click')
    expect(createGroupCredential).toHaveBeenCalledTimes(1)

    resolveCreate({
      data: { data: { api_key_id: 7, name: 'alice', status: 'active', secret: 'sk-new' } },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="primary-onboarding-action"]').exists()).toBe(false)
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

    await selectAccessGroup(wrapper, '42')
    await wrapper.get('[data-testid="primary-onboarding-action"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="reveal-key"]').trigger('click')
    expect(wrapper.text()).toContain('Confirm reveal API key')
    expect(wrapper.text()).not.toContain('sk-openai')
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    expect(wrapper.text()).toContain('sk-openai')

    await selectAccessGroup(wrapper, '43')
    await openOnboardingStep(wrapper, 1)
    await wrapper.get('[data-testid="regenerate-key"]').trigger('click')
    expect(wrapper.text()).toContain('Confirm regenerate API key')
    expect(regenerateGroupCredential).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('sk-claude')
    expect(wrapper.text()).toContain('sk-c***')
    expect(wrapper.get('[data-testid="reveal-key"]').text()).toContain('Reveal')

    await selectAccessGroup(wrapper, '42')
    await openOnboardingStep(wrapper, 1)
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
    expect(messageSuccess).toHaveBeenCalledWith('Copied')
  })

  it('shows Element Plus feedback when copying a key fails', async () => {
    ;(navigator.clipboard.writeText as any).mockRejectedValueOnce(new Error('clipboard unavailable'))
    const { wrapper } = await mountUserView()

    await wrapper.get('[data-testid="copy-key"]').trigger('click')
    await wrapper.get('[data-testid="confirm-secret-action"]').trigger('click')
    await flushPromises()

    expect(messageError).toHaveBeenCalledWith('Copy failed')
  })

  it('tests the selected provider with the current group platform', async () => {
    const { testUserProvider } = await import('@/api/user')
    ;(testUserProvider as any).mockResolvedValue({
      data: { data: { success: true, message: 'Connection successful', response: 'pong' } },
    })

    const { wrapper } = await mountUserView()
    await selectProviderModel(wrapper, 'Claude Sonnet 4.6', 'claude-sonnet-4-6')
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
    expect(modelSelect.classes()).toContain('el-select')
    expect(modelSelect.text()).toContain('Claude Sonnet 4.6')
    expect(getUserProviderModels).toHaveBeenCalledWith(2, '43', 'anthropic')

    await selectAccessGroup(wrapper, '44')
    await openOnboardingStep(wrapper, 1)

    expect(getUserProviderModels).toHaveBeenCalledWith(2, '44', 'openai')
    const refreshedSelect = wrapper.get('[data-testid="user-provider-test-model"]')
    expect(refreshedSelect.text()).toContain('GPT-5.4')
  })

  it('does not show the promoted test action when the selected group has no API key', async () => {
    const { testUserProvider } = await import('@/api/user')
    const { wrapper } = await mountUserView()

    await selectAccessGroup(wrapper, '42')

    expect(wrapper.find('[data-testid="user-provider-test-run"]').exists()).toBe(false)
    expect(testUserProvider).not.toHaveBeenCalled()
  })
})
