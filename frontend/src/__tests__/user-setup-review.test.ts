import { describe, expect, it } from 'vitest'
import {
  buildCCSwitchProviderImportLink,
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildDoctorCommand,
  resolveCCSwitchAppForPlatform,
  buildClaudeSettingsSnippet,
  buildCodexAuthSnippet,
  buildCodexConfigSnippet,
  buildGeminiEnvSnippet,
  buildGeminiModelSnippet,
  buildGeminiReloadSnippet,
  buildGithubConnectivityCommand,
  buildHooksGlobalCommand,
  buildHooksStatusUploadsCommand,
  buildInstallCommand,
  buildManualConfigSnippets,
  buildLoginCommand,
  buildPreferredGithubConnectivityCommand,
  buildPreferredInstallCommand,
  buildRepoInitCommand,
  buildSyncCommand,
  buildWindowsGithubConnectivityCommand,
  buildWindowsInstallCommand,
  detectInstallPlatform,
} from '@/utils/userSetupReview'

function decodeCCSwitchConfig(link: string) {
  const url = new URL(link)
  const config = url.searchParams.get('config')
  if (!config) {
    throw new Error('missing config parameter')
  }
  return JSON.parse(Buffer.from(config, 'base64').toString('utf8'))
}

describe('userSetupReview command builders', () => {
  it('builds Codex manual config snippets equivalent to ae-cli discover', () => {
    expect(buildCodexConfigSnippet('prod', 'https://prod.example.com')).toContain([
      'model_provider = "prod"',
      'model = "gpt-5.4"',
      'review_model = "gpt-5.4"',
      'model_reasoning_effort = "xhigh"',
      'disable_response_storage = true',
      'network_access = "enabled"',
      'windows_wsl_setup_acknowledged = true',
      'model_context_window = 1000000',
      'model_auto_compact_token_limit = 900000',
      '',
      '[model_providers.prod]',
      'name = "prod"',
      'base_url = "https://prod.example.com"',
      'wire_api = "responses"',
      'requires_openai_auth = true',
    ].join('\n'))
    expect(buildCodexAuthSnippet('sk-openai')).toBe('{"OPENAI_API_KEY":"sk-openai"}')
  })

  it('builds Claude manual settings equivalent to ae-cli discover', () => {
    const snippet = buildClaudeSettingsSnippet('https://prod.example.com', 'sk-claude')
    expect(snippet).toContain('"ANTHROPIC_BASE_URL": "https://prod.example.com"')
    expect(snippet).toContain('"ANTHROPIC_AUTH_TOKEN": "sk-claude"')
    expect(snippet).toContain('"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"')
    expect(snippet).toContain('"CLAUDE_CODE_ATTRIBUTION_HEADER": "0"')
    expect(snippet).not.toContain('ANTHROPIC_API_KEY')
  })

  it('builds Gemini manual shell guidance equivalent to ae-cli discover output', () => {
    expect(buildGeminiEnvSnippet('https://prod.example.com', 'sk-gemini')).toContain([
      'export GEMINI_API_KEY="sk-gemini"',
      'export GOOGLE_GEMINI_BASE_URL="https://prod.example.com"',
    ].join('\n'))
    expect(buildGeminiReloadSnippet()).toContain('case "${SHELL##*/}" in')
    expect(buildGeminiReloadSnippet()).toContain('zsh) rc_file="$HOME/.zshrc" ;;')
    expect(buildGeminiReloadSnippet()).toContain('bash) rc_file="$HOME/.bashrc" ;;')
    expect(buildGeminiReloadSnippet()).toContain('*) rc_file="$HOME/.profile" ;;')
    expect(buildGeminiReloadSnippet()).toContain('[ -f "$rc_file" ] && source "$rc_file"')
    expect(buildGeminiReloadSnippet()).not.toContain('source "$HOME/.zshrc"\nsource "$HOME/.bashrc"')
    expect(buildGeminiModelSnippet()).toContain('export GEMINI_MODEL="gemini-3.1-pro-preview"')
    expect(buildGeminiModelSnippet()).toContain('# Do not manually switch models inside Gemini')
  })

  it('builds platform-specific manual config card metadata', () => {
    expect(buildManualConfigSnippets({
      providerName: 'prod',
      baseUrl: 'https://prod.example.com',
      platform: 'openai',
      apiKey: 'sk-openai',
    }).map((snippet) => snippet.path)).toEqual(['~/.codex/config.toml', '~/.codex/auth.json'])

    expect(buildManualConfigSnippets({
      providerName: 'prod',
      baseUrl: 'https://prod.example.com',
      platform: 'anthropic',
      apiKey: 'sk-claude',
    }).map((snippet) => snippet.path)).toEqual(['~/.claude/settings.json'])

    expect(buildManualConfigSnippets({
      providerName: 'prod',
      baseUrl: 'https://prod.example.com',
      platform: 'gemini',
      apiKey: 'sk-gemini',
    }).map((snippet) => snippet.path)).toEqual(['~/.ae-cli/env.sh', 'Shell reload', 'Current shell'])
  })

  it('maps supported platforms to CC Switch apps', () => {
    expect(resolveCCSwitchAppForPlatform('openai')).toBe('codex')
    expect(resolveCCSwitchAppForPlatform('anthropic')).toBe('claude')
    expect(resolveCCSwitchAppForPlatform('gemini')).toBe('gemini')
    expect(resolveCCSwitchAppForPlatform('unknown')).toBeNull()
  })

  it('builds an app-specific CC Switch provider import link', () => {
    const link = buildCCSwitchProviderImportLink({
      app: 'claude',
      name: 'Production / Group Alpha',
      endpoint: 'https://prod.example.com',
      apiKey: 'sk-claude',
    })
    const url = new URL(link)
    expect(`${url.protocol}//${url.host}${url.pathname}`).toBe('ccswitch://v1/import')
    expect(url.searchParams.get('resource')).toBe('provider')
    expect(url.searchParams.get('app')).toBe('claude')
    expect(url.searchParams.get('name')).toBe('Production / Group Alpha')
    expect(url.searchParams.get('enabled')).toBe('true')
    expect(url.searchParams.get('configFormat')).toBe('json')
    expect(url.searchParams.get('endpoint')).toBeNull()
    expect(url.searchParams.get('apiKey')).toBeNull()
    expect(url.searchParams.get('model')).toBeNull()

    expect(decodeCCSwitchConfig(link)).toEqual({
      env: {
        ANTHROPIC_BASE_URL: 'https://prod.example.com',
        ANTHROPIC_AUTH_TOKEN: 'sk-claude',
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
        CLAUDE_CODE_ATTRIBUTION_HEADER: '0',
      },
    })
  })

  it('includes an explicit model when provided for a CC Switch import', () => {
    const link = buildCCSwitchProviderImportLink({
      app: 'codex',
      name: 'Production / Group Alpha',
      endpoint: 'https://prod.example.com',
      apiKey: 'sk-openai',
      model: 'gpt-5.4',
    })
    const url = new URL(link)
    expect(url.searchParams.get('app')).toBe('codex')
    expect(url.searchParams.get('configFormat')).toBe('json')
    expect(url.searchParams.get('endpoint')).toBeNull()
    expect(url.searchParams.get('apiKey')).toBeNull()
    expect(url.searchParams.get('model')).toBeNull()

    expect(decodeCCSwitchConfig(link)).toEqual({
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

  it('stores the selected Claude model inside the imported config', () => {
    const link = buildCCSwitchProviderImportLink({
      app: 'claude',
      name: 'Production / Group Alpha',
      endpoint: 'https://prod.example.com',
      apiKey: 'sk-claude',
      model: 'claude-sonnet-4-6',
    })

    expect(decodeCCSwitchConfig(link)).toEqual({
      env: {
        ANTHROPIC_BASE_URL: 'https://prod.example.com',
        ANTHROPIC_AUTH_TOKEN: 'sk-claude',
        ANTHROPIC_MODEL: 'claude-sonnet-4-6',
        ANTHROPIC_DEFAULT_SONNET_MODEL: 'claude-sonnet-4-6',
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1',
        CLAUDE_CODE_ATTRIBUTION_HEADER: '0',
      },
    })
  })

  it('stores Gemini providers as a config import payload', () => {
    const link = buildCCSwitchProviderImportLink({
      app: 'gemini',
      name: 'Production / Group Alpha',
      endpoint: 'https://prod.example.com',
      apiKey: 'sk-gemini',
      model: 'gemini-3.1-pro-preview',
    })
    const url = new URL(link)
    expect(url.searchParams.get('app')).toBe('gemini')
    expect(url.searchParams.get('configFormat')).toBe('json')
    expect(url.searchParams.get('endpoint')).toBeNull()
    expect(url.searchParams.get('apiKey')).toBeNull()
    expect(url.searchParams.get('model')).toBeNull()

    expect(decodeCCSwitchConfig(link)).toEqual({
      GEMINI_API_KEY: 'sk-gemini',
      GOOGLE_GEMINI_BASE_URL: 'https://prod.example.com',
      GEMINI_MODEL: 'gemini-3.1-pro-preview',
    })
  })

  it('buildDiscoverCommand uses the selected provider', () => {
    expect(buildDiscoverCommand('https://ae.example.com', 'sub2api-prod')).toBe(
      'ae-cli discover --provider sub2api-prod'
    )
  })

  it('buildInstallCommand passes AE_CLI_INSTALL_SERVER_URL to bash', () => {
    expect(buildInstallCommand('https://ae.example.com')).toBe(
      'curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=https://ae.example.com bash'
    )
  })

  it('builds GitHub release connectivity checks before installing from GitHub releases', () => {
    expect(buildGithubConnectivityCommand()).toBe(
      'curl -fsSI --connect-timeout 5 https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest'
    )
    expect(buildWindowsGithubConnectivityCommand()).toBe(
      'iwr -UseB -Method Head https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest'
    )
    expect(buildPreferredGithubConnectivityCommand('windows')).toContain('iwr -UseB')
    expect(buildPreferredGithubConnectivityCommand('shell')).toContain('curl -fsSI')
  })

  it('buildWindowsInstallCommand passes AE_CLI_INSTALL_SERVER_URL to PowerShell', () => {
    expect(buildWindowsInstallCommand('https://ae.example.com')).toBe(
      '$env:AE_CLI_INSTALL_SERVER_URL = "https://ae.example.com"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex'
    )
  })

  it('detects the preferred install command from the browser platform', () => {
    expect(detectInstallPlatform({ platform: 'Win32', userAgent: '' })).toBe('windows')
    expect(buildPreferredInstallCommand('https://ae.example.com', 'windows')).toContain('install.ps1')

    expect(detectInstallPlatform({ platform: 'MacIntel', userAgent: '' })).toBe('shell')
    expect(detectInstallPlatform({ platform: 'Linux x86_64', userAgent: '' })).toBe('shell')
    expect(buildPreferredInstallCommand('https://ae.example.com', 'shell')).toContain('install.sh')
  })

  it('buildLoginCommand and buildDeviceLoginCommand use the installed server config', () => {
    expect(buildLoginCommand('https://ae.example.com')).toBe('ae-cli login')
    expect(buildDeviceLoginCommand('https://ae.example.com')).toBe('ae-cli login --device')
  })

  it('builds machine and repo setup commands', () => {
    expect(buildHooksGlobalCommand()).toBe('ae-cli hooks enable --global')
    expect(buildRepoInitCommand()).toBe('ae-cli init')
    expect(buildDoctorCommand()).toBe('ae-cli doctor')
    expect(buildSyncCommand()).toBe('ae-cli sync')
    expect(buildHooksStatusUploadsCommand()).toBe('ae-cli hooks status --uploads')
  })
})
