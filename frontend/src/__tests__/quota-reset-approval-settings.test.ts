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
  it('removes existing approver rows with explicit full replacement save', async () => {
    const api = await import('@/api/quotaReset') as any
    api.getQuotaResetApproverConfigs.mockResolvedValueOnce({
      data: {
        data: {
          items: [
            {
              id: 7,
              directory_source_id: 1,
              department_external_id: 'dept-alpha',
              department_display_path: 'Department Alpha',
              approver_user_id: 12,
              approver_username: 'lead',
              approver_email: 'lead@example.com',
              enabled: true,
              created_at: '',
              updated_at: '',
            },
          ],
        },
      },
    })
    api.saveQuotaResetApproverConfigs.mockResolvedValueOnce({ data: { data: { items: [] } } })
    const wrapper = mount(QuotaResetApprovalSettings, { props: { credentials: [] } })
    await flushPromises()

    expect((wrapper.find('input[aria-label="Display path"]').element as HTMLInputElement).value).toBe('Department Alpha')
    await wrapper.find('[data-testid="quota-reset-config-remove-7"]').trigger('click')
    await wrapper.find('[data-testid="quota-reset-save-approvers"]').trigger('click')
    await flushPromises()

    expect(api.saveQuotaResetApproverConfigs).toHaveBeenCalledWith([], 'replace_all')
  })

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
