import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QuotaResetApprovalSettings from '@/components/settings/QuotaResetApprovalSettings.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetApproverConfigs: vi.fn(),
  saveQuotaResetApproverConfigs: vi.fn(),
  getQuotaResetNotificationSettings: vi.fn(),
  updateQuotaResetNotificationSettings: vi.fn(),
  testQuotaResetNotificationSettings: vi.fn(),
}))

beforeEach(async () => {
  setLocale('en-US')
  vi.clearAllMocks()
  const api = await import('@/api/quotaReset') as any
  api.getQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { items: [] } } })
  api.saveQuotaResetApproverConfigs.mockResolvedValue({ data: { data: { items: [] } } })
  api.getQuotaResetNotificationSettings.mockResolvedValue({ data: { data: { enabled: false, url: '', auth_type: 'none' } } })
  api.updateQuotaResetNotificationSettings.mockResolvedValue({
    data: { data: { enabled: true, url: 'https://hooks.example.com/ai-efficiency', auth_type: 'none' } },
  })
  api.testQuotaResetNotificationSettings.mockResolvedValue({ data: { data: { message: 'ok' } } })
})

describe('QuotaResetApprovalSettings', () => {
  it('saves webhook settings without exposing credential secrets', async () => {
    const wrapper = mount(QuotaResetApprovalSettings, {
      props: {
        credentials: [
          {
            id: 1,
            name: 'Webhook token',
            description: '',
            kind: 'secret_text',
            usage_count: 0,
            summary: { preview: 'tes****oken' },
            created_at: '',
            updated_at: '',
          },
        ],
      },
    })
    await flushPromises()

    await wrapper.find('input[data-testid="quota-reset-webhook-enabled"]').setValue(true)
    await wrapper.find('input[data-testid="quota-reset-webhook-url"]').setValue('https://hooks.example.com/ai-efficiency')
    await wrapper.find('button[data-testid="quota-reset-save-notification"]').trigger('click')
    await flushPromises()

    const api = await import('@/api/quotaReset')
    expect(api.updateQuotaResetNotificationSettings).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      url: 'https://hooks.example.com/ai-efficiency',
    }))
    expect(wrapper.text()).toContain('Webhook token')
    expect(wrapper.text()).toContain('tes****oken')
    expect(wrapper.text()).not.toContain('test-token')
  })
})
