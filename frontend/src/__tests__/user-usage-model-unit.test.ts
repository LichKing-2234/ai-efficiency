import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'
import { setLocale } from '@/i18n'

vi.mock('@/components/charts/DoughnutChartCanvas.vue', () => ({
  __esModule: true,
  default: { name: 'DoughnutChartCanvas', template: '<div data-testid="doughnut-chart-stub" />' },
}))

const tokenModels = [
  { model: 'claude-sonnet-4', requests: 10, input_tokens: 900, output_tokens: 100, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 1000, cost: 0.1, actual_cost: 0.1 },
]

const kiroModels = [
  { model: 'claude-opus-5.5', requests: 83, credits: 45.38, input_tokens: 5_653_919, output_tokens: 33_278 },
]

async function mountChart(locale: 'en-US' = 'en-US') {
  await setLocale(locale)
  const wrapper = mount(UsageModelChart, {
    props: {
      data: tokenModels,
      kiroData: kiroModels,
      loading: false,
      kiroLoading: false,
      kiroAvailable: true,
    },
  })
  await flushPromises()
  await vi.dynamicImportSettled()
  return wrapper
}

describe('usage model chart unit switch', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('switches the model table and chart from tokens to credits', async () => {
    const wrapper = await mountChart()

    expect(wrapper.text()).toContain('claude-sonnet-4')
    expect(wrapper.text()).toContain('1K')
    expect(wrapper.text()).not.toContain('45.38')

    const toggle = wrapper.get('[data-testid="usage-model-unit-toggle"]')
    await toggle.findAll('label')[1].find('input[type="radio"]').setValue(true)
    await nextTick()
    await flushPromises()
    await vi.dynamicImportSettled()

    expect(wrapper.text()).toContain('claude-opus-5.5')
    expect(wrapper.text()).toContain('45.38')
    expect(wrapper.text()).not.toContain('claude-sonnet-4')
  })

  it('shows the credits empty state when the user has no Kiro usage', async () => {
    const wrapper = mount(UsageModelChart, {
      props: {
        data: tokenModels,
        kiroData: [],
        loading: false,
        kiroLoading: false,
        kiroAvailable: false,
      },
    })
    await flushPromises()
    await vi.dynamicImportSettled()

    const toggle = wrapper.get('[data-testid="usage-model-unit-toggle"]')
    await toggle.findAll('label')[1].find('input[type="radio"]').setValue(true)
    await nextTick()
    await flushPromises()

    expect(wrapper.text()).toContain('no Kiro CLI calls')
    wrapper.unmount()
  })
})
