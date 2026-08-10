import { describe, expect, it, vi } from 'vitest'
import { h, type Ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useMediaQuery } from '@/composables/useMediaQuery'

describe('useMediaQuery', () => {
  it('uses the browser match synchronously on the first render', () => {
    const addEventListener = vi.fn()
    const removeEventListener = vi.fn()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn(() => ({
        matches: true,
        media: '(min-width: 768px)',
        onchange: null,
        addEventListener,
        removeEventListener,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })
    const renderMatches: boolean[] = []

    const wrapper = mount({
      setup() {
        const matches: Readonly<Ref<boolean>> = useMediaQuery('(min-width: 768px)')
        return () => {
          renderMatches.push(matches.value)
          return h('div', matches.value ? 'desktop' : 'mobile')
        }
      },
    })

    expect(renderMatches[0]).toBe(true)
    expect(wrapper.text()).toBe('desktop')
    expect(addEventListener).toHaveBeenCalledOnce()

    wrapper.unmount()
    expect(removeEventListener).toHaveBeenCalledOnce()
  })
})
