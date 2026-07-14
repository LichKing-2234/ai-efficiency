import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref, watch } from 'vue'
import { useModalFocus } from '@/composables/useModalFocus'

const ModalFocusHarness = defineComponent({
  setup(_, { expose }) {
    const open = ref(false)
    const dialog = ref<HTMLElement | null>(null)
    const initialFocus = ref<HTMLElement | null>(null)
    const fallbackFocus = ref<HTMLElement | null>(null)
    const reopenDuringCloseFlush = ref(false)

    function close() {
      open.value = false
    }

    function closeAndReopenBeforeContinuation() {
      reopenDuringCloseFlush.value = true
      open.value = false
    }

    const { handleKeydown } = useModalFocus(open, dialog, {
      initialFocus,
      restoreFocusFallback: fallbackFocus,
      onClose: close,
    })

    watch(open, (value) => {
      if (value || !reopenDuringCloseFlush.value) return
      reopenDuringCloseFlush.value = false
      open.value = true
    }, { flush: 'post' })

    expose({ close, closeAndReopenBeforeContinuation })
    return { dialog, fallbackFocus, handleKeydown, initialFocus, open }
  },
  template: `
    <div>
      <button data-testid="opener" type="button" @click="open = true">Open</button>
      <button ref="fallbackFocus" data-testid="fallback" type="button">Fallback</button>
      <div
        v-if="open"
        ref="dialog"
        data-testid="dialog"
        role="dialog"
        tabindex="-1"
        @keydown="handleKeydown"
      >
        <button ref="initialFocus" data-testid="inside" type="button">Inside</button>
      </div>
    </div>
  `,
})

describe('useModalFocus', () => {
  it('ignores a stale close continuation across rapid close and reopen', async () => {
    const host = document.createElement('div')
    document.body.appendChild(host)
    const wrapper = mount(ModalFocusHarness, { attachTo: host })
    const focusSpy = vi.spyOn(HTMLElement.prototype, 'focus')

    try {
      const opener = wrapper.get('[data-testid="opener"]')
      ;(opener.element as HTMLButtonElement).focus()
      await opener.trigger('click')
      await nextTick()
      await nextTick()
      expect(document.activeElement).toBe(wrapper.get('[data-testid="inside"]').element)
      focusSpy.mockClear()

      ;(wrapper.vm as unknown as {
        closeAndReopenBeforeContinuation: () => void
      }).closeAndReopenBeforeContinuation()
      await nextTick()
      await nextTick()

      expect(wrapper.find('[data-testid="dialog"]').exists()).toBe(true)
      const reopenedInitialFocus = wrapper.get('[data-testid="inside"]').element
      expect(document.activeElement).toBe(reopenedInitialFocus)
      expect(focusSpy.mock.instances).toEqual([reopenedInitialFocus])

      ;(wrapper.vm as unknown as { close: () => void }).close()
      await nextTick()
      await nextTick()

      expect(wrapper.find('[data-testid="dialog"]').exists()).toBe(false)
      expect(document.activeElement).toBe(opener.element)
    } finally {
      focusSpy.mockRestore()
      wrapper.unmount()
      host.remove()
    }
  })
})
