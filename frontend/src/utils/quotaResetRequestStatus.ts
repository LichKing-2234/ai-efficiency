import type { QuotaResetStatus } from '@/types'

type Translate = (key: any) => string

export function quotaResetStatusLabel(t: Translate, status: QuotaResetStatus) {
  switch (status) {
    case 'approved_resetting':
      return t('quotaReset.status.approved_resetting')
    case 'approved_reset_succeeded':
      return t('quotaReset.status.approved_reset_succeeded')
    case 'approved_reset_failed':
      return t('quotaReset.status.approved_reset_failed')
    case 'rejected':
      return t('quotaReset.status.rejected')
    case 'cancelled':
      return t('quotaReset.status.cancelled')
    default:
      return t('quotaReset.status.pending')
  }
}

export function quotaResetStatusClass(status: QuotaResetStatus) {
  if (status === 'approved_reset_succeeded') return 'bg-emerald-50 text-emerald-700'
  if (status === 'approved_reset_failed') return 'bg-red-50 text-red-700'
  if (status === 'rejected' || status === 'cancelled') return 'bg-slate-100 text-slate-600'
  if (status === 'approved_resetting') return 'bg-blue-50 text-blue-700'
  return 'bg-amber-50 text-amber-700'
}
