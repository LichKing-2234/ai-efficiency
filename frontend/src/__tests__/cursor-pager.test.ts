import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import CursorPager from '@/components/activity/CursorPager.vue'

describe('CursorPager', () => {
  it('emits only available page directions and disables navigation while loading', async () => {
    const wrapper = mount(CursorPager, {
      props: {
        hasPrevious: true,
        hasNext: true,
        loading: false,
        previousLabel: 'Previous page',
        nextLabel: 'Next page',
        testIDPrefix: 'activity-test',
      },
    })

    await wrapper.get('[data-testid="activity-test-previous"]').trigger('click')
    await wrapper.get('[data-testid="activity-test-next"]').trigger('click')
    expect(wrapper.emitted('previous')).toHaveLength(1)
    expect(wrapper.emitted('next')).toHaveLength(1)

    await wrapper.setProps({ loading: true })
    expect(wrapper.get('[data-testid="activity-test-previous"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="activity-test-next"]').attributes('disabled')).toBeDefined()
  })
})
