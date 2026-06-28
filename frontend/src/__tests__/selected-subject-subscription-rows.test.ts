import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SelectedSubjectSubscriptionRows from '@/components/user/usage/SelectedSubjectSubscriptionRows.vue'

const editableRow = {
  group_id: '42',
  group_name: 'Group Alpha',
  platform: 'openai',
  subscription_status: 'active',
  inherited_default_multiplier: 1,
  system_default_multiplier: 1,
  user_multiplier: null,
  effective_multiplier: 1,
  multiplier_source: 'group' as const,
  daily_display_used_usd: 0,
  weekly_display_used_usd: 0,
  monthly_display_used_usd: 80,
  daily_usage_usd: 0,
  weekly_usage_usd: 0,
  monthly_usage_usd: 80,
  monthly_effective_allowance_usd: 500,
  usage_value_basis: 'raw_actual_cost',
  quota_window_basis: 'sub2api_enforcement_window',
  editable: true,
}

describe('SelectedSubjectSubscriptionRows', () => {
  it('previews normalized Used / Quota when draft multiplier changes', async () => {
    const wrapper = mount(SelectedSubjectSubscriptionRows, {
      props: {
        subjectUserId: 101,
        rows: [editableRow],
      },
    })

    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')
    await wrapper.get('[data-testid="multiplier-input"]').setValue('2')

    expect(wrapper.text()).toContain('$40.00 / $250.00')
  })

  it('keeps the multiplier modal open and shows an error when update fails', async () => {
    const updateMultiplier = vi.fn().mockRejectedValue(new Error('network failed'))
    const wrapper = mount(SelectedSubjectSubscriptionRows, {
      props: {
        subjectUserId: 101,
        rows: [editableRow],
        updateMultiplier,
      },
    })

    await wrapper.get('[data-testid="edit-multiplier-42"]').trigger('click')
    await wrapper.get('[data-testid="multiplier-input"]').setValue('2')
    const confirmButton = wrapper.findAll('button').find((button) => button.text() === 'Confirm')
    expect(confirmButton).toBeTruthy()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(updateMultiplier).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="multiplier-input"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Unable to update rate multiplier')
  })
})
