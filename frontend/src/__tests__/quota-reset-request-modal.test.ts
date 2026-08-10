import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElDialog } from 'element-plus'
import QuotaResetRequestModal from '@/components/quota-reset/QuotaResetRequestModal.vue'
import { setLocale } from '@/i18n'
import { withTeleportedContent } from './helpers/teleport'

describe('QuotaResetRequestModal', () => {
  it('requires a subscription group and reason before submit', async () => {
    setLocale('en-US')
    const wrapper = withTeleportedContent(mount(QuotaResetRequestModal, {
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
    }))
    await flushPromises()
    const dialog = wrapper.findComponent(ElDialog)
    expect(dialog.props('appendToBody')).toBe(true)
    await wrapper.get('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.text()).toContain('Reason is required')
    await wrapper.get('textarea').setValue('Need reset for a build investigation')
    await wrapper.get('button[data-testid="quota-reset-submit"]').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ group_id: '42', reason: 'Need reset for a build investigation' }])
  })
})
