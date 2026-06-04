import { describe, expect, it } from 'vitest'
import {
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildDoctorCommand,
  buildGithubConnectivityCommand,
  buildHooksGlobalCommand,
  buildHooksStatusUploadsCommand,
  buildInstallCommand,
  buildLoginCommand,
  buildPreferredGithubConnectivityCommand,
  buildPreferredInstallCommand,
  buildRepoInitCommand,
  buildSyncCommand,
  buildWindowsGithubConnectivityCommand,
  buildWindowsInstallCommand,
  detectInstallPlatform,
} from '@/utils/userSetupReview'

describe('userSetupReview command builders', () => {
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
