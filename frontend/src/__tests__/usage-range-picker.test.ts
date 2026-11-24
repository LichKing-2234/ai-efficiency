import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import UsageRangePicker from '@/components/usage/UsageRangePicker.vue'
import {
  customSelection,
  formatDateInput,
  presetSelection,
  quotaWindowForSelection,
  rangeGranularity,
} from '@/utils/usageRange'
import type { UsageRangeSelection } from '@/utils/usageRange'

function lastEmitted(wrapper: ReturnType<typeof mount>): UsageRangeSelection {
  const events = (wrapper.emitted('update:modelValue') ?? []) as unknown[][]
  return events[events.length - 1]?.[0] as UsageRangeSelection
}

function dateOffset(days: number) {
  const date = new Date()
  date.setDate(date.getDate() + days)
  return formatDateInput(date)
}

describe('usage range helpers', () => {
  it('derives preset dates so the picker mirrors the selected window', () => {
    const today = new Date(2026, 5, 15)
    expect(presetSelection('today', today)).toEqual({
      range: 'today',
      startDate: '2026-06-15',
      endDate: '2026-06-15',
    })
    expect(presetSelection('7d', today)).toEqual({
      range: '7d',
      startDate: '2026-06-09',
      endDate: '2026-06-15',
    })
    expect(presetSelection('30d', today)).toEqual({
      range: '30d',
      startDate: '2026-05-17',
      endDate: '2026-06-15',
    })
  })

  it('normalises an inverted custom range', () => {
    expect(customSelection('2026-06-10', '2026-06-01')).toEqual({
      range: 'custom',
      startDate: '2026-06-01',
      endDate: '2026-06-01',
    })
    expect(customSelection('', '2026-06-01').range).toBe('30d')
  })

  it('maps the selection to granularity and quota window', () => {
    expect(rangeGranularity(presetSelection('today'))).toBe('hour')
    expect(rangeGranularity(presetSelection('7d'))).toBe('day')
    expect(quotaWindowForSelection(presetSelection('today'))).toBe('daily')
    expect(quotaWindowForSelection(presetSelection('7d'))).toBe('weekly')
    expect(quotaWindowForSelection(presetSelection('30d'))).toBe('monthly')
    expect(quotaWindowForSelection(customSelection('2026-06-01', '2026-06-01'))).toBe('daily')
    expect(quotaWindowForSelection(customSelection('2026-06-01', '2026-06-03'))).toBe('weekly')
    expect(quotaWindowForSelection(customSelection('2026-06-01', '2026-06-20'))).toBe('monthly')
  })
})

describe('UsageRangePicker', () => {
  const initial: UsageRangeSelection = presetSelection('30d')

  it('fills both date inputs when a preset is selected', async () => {
    const wrapper = mount(UsageRangePicker, { props: { modelValue: initial } })
    await wrapper.get('[data-test="range-today"]').trigger('click')

    const emitted = lastEmitted(wrapper)
    expect(emitted).toEqual({ range: 'today', startDate: dateOffset(0), endDate: dateOffset(0) })

    await wrapper.setProps({ modelValue: emitted })
    expect((wrapper.get('[data-test="range-custom-start"]').element as HTMLInputElement).value).toBe(dateOffset(0))
    expect((wrapper.get('[data-test="range-custom-end"]').element as HTMLInputElement).value).toBe(dateOffset(0))
  })

  it('fills the 7-day window into both inputs', async () => {
    const wrapper = mount(UsageRangePicker, { props: { modelValue: initial } })
    await wrapper.get('[data-test="range-7d"]').trigger('click')

    const emitted = lastEmitted(wrapper)
    expect(emitted).toEqual({ range: '7d', startDate: dateOffset(-6), endDate: dateOffset(0) })
  })

  it('switches to a custom range when a date input changes', async () => {
    const wrapper = mount(UsageRangePicker, { props: { modelValue: initial } })
    await wrapper.get('[data-test="range-custom-start"]').setValue('2026-06-01')
    await wrapper.get('[data-test="range-custom-start"]').trigger('change')
    const first = lastEmitted(wrapper)
    expect(first).toEqual({ range: 'custom', startDate: '2026-06-01', endDate: initial.endDate })

    await wrapper.setProps({ modelValue: first })
    await wrapper.get('[data-test="range-custom-end"]').setValue('2026-06-03')
    await wrapper.get('[data-test="range-custom-end"]').trigger('change')

    const emitted = lastEmitted(wrapper)
    expect(emitted).toEqual({ range: 'custom', startDate: '2026-06-01', endDate: '2026-06-03' })
  })

  it('collapses the click-plus-change duplicate of one interaction', async () => {
    const wrapper = mount(UsageRangePicker, { props: { modelValue: initial } })
    const button = wrapper.get('[data-test="range-7d"]')
    await button.trigger('click')
    await button.trigger('click')

    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })
})
