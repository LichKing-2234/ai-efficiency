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

  it('buildDiscoverCommand includes the selected provider and current origin', () => {
    expect(buildDiscoverCommand('https://ae.example.com', 'sub2api-prod')).toBe(
      'ae-cli --server https://ae.example.com discover --provider sub2api-prod'
    )
  })

  it('buildInstallCommand injects AE_CLI_INSTALL_SERVER_URL', () => {
    expect(buildInstallCommand('https://ae.example.com')).toBe(
      'AE_CLI_INSTALL_SERVER_URL=https://ae.example.com curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash'
    )
  })

  it('buildLoginCommand and buildDeviceLoginCommand include the current origin', () => {
    expect(buildLoginCommand('https://ae.example.com')).toBe('ae-cli --server https://ae.example.com login')
    expect(buildDeviceLoginCommand('https://ae.example.com')).toBe('ae-cli --server https://ae.example.com login --device')
  })
})
