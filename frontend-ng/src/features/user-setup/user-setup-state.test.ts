import { describe, expect, test } from 'vitest'
import {
  buildDiscoverCommand,
  buildInstallCommand,
  buildProviderTestRequest,
  chooseDefaultSelection,
  maskApiKey,
  modelLabel,
  visibleCredentialSecret
} from './user-setup-state'
import type { UserProviderSummary } from '@/lib/api/types'

const providers: UserProviderSummary[] = [
  {
    id: 1,
    name: 'secondary',
    display_name: 'Secondary',
    base_url: 'https://secondary.example.com',
    default_model: 'gpt-5.4',
    is_primary: false,
    groups: [
      {
        group_id: 'secondary-group',
        group_name: 'Secondary Group',
        platform: 'openai',
        credential: { state: 'missing' }
      }
    ]
  },
  {
    id: 2,
    name: 'primary',
    display_name: 'Primary',
    base_url: 'https://primary.example.com',
    default_model: 'claude-sonnet',
    is_primary: true,
    groups: [
      {
        group_id: '42',
        group_name: 'Group Alpha',
        platform: 'anthropic',
        credential: { state: 'existing_hidden', key: 'sk-live-secret-value' }
      },
      {
        group_id: '43',
        group_name: 'Group Beta',
        platform: 'openai',
        credential: { state: 'missing' }
      }
    ]
  }
]

describe('chooseDefaultSelection', () => {
  test('selects the primary provider and its first group', () => {
    expect(chooseDefaultSelection(providers)).toEqual({ providerId: 2, groupId: '42' })
  })

  test('preserves a valid provider and group selection', () => {
    expect(chooseDefaultSelection(providers, { providerId: 2, groupId: '43' })).toEqual({ providerId: 2, groupId: '43' })
  })

  test('falls back to the provider first group when current group is invalid', () => {
    expect(chooseDefaultSelection(providers, { providerId: 2, groupId: 'missing' })).toEqual({ providerId: 2, groupId: '42' })
  })
})

describe('credential helpers', () => {
  test('prefers session secret over masked backend key', () => {
    const group = providers[1].groups[0]
    expect(visibleCredentialSecret(2, group, { '2:42': 'fresh-secret' })).toBe('fresh-secret')
  })

  test('masks long API keys without leaking the middle', () => {
    expect(maskApiKey('sk-live-secret-value')).toBe('sk-liv...alue')
  })
})

describe('provider test helpers', () => {
  test('builds backend test payload from selected group and form fields', () => {
    expect(buildProviderTestRequest(providers[1].groups[0], ' claude-sonnet ', ' Hello ')).toEqual({
      platform: 'anthropic',
      group_id: '42',
      model: 'claude-sonnet',
      prompt: 'Hello'
    })
  })

  test('falls back to Hi prompt when prompt is blank', () => {
    expect(buildProviderTestRequest(providers[1].groups[0], 'claude-sonnet', '   ')).toMatchObject({ prompt: 'Hi' })
  })

  test('formats display name with model id when they differ', () => {
    expect(modelLabel({ id: 'claude-sonnet', display_name: 'Claude Sonnet' })).toBe('Claude Sonnet (claude-sonnet)')
    expect(modelLabel({ id: 'gpt-5.4', display_name: 'gpt-5.4' })).toBe('gpt-5.4')
  })
})

describe('command helpers', () => {
  test('uses current frontend origin for installer server URL', () => {
    expect(buildInstallCommand('http://localhost:3000')).toContain('AE_CLI_INSTALL_SERVER_URL=http://localhost:3000')
  })

  test('builds discover command for the selected provider', () => {
    expect(buildDiscoverCommand('primary')).toBe('ae-cli discover --provider primary')
  })
})
