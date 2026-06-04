export type InstallPlatform = 'shell' | 'windows'

export type ManualConfigSnippetKey =
  | 'codex-config'
  | 'codex-auth'
  | 'claude-settings'
  | 'gemini-env'
  | 'gemini-reload'
  | 'gemini-model'

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
}

const GITHUB_RELEASE_API_URL = 'https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest'
const CODEX_MODEL = 'gpt-5.4'
const GEMINI_MODEL = 'gemini-3.1-pro-preview'

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
    env: {
      ANTHROPIC_BASE_URL: baseUrl,
      ANTHROPIC_AUTH_TOKEN: apiKey,
      CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
      CLAUDE_CODE_ATTRIBUTION_HEADER: '0',
    },
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
    'Do not manually switch models inside Gemini after setting this value.',
  ].join('\n')
}

export function buildManualConfigSnippets(input: ManualConfigSnippetInput): ManualConfigSnippet[] {
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
