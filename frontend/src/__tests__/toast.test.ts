import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AppToastHost from '@/components/AppToastHost.vue'
import { resetToastsForTest, useToast } from '@/composables/useToast'

describe('AppToastHost', () => {
  afterEach(() => {
    vi.useRealTimers()
    resetToastsForTest()
  })

  it('renders global toast messages and auto dismisses them', async () => {
    vi.useFakeTimers()
    const wrapper = mount(AppToastHost)
    const { showToast } = useToast()

    showToast({ message: 'Copied encrypted', tone: 'success', durationMs: 1200 })
    await wrapper.vm.$nextTick()

    const toast = wrapper.get('[data-testid="app-toast"]')
    expect(toast.text()).toContain('Copied encrypted')
    expect(toast.classes().join(' ')).toContain('bg-emerald-50')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(1200)
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="app-toast"]').exists()).toBe(false)
  })

  it('lets users close a toast manually', async () => {
    const wrapper = mount(AppToastHost)
    const { showToast } = useToast()

    showToast({ message: 'Copy failed', tone: 'error', durationMs: 0 })
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[data-testid="app-toast"]').text()).toContain('Copy failed')
    await wrapper.get('[data-testid="app-toast-close"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="app-toast"]').exists()).toBe(false)
  })
})
