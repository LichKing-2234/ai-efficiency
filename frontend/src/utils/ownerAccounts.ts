import type { MessageKey } from '@/i18n'

type Translate = (key: MessageKey, params?: Record<string, string | number>) => string

export function ownerAccountDateTime(value: string | null | undefined, locale: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(locale)
}

export function ownerAccountCountdown(
  value: string | null | undefined,
  now: number,
  t: Translate,
): string {
  if (!value) return ''
  const target = new Date(value).getTime()
  if (Number.isNaN(target)) return ''
  const remaining = target - now
  if (remaining <= 0) return t('ownerAccounts.dueNow')
  const totalSeconds = Math.ceil(remaining / 1000)
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (days > 0) return t('ownerAccounts.durationDays', { days, hours })
  if (hours > 0) return t('ownerAccounts.durationHours', { hours, minutes })
  if (minutes > 0) return t('ownerAccounts.durationMinutes', { minutes })
  return t('ownerAccounts.durationSeconds', { seconds })
}

export function ownerAccountUtilization(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return `${Math.round(value)}%`
}

export function ownerAccountCost(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return value.toFixed(2)
}

export function ownerAccountNumber(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return Math.round(value).toLocaleString()
}
