import { describe, expect, test } from 'vitest'
import { canShowWebhookRepair, repoWebhookRepairNotice } from './repo-webhook-state'

describe('repo webhook repair state', () => {
  test('shows repair only for admin bound repositories with failed or missing webhook', () => {
    expect(canShowWebhookRepair({ role: 'admin', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(true)
    expect(canShowWebhookRepair({ role: 'admin', bindingState: 'bound', status: 'active', webhookId: null })).toBe(true)
    expect(canShowWebhookRepair({ role: 'user', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)
    expect(canShowWebhookRepair({ role: 'admin', bindingState: 'unbound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)
  })

  test('formats backend repair result into success or error notice', () => {
    expect(repoWebhookRepairNotice({ repo_config_id: 1, full_name: 'org/repo', previous_status: 'webhook_failed', status: 'active', webhook_status: 'registered' })).toEqual({ kind: 'success', key: 'repoDetail.webhookRepaired' })
    expect(repoWebhookRepairNotice({ repo_config_id: 1, full_name: 'org/repo', previous_status: 'webhook_failed', status: 'webhook_failed', webhook_status: 'failed', error: 'api failed' })).toEqual({ kind: 'error', key: 'repoDetail.webhookRepairFailed', detail: 'api failed' })
  })
})
