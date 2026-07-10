import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import QuotaResetRequestModal from '@/components/quota-reset/QuotaResetRequestModal.vue'
import { setLocale } from '@/i18n'

describe('QuotaResetRequestModal', () => {
  it('requires a subscription group and reason before submit', async () => {
    setLocale('en-US')
    const wrapper = mount(QuotaResetRequestModal, {
      props: {
        open: true,
        groups: [
          {
            group_id: '42',
            group_name: 'Group Alpha',
            platform: 'openai',
            daily_usage_usd: 10,
            weekly_usage_usd: 20,
            monthly_usage_usd: 30,
          },
        ],
        submitting: false,
      },
    })
    await wrapper.find('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.text()).toContain('Reason is required')
    await wrapper.find('textarea').setValue('Need reset for a build investigation')
    await wrapper.find('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ group_id: '42', reason: 'Need reset for a build investigation' }])
  })
})
