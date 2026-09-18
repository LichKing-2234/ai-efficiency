import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import CursorPager from '@/components/CursorPager.vue'

describe('CursorPager', () => {
  it('keeps both directions rendered and disables unavailable navigation', () => {
    const wrapper = mount(CursorPager, {
      props: {
        hasPrevious: false,
        hasNext: true,
        loadingLabel: 'Loading',
        previousLabel: 'Previous page',
        nextLabel: 'Next page',
        testIDPrefix: 'activity-first-page',
      },
    })

    expect(wrapper.get('[data-testid="activity-first-page-previous"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="activity-first-page-next"]').attributes('disabled')).toBeUndefined()
  })

  it('renders a truthful range when the caller provides one', () => {
    const wrapper = mount(CursorPager, {
      props: {
        hasPrevious: true,
        hasNext: true,
        loadingLabel: 'Loading',
        previousLabel: 'Previous page',
        nextLabel: 'Next page',
        rangeLabel: 'Showing 51-100 of 238',
        testIDPrefix: 'team-members',
      },
    })

    expect(wrapper.get('[data-testid="team-members-range"]').text()).toBe('Showing 51-100 of 238')
  })

  it('emits only available page directions and disables navigation while loading', async () => {
    const wrapper = mount(CursorPager, {
      props: {
        hasPrevious: true,
        hasNext: true,
        loading: false,
        loadingLabel: 'Loading',
        previousLabel: 'Previous page',
        nextLabel: 'Next page',
        testIDPrefix: 'activity-test',
      },
    })

    expect(wrapper.get('[data-testid="activity-test-previous"]').classes()).toContain('el-button')
    expect(wrapper.get('[data-testid="activity-test-next"]').classes()).toContain('el-button')

    await wrapper.get('[data-testid="activity-test-previous"]').trigger('click')
    await wrapper.get('[data-testid="activity-test-next"]').trigger('click')
    expect(wrapper.emitted('previous')).toHaveLength(1)
    expect(wrapper.emitted('next')).toHaveLength(1)

    await wrapper.setProps({ loading: true })
    expect(wrapper.get('[data-testid="activity-test-previous"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="activity-test-next"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="activity-test-range"]').text()).toBe('Loading')
    expect(wrapper.get('[aria-busy="true"]').attributes('aria-busy')).toBe('true')
  })
})
