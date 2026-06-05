import type { CredentialPayload, LDAPSettings, RelayProviderPayload, SCMProviderPayload } from '@/lib/api/types'

type CredentialKind = CredentialPayload['kind']
type CredentialSecretFields = NonNullable<CredentialPayload['payload']>

export type SettingsFormMode = 'create' | 'edit'

export interface RelayFormState {
  name: string
  display_name: string
  base_url: string
  admin_api_key: string
  is_primary: boolean
  enabled: boolean
}

export interface ScmFormState {
  name: string
  type: string
  base_url: string
  api_credential_id: string
  clone_protocol: 'https' | 'ssh'
  clone_credential_id: string
  ssh_host: string
}

export interface CredentialFormState {
  name: string
  description?: string
  kind: CredentialKind
  text?: string
  username?: string
  password?: string
  private_key?: string
  passphrase?: string
}

export interface LDAPFormState {
  url: string
  base_dn: string
  bind_dn: string
  bind_password: string
  user_filter: string
  tls: boolean
}

export function buildRelayPayload(form: RelayFormState, mode: 'create'): RelayProviderPayload
export function buildRelayPayload(form: RelayFormState, mode: 'edit'): Partial<RelayProviderPayload>
export function buildRelayPayload(form: RelayFormState, mode: SettingsFormMode): RelayProviderPayload | Partial<RelayProviderPayload> {
  const shared = {
    display_name: form.display_name.trim(),
    base_url: form.base_url.trim(),
    admin_api_key: form.admin_api_key.trim() || undefined,
    is_primary: form.is_primary,
    enabled: form.enabled
  }
  if (mode === 'edit') return shared
  return {
    name: form.name.trim(),
    ...shared,
    admin_api_key: form.admin_api_key.trim()
  } satisfies RelayProviderPayload
}

export function buildScmProviderPayload(form: ScmFormState, mode: SettingsFormMode) {
  const shared: SCMProviderPayload = {
    name: form.name.trim(),
    base_url: form.base_url.trim(),
    api_credential_id: Number(form.api_credential_id),
    clone_protocol: form.clone_protocol,
    clone_credential_id: form.clone_protocol === 'ssh' && form.clone_credential_id ? Number(form.clone_credential_id) : null,
    ssh_host: form.ssh_host.trim() || null
  }
  return mode === 'create' ? { ...shared, type: form.type } : shared
}

export function buildCredentialPayload(form: CredentialFormState, mode: 'create'): CredentialPayload
export function buildCredentialPayload(form: CredentialFormState, mode: 'edit'): Partial<CredentialPayload>
export function buildCredentialPayload(form: CredentialFormState, mode: SettingsFormMode): CredentialPayload | Partial<CredentialPayload> {
  const base: CredentialPayload = {
    name: form.name.trim(),
    description: form.description?.trim() || undefined,
    kind: form.kind,
    payload: {}
  }
  const payload = credentialSecretPayload(form)
  if (mode === 'edit') {
    return payload ? { name: base.name, description: base.description, payload } : { name: base.name, description: base.description }
  }
  if (!payload) throw new Error('credential payload is required')
  return { ...base, payload }
}

function credentialSecretPayload(form: CredentialFormState): CredentialSecretFields | undefined {
  if (form.kind === 'secret_text') {
    const text = form.text?.trim()
    return text ? { text } : undefined
  }
  if (form.kind === 'username_password') {
    const username = form.username?.trim()
    const password = form.password?.trim()
    return username && password ? { username, password } : undefined
  }
  const username = form.username?.trim()
  const privateKey = form.private_key?.trim()
  const passphrase = form.passphrase?.trim()
  return username && privateKey ? { username, private_key: privateKey, passphrase } : undefined
}

export function buildLDAPForm(settings: LDAPSettings | null | undefined): LDAPFormState {
  return {
    url: settings?.url ?? '',
    base_dn: settings?.base_dn ?? '',
    bind_dn: settings?.bind_dn ?? '',
    bind_password: settings?.bind_password === '***' ? '' : settings?.bind_password ?? '',
    user_filter: settings?.user_filter ?? '(uid=%s)',
    tls: settings?.tls ?? false
  }
}

export function buildLDAPPayload(form: LDAPFormState): LDAPSettings {
  return {
    url: form.url.trim(),
    base_dn: form.base_dn.trim(),
    bind_dn: form.bind_dn.trim(),
    bind_password: form.bind_password.trim(),
    user_filter: form.user_filter.trim(),
    tls: form.tls
  }
}
