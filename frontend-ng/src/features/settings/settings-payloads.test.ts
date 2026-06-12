import { describe, expect, test } from 'vitest'
import {
  buildCredentialPayload,
  buildLDAPForm,
  buildLDAPPayload,
  buildRelayPayload,
  buildScmProviderPayload,
  buildSettingsSectionSearch,
  settingsCredentialKindLabel,
  settingsSectionMeta,
  settingsSectionFromSearch,
  settingsScmProviderTypeLabel,
  type CredentialFormState,
  type LDAPFormState,
  type RelayFormState,
  settingsSections,
  type ScmFormState
} from './settings-payloads'

describe('buildRelayPayload', () => {
  const form: RelayFormState = {
    name: 'relay-main',
    display_name: 'Relay Main',
    base_url: 'https://relay.example.com',
    admin_api_key: '',
    is_primary: true,
    enabled: true
  }

  test('keeps provider name and admin key when creating relay providers', () => {
    expect(buildRelayPayload({ ...form, admin_api_key: 'admin-secret' }, 'create')).toEqual({
      name: 'relay-main',
      display_name: 'Relay Main',
      base_url: 'https://relay.example.com',
      admin_api_key: 'admin-secret',
      is_primary: true,
      enabled: true
    })
  })

  test('omits blank admin key and immutable name when editing relay providers', () => {
    expect(buildRelayPayload(form, 'edit')).toEqual({
      display_name: 'Relay Main',
      base_url: 'https://relay.example.com',
      admin_api_key: undefined,
      is_primary: true,
      enabled: true
    })
  })
})

describe('buildScmProviderPayload', () => {
  const form: ScmFormState = {
    name: 'GitHub',
    type: 'github',
    base_url: 'https://api.github.com',
    api_credential_id: '3',
    clone_protocol: 'https',
    clone_credential_id: '',
    ssh_host: 'github.com'
  }

  test('includes provider type only for create payloads', () => {
    expect(buildScmProviderPayload(form, 'create')).toMatchObject({ type: 'github' })
    expect(buildScmProviderPayload(form, 'edit')).not.toHaveProperty('type')
  })

  test('clears clone credential when clone protocol is https', () => {
    expect(buildScmProviderPayload(form, 'edit')).toMatchObject({
      clone_protocol: 'https',
      clone_credential_id: null
    })
  })
})

describe('buildCredentialPayload', () => {
  const form: CredentialFormState = {
    name: 'api-token',
    description: 'API token',
    kind: 'secret_text',
    text: '',
    username: '',
    password: '',
    private_key: '',
    passphrase: ''
  }

  test('sends secret values for new credentials', () => {
    expect(buildCredentialPayload({ ...form, text: 'secret' }, 'create')).toEqual({
      name: 'api-token',
      description: 'API token',
      kind: 'secret_text',
      payload: { text: 'secret' }
    })
  })

  test('does not overwrite an existing secret with a blank value during edit', () => {
    expect(buildCredentialPayload(form, 'edit')).toEqual({
      name: 'api-token',
      description: 'API token'
    })
  })

  test('does not send partial username password secrets during edit', () => {
    expect(buildCredentialPayload({ ...form, kind: 'username_password', username: 'alice' }, 'edit')).toEqual({
      name: 'api-token',
      description: 'API token'
    })
  })
})

describe('LDAP settings payloads', () => {
  test('maps backend LDAP config into an editable form without exposing masked password', () => {
    expect(buildLDAPForm({
      url: 'ldap://ldap.example.com:389',
      base_dn: 'dc=example,dc=com',
      bind_dn: 'cn=reader,dc=example,dc=com',
      bind_password: '***',
      user_filter: '(uid=%s)',
      tls: true
    })).toEqual({
      url: 'ldap://ldap.example.com:389',
      base_dn: 'dc=example,dc=com',
      bind_dn: 'cn=reader,dc=example,dc=com',
      bind_password: '',
      user_filter: '(uid=%s)',
      tls: true
    })
  })

  test('builds LDAP save and test payloads using backend field names', () => {
    const form: LDAPFormState = {
      url: 'ldap://ldap.example.com:389',
      base_dn: 'dc=example,dc=com',
      bind_dn: 'cn=reader,dc=example,dc=com',
      bind_password: '',
      user_filter: '(mail=%s)',
      tls: false
    }

    expect(buildLDAPPayload(form)).toEqual({
      url: 'ldap://ldap.example.com:389',
      base_dn: 'dc=example,dc=com',
      bind_dn: 'cn=reader,dc=example,dc=com',
      bind_password: '',
      user_filter: '(mail=%s)',
      tls: false
    })
  })
})

describe('settings section search state', () => {
  test('uses AI services as the default section for missing or invalid search values', () => {
    expect(settingsSectionFromSearch({})).toBe('ai-services')
    expect(settingsSectionFromSearch({ section: 'unknown' })).toBe('ai-services')
  })

  test('accepts every visible settings section from URL search', () => {
    for (const section of settingsSections) {
      expect(settingsSectionFromSearch({ section })).toBe(section)
    }
  })

  test('keeps settings sections in the reference rail order', () => {
    expect(settingsSections).toEqual([
      'ai-services',
      'code-platforms',
      'advanced-credentials',
      'organization-login',
      'deployment-runtime'
    ])
  })

  test('omits default section from URL search and keeps non-default sections', () => {
    expect(buildSettingsSectionSearch('ai-services')).toEqual({})
    expect(buildSettingsSectionSearch('organization-login')).toEqual({ section: 'organization-login' })
  })

  test('provides stable metadata for every settings section', () => {
    expect(settingsSectionMeta['ai-services']).toMatchObject({
      labelKey: 'settings.aiServices',
      descriptionKey: 'settings.relayProvidersDescription',
      addKey: 'settings.addRelayProvider'
    })
    expect(settingsSectionMeta['code-platforms']).toMatchObject({
      labelKey: 'settings.codePlatforms',
      descriptionKey: 'settings.scmProvidersDescription',
      addKey: 'settings.addScmProvider'
    })
    expect(settingsSectionMeta['advanced-credentials']).toMatchObject({
      labelKey: 'settings.advancedCredentials',
      descriptionKey: 'settings.advancedCredentialsDescription',
      addKey: 'settings.addCredential'
    })
    expect(settingsSectionMeta['organization-login']).toMatchObject({
      labelKey: 'settings.organizationLogin',
      descriptionKey: 'settings.ldapLoginBehavior'
    })
    expect(settingsSectionMeta['deployment-runtime']).toMatchObject({
      labelKey: 'settings.deploymentRuntime',
      descriptionKey: 'settings.currentBackendDeployment'
    })
  })

  test('maps scm provider types and credential kinds to stable display keys', () => {
    expect(settingsScmProviderTypeLabel('github')).toBe('settings.scmTypeGithub')
    expect(settingsScmProviderTypeLabel('bitbucket')).toBe('settings.scmTypeBitbucket')
    expect(settingsScmProviderTypeLabel('unknown')).toBe('common.unknown')
    expect(settingsCredentialKindLabel('secret_text')).toBe('settings.secretTextKind')
    expect(settingsCredentialKindLabel('username_password')).toBe('settings.usernamePasswordKind')
    expect(settingsCredentialKindLabel('ssh_username_with_private_key')).toBe('settings.sshPrivateKeyKind')
  })
})
