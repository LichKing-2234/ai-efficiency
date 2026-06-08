import { describe, expect, test } from 'vitest'
import { buildRepoBindingPayload } from './repo-binding'

describe('buildRepoBindingPayload', () => {
  test('builds an SCM provider binding payload from a selected provider id', () => {
    expect(buildRepoBindingPayload('42')).toEqual({ scm_provider_id: 42 })
  })

  test('builds an explicit clear payload when no provider is selected', () => {
    expect(buildRepoBindingPayload('')).toEqual({ clear_scm_provider: true })
  })
})
