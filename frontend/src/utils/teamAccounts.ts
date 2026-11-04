import type { MessageKey } from '@/i18n'

export type TeamAccountTagType = 'success' | 'warning' | 'danger' | 'info'

const statusLabelKeys: Record<string, MessageKey> = {
  active: 'teamAccounts.status.active',
  suspended: 'teamAccounts.status.suspended',
  orphaned: 'teamAccounts.status.orphaned',
}

const keyStatusLabelKeys: Record<string, MessageKey> = {
  active: 'teamAccounts.keys.status.active',
  revoked: 'teamAccounts.keys.status.revoked',
}

const roleLabelKeys: Record<string, MessageKey> = {
  owner: 'teamAccounts.role.owner',
  manager: 'teamAccounts.role.manager',
}

const eventLabelKeys: Record<string, MessageKey> = {
  account_created: 'teamAccounts.event.account_created',
  key_created: 'teamAccounts.event.key_created',
  key_revoked: 'teamAccounts.event.key_revoked',
  credentials_revealed: 'teamAccounts.event.credentials_revealed',
  manager_added: 'teamAccounts.event.manager_added',
  manager_removed: 'teamAccounts.event.manager_removed',
  owner_transferred: 'teamAccounts.event.owner_transferred',
  account_suspended: 'teamAccounts.event.account_suspended',
  account_restored: 'teamAccounts.event.account_restored',
  account_orphaned: 'teamAccounts.event.account_orphaned',
}

export function teamAccountStatusLabelKey(status: string | null | undefined): MessageKey {
  return statusLabelKeys[status ?? ''] ?? 'teamAccounts.status.unknown'
}

export function teamAccountKeyStatusLabelKey(status: string | null | undefined): MessageKey {
  return keyStatusLabelKeys[status ?? ''] ?? 'teamAccounts.status.unknown'
}

export function teamAccountRoleLabelKey(role: string | null | undefined): MessageKey {
  return roleLabelKeys[role ?? ''] ?? 'teamAccounts.role.unknown'
}

export function teamAccountEventLabelKey(action: string | null | undefined): MessageKey {
  return eventLabelKeys[action ?? ''] ?? 'teamAccounts.event.unknown'
}

export function teamAccountStatusTagType(status: string | null | undefined): TeamAccountTagType {
  if (status === 'active') return 'success'
  if (status === 'suspended') return 'warning'
  if (status === 'orphaned') return 'danger'
  return 'info'
}

export function teamAccountKeyStatusTagType(status: string | null | undefined): TeamAccountTagType {
  if (status === 'active') return 'success'
  if (status === 'revoked') return 'info'
  return 'warning'
}

export function formatTeamAccountCost(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return `$${value.toFixed(2)}`
}

export function formatTeamAccountNumber(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return value.toLocaleString()
}

export function teamAccountErrorMessage(error: unknown, fallback: string): string {
  if (typeof error !== 'object' || error == null) return fallback
  const message = (error as { response?: { data?: { message?: unknown } } }).response?.data?.message
  if (typeof message === 'string' && message.trim()) return message
  const ownMessage = (error as { message?: unknown }).message
  if (typeof ownMessage === 'string' && ownMessage.trim()) return ownMessage
  return fallback
}

export function formatTeamAccountTimestamp(value: string | null | undefined, locale: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}
