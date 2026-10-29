export type InstallPlatform = 'shell' | 'windows'
export type CCSwitchApp = 'codex' | 'claude' | 'gemini' | 'hermes' | 'openclaw'
export type DiscoverTool = 'codex' | 'claude' | 'gemini'

export type ManualConfigSnippetKey =
  | 'codex-config'
  | 'codex-auth'
  | 'codex-env'
  | 'claude-settings'
  | 'gemini-env'
  | 'gemini-reload'
  | 'gemini-model'
  | 'hermes-agent'
  | 'openclaw-agent'
  | 'custom-agent-env'
  | 'custom-agent-json'
  | 'dsh-config'
  | 'dsh-steps'

export type ManualConfigSnippet = {
  key: ManualConfigSnippetKey
  path: string
  body: string
  containsSecret: boolean
  /** code: 可复制的配置片段；steps: 供人阅读的操作步骤，不提供复制 */
  render?: 'code' | 'steps'
}

export type ManualConfigSnippetInput = {
  providerName: string
  baseUrl: string
  platform: string
  apiKey: string
  groupName?: string | null
  model?: string
  models?: string[]
}

export type CCSwitchProviderImportInput = {
  app: CCSwitchApp
  name: string
  endpoint: string
  apiKey: string
  model?: string
  enabled?: boolean
}

const GITHUB_RELEASE_API_URL = 'https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest'
const CODEX_MODEL = 'gpt-5.4'
const GEMINI_MODEL = 'gemini-3.1-pro-preview'

type AgentPlatform = 'openai' | 'anthropic' | 'gemini'

type AgentPlatformProfile = {
  platform: AgentPlatform
  displayName: string
  baseUrl: string
  env: Record<string, string>
  openClawApi: 'openai-completions'
  hermesApiMode: 'chat_completions'
}

type PlatformSource = {
  platform?: string
  userAgent?: string
  userAgentData?: {
    platform?: string
  }
}

export function detectInstallPlatform(source: PlatformSource = navigator): InstallPlatform {
  const platform = [
    source.userAgentData?.platform,
    source.platform,
    source.userAgent,
  ].filter(Boolean).join(' ')
  return /windows|win32|win64/i.test(platform) ? 'windows' : 'shell'
}

export function buildInstallCommand(origin: string) {
  return `curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=${origin} bash`
}

function tomlString(value: string) {
  return JSON.stringify(value)
}

function tomlTableKey(value: string) {
  return /^[A-Za-z0-9_-]+$/.test(value) ? value : tomlString(value)
}

function shellString(value: string) {
  return `"${value.replace(/(["\\$`])/g, '\\$1')}"`
}

export function isAgentAccessGroup(groupName: string | null | undefined) {
  return Boolean(groupName?.startsWith('Agent'))
}

export function buildAgentProviderBaseUrl(baseUrl: string) {
  const trimmed = baseUrl.trim().replace(/\/+$/, '')
  if (!trimmed) return '/v1'
  if (/\/v\d+$/i.test(trimmed)) return trimmed
  return `${trimmed}/v1`
}

function normalizeAgentPlatform(platform: string): AgentPlatform | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai' || normalized === 'composite') return 'openai'
  if (normalized === 'anthropic') return 'anthropic'
  if (normalized === 'gemini') return 'gemini'
  return null
}

function resolveAgentPlatformProfile(platform: string, baseUrl: string, apiKey: string, model?: string): AgentPlatformProfile | null {
  const normalized = normalizeAgentPlatform(platform)
  const selectedModel = model?.trim()
  if (!normalized) return null
  const agentBaseUrl = buildAgentProviderBaseUrl(baseUrl)
  return {
    platform: normalized,
    displayName: 'OpenAI-compatible Chat Completions',
    baseUrl: agentBaseUrl,
    env: {
      OPENAI_API_KEY: apiKey,
      OPENAI_BASE_URL: agentBaseUrl,
      ...(selectedModel ? { OPENAI_MODEL: selectedModel } : {}),
    },
    openClawApi: 'openai-completions',
    hermesApiMode: 'chat_completions',
  }
}

function buildClaudeSettingsEnv(baseUrl: string, apiKey: string) {
  return {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
  }
}

function buildCodexCCSwitchConfig(name: string, baseUrl: string, apiKey: string, model?: string) {
  const selectedModel = model?.trim() || CODEX_MODEL
  const apiBase = buildAgentProviderBaseUrl(baseUrl)
  const config = [
    `model_provider = "custom"`,
    `model = ${tomlString(selectedModel)}`,
    `review_model = ${tomlString(selectedModel)}`,
    `disable_response_storage = true`,
    `network_access = "enabled"`,
    `windows_wsl_setup_acknowledged = true`,
    ``,
    `[model_providers.custom]`,
    `name = ${tomlString(name)}`,
    `base_url = ${tomlString(apiBase)}`,
    `model_catalog_url = ${tomlString(apiBase + '/models')}`,
    `wire_api = "responses"`,
    `requires_openai_auth = true`,
    ``,
    `[features]`,
    `api_key_model_discovery = true`,
    `goals = true`,
  ].join('\n')

  return {
    auth: {
      OPENAI_API_KEY: apiKey,
    },
    config,
  }
}

function encodeBase64(value: string) {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  const chunkSize = 0x8000
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize))
  }
  return btoa(binary)
}

function buildClaudeCCSwitchConfig(baseUrl: string, apiKey: string) {
  return { env: buildClaudeSettingsEnv(baseUrl, apiKey) }
}

function buildGeminiCCSwitchConfig(baseUrl: string, apiKey: string, model?: string) {
  const config: Record<string, string> = {
    GEMINI_API_KEY: apiKey,
    GOOGLE_GEMINI_BASE_URL: baseUrl,
  }
  const selectedModel = model?.trim()
  if (selectedModel) {
    config.GEMINI_MODEL = selectedModel
  }
  return config
}

export function buildCodexConfigSnippet(providerName: string, baseUrl: string, model?: string) {
  const provider = providerName.trim() || 'OpenAI'
  const selectedModel = model?.trim() || CODEX_MODEL
  const apiBase = buildAgentProviderBaseUrl(baseUrl)
  return [
    `model_provider = ${tomlString(provider)}`,
    `model = ${tomlString(selectedModel)}`,
    `review_model = ${tomlString(selectedModel)}`,
    `disable_response_storage = true`,
    `network_access = "enabled"`,
    `windows_wsl_setup_acknowledged = true`,
    ``,
    `[model_providers.${tomlTableKey(provider)}]`,
    `name = ${tomlString(provider)}`,
    `base_url = ${tomlString(apiBase)}`,
    `model_catalog_url = ${tomlString(apiBase + '/models')}`,
    `wire_api = "responses"`,
    `requires_openai_auth = true`,
    ``,
    `[features]`,
    `api_key_model_discovery = true`,
    `goals = true`,
  ].join('\n')
}

export function buildCodexAuthSnippet(apiKey: string) {
  return JSON.stringify({ OPENAI_API_KEY: apiKey })
}

export function buildClaudeSettingsSnippet(baseUrl: string, apiKey: string) {
  return JSON.stringify({
    $schema: 'https://json.schemastore.org/claude-code-settings.json',
    env: buildClaudeSettingsEnv(baseUrl, apiKey),
  }, null, 2)
}

export function buildGeminiEnvSnippet(baseUrl: string, apiKey: string) {
  return [
    `export GEMINI_API_KEY=${shellString(apiKey)}`,
    `export GOOGLE_GEMINI_BASE_URL=${shellString(baseUrl)}`,
  ].join('\n')
}

export function buildGeminiReloadSnippet() {
  return [
    'case "${SHELL##*/}" in',
    '  zsh) rc_file="$HOME/.zshrc" ;;',
    '  bash) rc_file="$HOME/.bashrc" ;;',
    '  *) rc_file="$HOME/.profile" ;;',
    'esac',
    '[ -f "$rc_file" ] && source "$rc_file"',
  ].join('\n')
}

export function buildGeminiModelSnippet() {
  return [
    `export GEMINI_MODEL=${shellString(GEMINI_MODEL)}`,
    '# Do not manually switch models inside Gemini after setting this value.',
  ].join('\n')
}

function envExports(env: Record<string, string>) {
  return Object.entries(env)
    .map(([key, value]) => `export ${key}=${shellString(value)}`)
    .join('\n')
}

function buildHermesAgentSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  const providerName = input.providerName.trim() || 'ae-agent'
  const model = input.model?.trim() || '<model-id>'
  return [
    '# Merge this into ~/.hermes/config.yaml to make this Agent group the active main model.',
    'model:',
    `  provider: ${JSON.stringify(`custom:${providerName}`)}`,
    `  default: ${JSON.stringify(model)}`,
    `  base_url: ${JSON.stringify(profile.baseUrl)}`,
    `  api_mode: ${JSON.stringify(profile.hermesApiMode)}`,
    '',
    'custom_providers:',
    `  - name: ${JSON.stringify(providerName)}`,
    `    base_url: ${JSON.stringify(profile.baseUrl)}`,
    `    api_key: ${JSON.stringify(input.apiKey)}`,
    `    api_mode: ${JSON.stringify(profile.hermesApiMode)}`,
    '    models:',
    `      ${JSON.stringify(model)}:`,
    '        context_length: 200000',
  ].join('\n')
}

function buildOpenClawAgentSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  const providerName = input.providerName.trim() || 'ae-agent'
  const model = input.model?.trim() || '<model-id>'
  return JSON.stringify({
    models: {
      mode: 'merge',
      providers: {
        [providerName]: {
          baseUrl: profile.baseUrl,
          apiKey: input.apiKey,
          api: profile.openClawApi,
          models: [{ id: model, name: model }],
        },
      },
    },
  }, null, 2)
}

function buildCustomAgentJSONSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  return JSON.stringify({
    platform: profile.platform,
    protocol: profile.displayName,
    base_url: profile.baseUrl,
    api_key: input.apiKey,
    ...(input.model?.trim() ? { model: input.model.trim() } : {}),
  }, null, 2)
}

function buildAgentManualConfigSnippets(input: ManualConfigSnippetInput): ManualConfigSnippet[] {
  const profile = resolveAgentPlatformProfile(input.platform, input.baseUrl, input.apiKey, input.model)
  if (!profile) {
    return [
      {
        key: 'custom-agent-json',
        path: 'Custom Agent provider values',
        body: JSON.stringify({
          platform: input.platform,
          base_url: buildAgentProviderBaseUrl(input.baseUrl),
          api_key: input.apiKey,
          ...(input.model?.trim() ? { model: input.model.trim() } : {}),
        }, null, 2),
        containsSecret: true,
      },
    ]
  }
  return [
    {
      key: 'hermes-agent',
      path: '~/.hermes/config.yaml',
      body: buildHermesAgentSnippet(input, profile),
      containsSecret: true,
    },
    {
      key: 'openclaw-agent',
      path: '~/.openclaw/openclaw.json',
      body: buildOpenClawAgentSnippet(input, profile),
      containsSecret: true,
    },
    {
      key: 'custom-agent-env',
      path: 'Custom Agent environment',
      body: envExports(profile.env),
      containsSecret: true,
    },
    {
      key: 'custom-agent-json',
      path: 'Custom Agent JSON',
      body: buildCustomAgentJSONSnippet(input, profile),
      containsSecret: true,
    },
  ]
}

/** 与 sub2api「使用 API 密钥」面板一致的 Codex 配置（环境变量 + 模型目录） */
export function buildCompositeCodexConfigSnippet(input: ManualConfigSnippetInput) {
  const providerId = 'sub2api'
  const baseUrl = buildAgentProviderBaseUrl(input.baseUrl)
  const model = input.model?.trim() || resolveOnboardingModels(input)[0] || 'deepseek-v4-pro'
  return [
    `# Codex CLI -> Sub2API Composite group`,
    `model_provider = ${tomlString(providerId)}`,
    `model = ${tomlString(model)}`,
    `review_model = ${tomlString(model)}`,
    `disable_response_storage = true`,
    ``,
    `[model_providers.${tomlTableKey(providerId)}]`,
    `name = "Sub2API Composite"`,
    `base_url = ${tomlString(baseUrl)}`,
    `model_catalog_url = ${tomlString(baseUrl + '/models')}`,
    `env_key = "SUB2API_API_KEY"`,
    `wire_api = "responses"`,
    `requires_openai_auth = false`,
    `supports_websockets = false`,
    ``,
    `[features]`,
    `api_key_model_discovery = true`,
  ].join('\n')
}

/** Codex 通过环境变量取密钥，不再需要 auth.json */
export function buildCodexEnvSnippet(apiKey: string) {
  return [
    `# macOS / Linux`,
    `export SUB2API_API_KEY="${apiKey}"`,
    ``,
    `# Windows PowerShell`,
    `$env:SUB2API_API_KEY="${apiKey}"`,
  ].join('\n')
}

const DSH_API_KEY_ENV = 'SUB2API_API_KEY'

export function resolveOnboardingModels(input: ManualConfigSnippetInput): string[] {
  const listed = (input.models ?? []).map((item) => item.trim()).filter(Boolean)
  if (listed.length > 0) return listed
  const single = input.model?.trim()
  return single ? [single] : []
}

/** DSH 的自定义供应商配置（写入 ~/.dsh/profiles/<profile>/cordis.patch.yml） */
export function buildDshProviderSnippet(input: ManualConfigSnippetInput) {
  const baseUrl = buildAgentProviderBaseUrl(input.baseUrl)
  const providerId = 'sub2api'
  const models = resolveOnboardingModels(input)
  const modelLines = models.length > 0
    ? models.flatMap((model) => [
        `          - id: ${model}`,
        `            name: ${model}`,
        `            contextWindow: 1000000`,
        `            maxTokens: 384000`,
      ])
    : [
        `          # 先点击「获取可用模型」再把模型 id 填入这里`,
        `          - id: deepseek-v4-pro`,
        `            name: deepseek-v4-pro`,
        `            contextWindow: 1000000`,
        `            maxTokens: 384000`,
      ]
  return [
    `- id: llm-pi-ai`,
    `  name: "@deepseek-ai/dsh-llm-pi-ai"`,
    `  config:`,
    `    providers:`,
    `      ${providerId}:`,
    `        displayName: ${input.providerName.trim() || 'Sub2API'}`,
    `        apiKeyEnv: ${DSH_API_KEY_ENV}`,
    `        api: openai-completions`,
    `        baseURL: ${baseUrl}`,
    `        compat:`,
    `          supportsStore: false`,
    `          supportsDeveloperRole: false`,
    `          maxTokensField: max_tokens`,
    `          requiresReasoningContentOnAssistantMessages: true`,
    `          thinkingFormat: deepseek`,
    `        defaultContextWindow: 1000000`,
    `        defaultMaxTokens: 384000`,
    `        models:`,
    ...modelLines,
  ].join('\n')
}

/** DSH 界面配置步骤（新手推荐路径） */
export function buildDshStepsSnippet(input: ManualConfigSnippetInput) {
  const baseUrl = buildAgentProviderBaseUrl(input.baseUrl)
  const models = resolveOnboardingModels(input)
  const modelHint = models.length > 0 ? models.join(' / ') : 'deepseek-v4-pro / deepseek-flash'
  return [
    `1. 安装并启动 DSH（已安装可跳过）。官方安装说明：https://deepseek-harness.github.io/deepseek-harness/guide/quickstart`,
    ``,
    `2. 打开 DSH 设置 → 模型供应商 → 添加自定义供应商。官方配置说明：https://deepseek-harness.github.io/deepseek-harness/guide/providers`,
    `   名称:     ${input.providerName.trim() || 'Sub2API'}`,
    `   Base URL: ${baseUrl}`,
    `   API Key:  ${input.apiKey}`,
    `   协议:     openai-completions`,
    ``,
    `3. 点击「获取可用模型」，从列表中选择（不要手动输入；探测失败时也可手动填写模型 ID）`,
    `   可选模型: ${modelHint}`,
    ``,
    `4. 新建会话，在模型选择器里选中 ${modelHint.split(' / ')[0]}`,
  ].join('\n')
}

export function buildManualConfigSnippets(input: ManualConfigSnippetInput): ManualConfigSnippet[] {
  if (isAgentAccessGroup(input.groupName)) {
    return buildAgentManualConfigSnippets(input)
  }
  const platform = input.platform.trim().toLowerCase()
  if (platform === 'composite') {
    return [
      {
        key: 'codex-config',
        path: '~/.codex/config.toml',
        body: buildCompositeCodexConfigSnippet(input),
        containsSecret: false,
      },
      {
        key: 'codex-env',
        path: '终端环境变量',
        body: buildCodexEnvSnippet(input.apiKey),
        containsSecret: true,
      },
      {
        key: 'claude-settings',
        path: '~/.claude/settings.json',
        body: buildClaudeSettingsSnippet(input.baseUrl, input.apiKey),
        containsSecret: true,
      },
      {
        key: 'dsh-config',
        path: '~/.dsh/profiles/desktop/cordis.patch.yml',
        body: buildDshProviderSnippet(input),
        containsSecret: false,
      },
      {
        key: 'dsh-steps',
        path: '',
        body: buildDshStepsSnippet(input),
        containsSecret: true,
        render: 'steps',
      },
    ]
  }
  if (platform === 'openai') {
    return [
      {
        key: 'codex-config',
        path: '~/.codex/config.toml',
        body: buildCodexConfigSnippet('OpenAI', input.baseUrl, input.model),
        containsSecret: false,
      },
      {
        key: 'codex-auth',
        path: '~/.codex/auth.json',
        body: buildCodexAuthSnippet(input.apiKey),
        containsSecret: true,
      },
    ]
  }
  if (platform === 'anthropic') {
    return [
      {
        key: 'claude-settings',
        path: '~/.claude/settings.json',
        body: buildClaudeSettingsSnippet(input.baseUrl, input.apiKey),
        containsSecret: true,
      },
    ]
  }
  if (platform === 'gemini') {
    return [
      {
        key: 'gemini-env',
        path: '~/.ae-cli/env.sh',
        body: buildGeminiEnvSnippet(input.baseUrl, input.apiKey),
        containsSecret: true,
      },
      {
        key: 'gemini-reload',
        path: 'Shell reload',
        body: buildGeminiReloadSnippet(),
        containsSecret: false,
      },
      {
        key: 'gemini-model',
        path: 'Current shell',
        body: buildGeminiModelSnippet(),
        containsSecret: false,
      },
    ]
  }
  return []
}

export function resolveCCSwitchAppForPlatform(platform: string): CCSwitchApp | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai' || normalized === 'composite') return 'codex'
  if (normalized === 'anthropic') return 'claude'
  if (normalized === 'gemini') return 'gemini'
  return null
}

export function resolveDiscoverToolForPlatform(platform: string): DiscoverTool | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai' || normalized === 'composite') return 'codex'
  if (normalized === 'anthropic') return 'claude'
  if (normalized === 'gemini') return 'gemini'
  return null
}

export function resolveCCSwitchAppsForGroup(platform: string, groupName?: string | null): CCSwitchApp[] {
  if (isAgentAccessGroup(groupName)) {
    return ['hermes', 'openclaw']
  }
  if (platform.trim().toLowerCase() === 'composite') {
    // composite 组同时提供 OpenAI(Codex) 与 Anthropic(Claude Code) 协议
    return ['codex', 'claude']
  }
  const app = resolveCCSwitchAppForPlatform(platform)
  return app ? [app] : []
}

export function buildCCSwitchProviderImportLink(input: CCSwitchProviderImportInput) {
  if (input.app === 'claude') {
    const params = new URLSearchParams({
      resource: 'provider',
      app: input.app,
      name: input.name,
      enabled: String(input.enabled ?? true),
      configFormat: 'json',
      config: encodeBase64(JSON.stringify(buildClaudeCCSwitchConfig(input.endpoint, input.apiKey))),
    })
    return `ccswitch://v1/import?${params.toString()}`
  }

  if (input.app === 'codex') {
    const params = new URLSearchParams({
      resource: 'provider',
      app: input.app,
      name: input.name,
      enabled: String(input.enabled ?? true),
      configFormat: 'json',
      config: encodeBase64(JSON.stringify(buildCodexCCSwitchConfig(input.name, input.endpoint, input.apiKey, input.model))),
    })
    return `ccswitch://v1/import?${params.toString()}`
  }

  if (input.app === 'gemini') {
    const params = new URLSearchParams({
      resource: 'provider',
      app: input.app,
      name: input.name,
      enabled: String(input.enabled ?? true),
      configFormat: 'json',
      config: encodeBase64(JSON.stringify(buildGeminiCCSwitchConfig(input.endpoint, input.apiKey, input.model))),
    })
    return `ccswitch://v1/import?${params.toString()}`
  }

  const params = new URLSearchParams({
    resource: 'provider',
    app: input.app,
    name: input.name,
    endpoint: input.app === 'hermes' || input.app === 'openclaw' ? buildAgentProviderBaseUrl(input.endpoint) : input.endpoint,
    apiKey: input.apiKey,
    enabled: String(input.enabled ?? true),
  })
  if (input.model) {
    params.set('model', input.model)
  }
  return `ccswitch://v1/import?${params.toString()}`
}

export function buildGithubConnectivityCommand() {
  return `curl -fsSI --connect-timeout 5 ${GITHUB_RELEASE_API_URL}`
}

export function buildWindowsInstallCommand(origin: string) {
  return `$env:AE_CLI_INSTALL_SERVER_URL = "${origin}"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex`
}

export function buildWindowsGithubConnectivityCommand() {
  return `iwr -UseB -Method Head ${GITHUB_RELEASE_API_URL}`
}

export function buildPreferredInstallCommand(origin: string, platform: InstallPlatform = detectInstallPlatform()) {
  return platform === 'windows' ? buildWindowsInstallCommand(origin) : buildInstallCommand(origin)
}

export function buildPreferredGithubConnectivityCommand(platform: InstallPlatform = detectInstallPlatform()) {
  return platform === 'windows' ? buildWindowsGithubConnectivityCommand() : buildGithubConnectivityCommand()
}

export function buildLoginCommand(origin: string) {
  return 'ae-cli login'
}

export function buildDeviceLoginCommand(origin: string) {
  return 'ae-cli login --device'
}

export function buildDiscoverCommand(origin: string, providerName: string, explicitTool?: DiscoverTool) {
  const command = `ae-cli discover --provider ${providerName}`
  return explicitTool ? `${command} --tool ${explicitTool}` : command
}

export function buildHooksGlobalCommand() {
  return 'ae-cli hooks enable --global'
}

export function buildRepoInitCommand() {
	return 'ae-cli init --hooks repo'
}

export function buildDoctorCommand() {
  return 'ae-cli doctor'
}

export function buildSyncCommand() {
  return 'ae-cli sync'
}

export function buildHooksStatusUploadsCommand() {
  return 'ae-cli hooks status --uploads'
}
