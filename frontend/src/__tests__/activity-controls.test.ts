import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ActivityDateRange from '@/components/activity/ActivityDateRange.vue'
import { setLocale } from '@/i18n'

describe('Activity controls', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('uses Element Plus range controls while preserving the exclusive custom end date', async () => {
    const wrapper = mount(ActivityDateRange, {
      props: {
        from: '2026-08-01T00:00:00Z',
        to: '2026-08-08T00:00:00Z',
        loading: false,
      },
    })

    expect(wrapper.get('[data-testid="activity-range-7"]').classes()).toContain('el-radio-button')
    expect(wrapper.get('[data-testid="activity-range-custom"]').classes()).toContain('el-radio-button')
    expect(wrapper.get('[data-testid="activity-range-refresh"]').classes()).toContain('el-button')

    await wrapper.get('[data-testid="activity-range-custom"]').trigger('click')
    const range = wrapper.get('[data-testid="activity-date-range"]')
    expect(range.get('[data-testid="activity-custom-panel"]')).toBeTruthy()
    const from = wrapper.get('input[data-testid="activity-custom-from"]')
    const to = wrapper.get('input[data-testid="activity-custom-to"]')
    expect(from.classes()).toContain('el-input__inner')
    expect(to.classes()).toContain('el-input__inner')
    await from.setValue('2026-08-01')
    await to.setValue('2026-08-03')
    const apply = wrapper.get('[data-testid="activity-range-apply"]')
    expect(apply.classes()).toContain('el-button')
    await apply.trigger('click')

    const changes = wrapper.emitted('change') ?? []
    const change = changes[changes.length - 1]?.[0] as { from: string; to: string }
    const changeFrom = new Date(change.from)
    const changeTo = new Date(change.to)
    expect([changeFrom.getFullYear(), changeFrom.getMonth(), changeFrom.getDate()]).toEqual([2026, 7, 1])
    expect([changeTo.getFullYear(), changeTo.getMonth(), changeTo.getDate()]).toEqual([2026, 7, 3])
  })

  it('explains and blocks an invalid custom range', async () => {
    const wrapper = mount(ActivityDateRange, { props: { from: '2026-08-01', to: '2026-08-08' } })
    await wrapper.get('[data-testid="activity-range-custom"]').trigger('click')
    await wrapper.get('input[data-testid="activity-custom-from"]').setValue('2026-01-01')
    await wrapper.get('input[data-testid="activity-custom-to"]').setValue('2026-08-08')

    expect(wrapper.get('[role="alert"]').text()).toContain('90 days')
    expect(wrapper.get('[data-testid="activity-range-apply"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="activity-range-apply"]').trigger('click')
    expect(wrapper.emitted('change')).toBeUndefined()
  })

})
