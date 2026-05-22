import { describe, expect, it } from 'vitest'
import {
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildInstallCommand,
  buildLoginCommand,
  reviewVerifyOutput,
} from '@/utils/userSetupReview'

describe('userSetupReview', () => {
  it('reviewVerifyOutput flags discover output without the selected provider as needs_attention', () => {
    const result = reviewVerifyOutput({
      selectedProviderName: 'sub2api-prod',
      versionOutput: 'ae-cli version v0.2.0',
      discoverOutput: 'Would update ~/.codex/config.toml with provider staging',
      doctorOutput: 'all good',
    })
    expect(result.discover.status).toBe('needs_attention')
  })

  it('reviewVerifyOutput marks happy-path outputs as looks_good', () => {
    const result = reviewVerifyOutput({
      selectedProviderName: 'sub2api-prod',
      versionOutput: 'ae-cli version v0.2.0',
      discoverOutput: 'selected provider sub2api-prod; would update ~/.codex/config.toml and ~/.ae-cli/env.sh',
      doctorOutput: 'Doctor OK: auth ready',
    })
    expect(result.version.status).toBe('looks_good')
    expect(result.discover.status).toBe('looks_good')
    expect(result.doctor.status).toBe('looks_good')
  })

  it('reviewVerifyOutput marks doctor output with error keywords as needs_attention', () => {
    const result = reviewVerifyOutput({
      selectedProviderName: 'sub2api-prod',
      versionOutput: '',
      discoverOutput: '',
      doctorOutput: 'ERROR: unauthorized request to /auth/me',
    })
    expect(result.doctor.status).toBe('needs_attention')
  })

  it('buildDiscoverCommand uses the installed server config and selected provider', () => {
    expect(buildDiscoverCommand('https://ae.example.com', 'sub2api-prod')).toBe(
      'ae-cli discover --provider sub2api-prod'
    )
  })

  it('buildInstallCommand passes AE_CLI_INSTALL_SERVER_URL to bash', () => {
    expect(buildInstallCommand('https://ae.example.com')).toBe(
      'curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=https://ae.example.com bash'
    )
  })

  it('buildLoginCommand and buildDeviceLoginCommand use the installed server config', () => {
    expect(buildLoginCommand('https://ae.example.com')).toBe('ae-cli login')
    expect(buildDeviceLoginCommand('https://ae.example.com')).toBe('ae-cli login --device')
  })
})
