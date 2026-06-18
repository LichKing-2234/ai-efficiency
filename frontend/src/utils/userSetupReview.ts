export type InstallPlatform = 'shell' | 'windows'
export type CCSwitchApp = 'codex' | 'claude' | 'gemini' | 'hermes' | 'openclaw'

export type ManualConfigSnippetKey =
  | 'codex-config'
  | 'codex-auth'
  | 'claude-settings'
  | 'gemini-env'
  | 'gemini-reload'
  | 'gemini-model'
  | 'hermes-agent'
  | 'openclaw-agent'
  | 'custom-agent-env'
  | 'custom-agent-json'

export type ManualConfigSnippet = {
  key: ManualConfigSnippetKey
  path: string
  body: string
  containsSecret: boolean
}

export type ManualConfigSnippetInput = {
  providerName: string
  baseUrl: string
  platform: string
  apiKey: string
  groupName?: string | null
  model?: string
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
  env: Record<string, string>
  openClawApi: 'openai-completions' | 'anthropic-messages' | 'google-generative-ai'
  hermesApiMode: 'chat_completions' | 'anthropic_messages' | null
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

function normalizeAgentPlatform(platform: string): AgentPlatform | null {
  const normalized = platform.trim().toLowerCase()
  if (normalized === 'openai') return 'openai'
  if (normalized === 'anthropic') return 'anthropic'
  if (normalized === 'gemini') return 'gemini'
  return null
}

function resolveAgentPlatformProfile(platform: string, baseUrl: string, apiKey: string, model?: string): AgentPlatformProfile | null {
  const normalized = normalizeAgentPlatform(platform)
  const selectedModel = model?.trim()
  if (normalized === 'openai') {
    return {
      platform: 'openai',
      displayName: 'OpenAI-compatible',
      env: {
        OPENAI_API_KEY: apiKey,
        OPENAI_BASE_URL: baseUrl,
        ...(selectedModel ? { OPENAI_MODEL: selectedModel } : {}),
      },
      openClawApi: 'openai-completions',
      hermesApiMode: 'chat_completions',
    }
  }
  if (normalized === 'anthropic') {
    return {
      platform: 'anthropic',
      displayName: 'Anthropic-compatible',
      env: {
        ANTHROPIC_AUTH_TOKEN: apiKey,
        ANTHROPIC_BASE_URL: baseUrl,
        ...(selectedModel ? { ANTHROPIC_MODEL: selectedModel } : {}),
      },
      openClawApi: 'anthropic-messages',
      hermesApiMode: 'anthropic_messages',
    }
  }
  if (normalized === 'gemini') {
    return {
      platform: 'gemini',
      displayName: 'Gemini-compatible',
      env: {
        GEMINI_API_KEY: apiKey,
        GOOGLE_GEMINI_BASE_URL: baseUrl,
        ...(selectedModel ? { GEMINI_MODEL: selectedModel } : {}),
      },
      openClawApi: 'google-generative-ai',
      hermesApiMode: null,
    }
  }
  return null
}

function buildClaudeSettingsEnv(baseUrl: string, apiKey: string) {
  return {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
    CLAUDE_CODE_ATTRIBUTION_HEADER: '0',
  }
}

function buildCodexCCSwitchConfig(name: string, baseUrl: string, apiKey: string, model?: string) {
  const selectedModel = model?.trim() || CODEX_MODEL
  const config = [
    `model_provider = "custom"`,
    `model = ${tomlString(selectedModel)}`,
    `review_model = ${tomlString(selectedModel)}`,
    `model_reasoning_effort = "xhigh"`,
    `disable_response_storage = true`,
    `network_access = "enabled"`,
    `windows_wsl_setup_acknowledged = true`,
    `model_context_window = 1000000`,
    `model_auto_compact_token_limit = 900000`,
    ``,
    `[model_providers.custom]`,
    `name = ${tomlString(name)}`,
    `base_url = ${tomlString(baseUrl)}`,
    `wire_api = "responses"`,
    `requires_openai_auth = true`,
  ].join('\n')

  return {
    auth: {
      OPENAI_API_KEY: apiKey,
    },
    config,
  }
}

function resolveClaudeDefaultModelEnv(model: string) {
  const normalized = model.trim().toLowerCase()
  if (normalized.includes('haiku')) return 'ANTHROPIC_DEFAULT_HAIKU_MODEL'
  if (normalized.includes('sonnet')) return 'ANTHROPIC_DEFAULT_SONNET_MODEL'
  if (normalized.includes('opus')) return 'ANTHROPIC_DEFAULT_OPUS_MODEL'
  return null
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

function buildClaudeCCSwitchConfig(baseUrl: string, apiKey: string, model?: string) {
  const env: Record<string, string> = buildClaudeSettingsEnv(baseUrl, apiKey)
  const selectedModel = model?.trim()
  if (selectedModel) {
    env.ANTHROPIC_MODEL = selectedModel
    const defaultModelEnv = resolveClaudeDefaultModelEnv(selectedModel)
    if (defaultModelEnv) {
      env[defaultModelEnv] = selectedModel
    }
  }
  return { env }
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

export function buildCodexConfigSnippet(providerName: string, baseUrl: string) {
  const provider = providerName.trim() || 'provider'
  return [
    `model_provider = ${tomlString(provider)}`,
    `model = ${tomlString(CODEX_MODEL)}`,
    `review_model = ${tomlString(CODEX_MODEL)}`,
    `model_reasoning_effort = "xhigh"`,
    `disable_response_storage = true`,
    `network_access = "enabled"`,
    `windows_wsl_setup_acknowledged = true`,
    `model_context_window = 1000000`,
    `model_auto_compact_token_limit = 900000`,
    ``,
    `[model_providers.${tomlTableKey(provider)}]`,
    `name = ${tomlString(provider)}`,
    `base_url = ${tomlString(baseUrl)}`,
    `wire_api = "responses"`,
    `requires_openai_auth = true`,
  ].join('\n')
}

export function buildCodexAuthSnippet(apiKey: string) {
  return JSON.stringify({ OPENAI_API_KEY: apiKey })
}

export function buildClaudeSettingsSnippet(baseUrl: string, apiKey: string) {
  return JSON.stringify({
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
  const model = input.model?.trim()
  if (profile.platform === 'gemini') {
    return [
      '# Hermes Agent does not expose a stable native Gemini provider mode through CC Switch import.',
      '# Use the Hermes setup portal and enter these current access-group values:',
      'hermes setup --portal',
      '',
      `provider_name=${shellString(providerName)}`,
      `provider_protocol=${shellString(profile.displayName)}`,
      `base_url=${shellString(input.baseUrl)}`,
      `api_key=${shellString(input.apiKey)}`,
      ...(model ? [`model=${shellString(model)}`] : []),
    ].join('\n')
  }
  return [
    '# Add this provider through Hermes setup or config, using the same fields.',
    'hermes setup --portal',
    '',
    'custom_providers:',
    `  - name: ${JSON.stringify(providerName)}`,
    `    base_url: ${JSON.stringify(input.baseUrl)}`,
    `    api_key: ${JSON.stringify(input.apiKey)}`,
    `    api_mode: ${JSON.stringify(profile.hermesApiMode)}`,
    ...(model ? [
      '    models:',
      `      ${JSON.stringify(model)}:`,
      '        context_length: 200000',
    ] : []),
  ].join('\n')
}

function buildOpenClawAgentSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  const model = input.model?.trim()
  return JSON.stringify({
    baseUrl: input.baseUrl,
    apiKey: input.apiKey,
    api: profile.openClawApi,
    ...(model ? { models: [{ id: model, name: model }] } : {}),
  }, null, 2)
}

function buildCustomAgentJSONSnippet(input: ManualConfigSnippetInput, profile: AgentPlatformProfile) {
  return JSON.stringify({
    platform: profile.platform,
    protocol: profile.displayName,
    base_url: input.baseUrl,
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
          base_url: input.baseUrl,
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
      path: profile.platform === 'gemini' ? 'Hermes Agent setup values' : '~/.hermes/config.yaml',
      body: buildHermesAgentSnippet(input, profile),
      containsSecret: true,
    },
    {
      key: 'openclaw-agent',
      path: '~/.openclaw/openclaw.json provider entry',
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

export function buildManualConfigSnippets(input: ManualConfigSnippetInput): ManualConfigSnippet[] {
  if (isAgentAccessGroup(input.groupName)) {
    return buildAgentManualConfigSnippets(input)
  }
  const platform = input.platform.trim().toLowerCase()
  if (platform === 'openai') {
    return [
      {
        key: 'codex-config',
        path: '~/.codex/config.toml',
        body: buildCodexConfigSnippet(input.providerName, input.baseUrl),
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
  if (normalized === 'openai') return 'codex'
  if (normalized === 'anthropic') return 'claude'
  if (normalized === 'gemini') return 'gemini'
  return null
}

export function resolveCCSwitchAppsForGroup(platform: string, groupName?: string | null): CCSwitchApp[] {
  if (isAgentAccessGroup(groupName)) {
    return normalizeAgentPlatform(platform) ? ['hermes', 'openclaw'] : []
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
      config: encodeBase64(JSON.stringify(buildClaudeCCSwitchConfig(input.endpoint, input.apiKey, input.model))),
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
    endpoint: input.endpoint,
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

export function buildDiscoverCommand(origin: string, providerName: string) {
  return `ae-cli discover --provider ${providerName}`
}

export function buildHooksGlobalCommand() {
  return 'ae-cli hooks enable --global'
}

export function buildRepoInitCommand() {
  return 'ae-cli init'
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
