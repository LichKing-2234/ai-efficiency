import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'
import { setLocale } from '@/i18n'

const modelRows = [
  {
    model: 'model-alpha',
    requests: 12,
    input_tokens: 1000,
    output_tokens: 500,
    cache_creation_tokens: 20,
    cache_read_tokens: 30,
    total_tokens: 1550,
    cost: 0.25,
    actual_cost: 0.2,
  },
  {
    model: 'model-beta',
    requests: 4,
    input_tokens: 400,
    output_tokens: 200,
    cache_creation_tokens: 0,
    cache_read_tokens: 0,
    total_tokens: 600,
    cost: 0.1,
    actual_cost: 0.08,
  },
]

function installMatchMedia(initialMatches: boolean) {
  const listeners = new Set<(event: { matches: boolean; media: string }) => void>()
  const mediaQuery = {
    matches: initialMatches,
    media: '(min-width: 768px)',
    onchange: null,
    addEventListener: vi.fn((type: string, listener: (event: { matches: boolean; media: string }) => void) => {
      if (type === 'change') listeners.add(listener)
    }),
    removeEventListener: vi.fn((type: string, listener: (event: { matches: boolean; media: string }) => void) => {
      if (type === 'change') listeners.delete(listener)
    }),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(() => true),
  }
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => mediaQuery),
  })
  return {
    change(matches: boolean) {
      mediaQuery.matches = matches
      for (const listener of listeners) listener({ matches, media: mediaQuery.media })
    },
  }
}

describe('UsageModelChart', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('mounts one table-or-card model representation for the active breakpoint', async () => {
    const media = installMatchMedia(true)
    const wrapper = mount(UsageModelChart, {
      props: { data: modelRows, loading: false },
      global: { stubs: { DoughnutChartCanvas: true } },
    })
    await flushPromises()

    expect(wrapper.find('[data-model-list="desktop"]').exists()).toBe(true)
    expect(wrapper.find('[data-model-list="mobile"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-model-row]')).toHaveLength(2)

    media.change(false)
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-model-list="desktop"]').exists()).toBe(false)
    expect(wrapper.find('[data-model-list="mobile"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-model-row]')).toHaveLength(2)
    expect(wrapper.find('[data-model-list="mobile"]').classes()).not.toContain('overflow-x-auto')
  })
})
