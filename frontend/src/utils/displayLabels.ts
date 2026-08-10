import type { MessageKey } from '@/i18n'

type Translate = (key: MessageKey, params?: Record<string, string | number>) => string

export function scmProviderTypeLabel(type: string | null | undefined, t: Translate): string {
  switch (type) {
    case 'github':
      return 'GitHub'
    case 'bitbucket_server':
      return 'Bitbucket Server'
    default:
      return t('display.unknown')
  }
}

export function scmProviderStatusLabel(status: string | null | undefined, t: Translate): string {
  switch (status) {
    case 'active':
      return t('display.status.active')
    case 'inactive':
      return t('display.status.inactive')
    case 'error':
      return t('display.status.error')
    default:
      return t('display.unknown')
  }
}

export function repositoryStatusLabel(status: string | null | undefined, t: Translate): string {
  switch (status) {
    case 'active':
      return t('display.status.active')
    case 'webhook_failed':
      return t('display.repoStatus.webhookFailed')
    case 'inactive':
      return t('display.status.inactive')
    default:
      return t('display.unknown')
  }
}

export function pullRequestStatusLabel(status: string | null | undefined, t: Translate): string {
  switch (status) {
    case 'open':
      return t('display.prStatus.open')
    case 'merged':
      return t('display.prStatus.merged')
    case 'closed':
      return t('display.prStatus.closed')
    default:
      return t('display.unknown')
  }
}

export function subscriptionResultStatusLabel(status: string | null | undefined, t: Translate): string {
  switch (status) {
    case 'success':
      return t('display.subscriptionResult.succeeded')
    case 'skipped':
      return t('display.subscriptionResult.skipped')
    case 'failed':
      return t('display.subscriptionResult.failed')
    default:
      return t('display.unknown')
  }
}

export function credentialKindLabel(kind: string | null | undefined, t: Translate): string {
  switch (kind) {
    case 'secret_text':
      return t('settings.secretTextKind')
    case 'username_password':
      return t('settings.usernamePasswordKind')
    case 'ssh_username_with_private_key':
      return t('settings.sshPrivateKeyKind')
    default:
      return t('display.unknown')
  }
}

export function credentialSummaryLabel(summary: Record<string, unknown> | null | undefined, t: Translate): string {
  if (typeof summary?.preview === 'string' && summary.preview) return summary.preview

  const parts: string[] = []
  if (typeof summary?.username === 'string' && summary.username) {
    parts.push(t('display.credential.username', { value: summary.username }))
  }
  if (typeof summary?.password_preview === 'string' && summary.password_preview) {
    parts.push(t('display.credential.password', { value: summary.password_preview }))
  }
  if (typeof summary?.private_key_preview === 'string' && summary.private_key_preview) {
    parts.push(t('display.credential.privateKeyConfigured'))
  }
  if (typeof summary?.has_passphrase === 'boolean') {
    parts.push(t(summary.has_passphrase
      ? 'display.credential.passphraseConfigured'
      : 'display.credential.passphraseNotConfigured'))
  }
  if (typeof summary?.fingerprint === 'string' && summary.fingerprint) {
    parts.push(t('display.credential.fingerprint', { value: summary.fingerprint }))
  }
  return parts.join(' · ') || t('display.none')
}

export function authSourceLabel(source: string | null | undefined, t: Translate): string {
  switch (source) {
    case 'ldap':
      return t('display.authSource.ldap')
    case 'relay_sso':
    case 'sub2api_sso':
      return t('display.authSource.relaySso')
    case 'dev':
      return t('display.authSource.dev')
    default:
      return t('display.unknown')
  }
}

export function userRoleLabel(role: string | null | undefined, t: Translate): string {
  switch (role) {
    case 'admin':
      return t('display.userRole.admin')
    case 'user':
      return t('display.userRole.user')
    default:
      return t('display.unknown')
  }
}

export function offboardingReasonLabel(reason: string | null | undefined, t: Translate): string {
  if (reason === 'missing_from_latest_full_company_directory') {
    return t('directoryOffboarding.reasonMissingFromDirectory')
  }
  return t('display.unknown')
}

export function offboardingStatusLabel(status: string | null | undefined, t: Translate): string {
  switch (status) {
    case 'running':
      return t('directoryOffboarding.statusRunning')
    case 'succeeded':
      return t('directoryOffboarding.statusSucceeded')
    case 'failed':
      return t('directoryOffboarding.statusFailed')
    case 'partial_failed':
      return t('directoryOffboarding.statusPartialFailed')
    default:
      return t('directoryOffboarding.statusPending')
  }
}
