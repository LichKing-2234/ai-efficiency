import type { RepoWebhookRepairItem } from '@/lib/api/types'
import type { MessageKey } from '@/lib/i18n/messages'

export function canShowWebhookRepair(state: {
  role?: string
  bindingState?: 'bound' | 'unbound'
  status?: string
  webhookId?: string | null
}) {
  return state.role === 'admin'
    && state.bindingState === 'bound'
    && (state.status === 'webhook_failed' || !state.webhookId)
}

export function repoWebhookRepairNotice(item: RepoWebhookRepairItem): { kind: 'success' | 'error'; key: MessageKey; detail?: string } {
  if (item.webhook_status === 'failed' || item.status === 'webhook_failed' || item.error) {
    return { kind: 'error', key: 'repoDetail.webhookRepairFailed', detail: item.error }
  }
  return { kind: 'success', key: item.webhook_status === 'registered' ? 'repoDetail.webhookRepaired' : 'repoDetail.webhookRepairComplete' }
}
