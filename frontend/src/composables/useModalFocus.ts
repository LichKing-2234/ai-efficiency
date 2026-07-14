import { nextTick, watch, type Ref } from 'vue'

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

function focusableElements(container: HTMLElement | null) {
  if (!container) return []
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
    .filter((element) => !element.hasAttribute('disabled') && element.tabIndex !== -1)
}

export function useModalFocus(
  isOpen: Readonly<Ref<boolean>>,
  container: Ref<HTMLElement | null>,
  options: {
    initialFocus?: Ref<HTMLElement | null>
    restoreFocusFallback?: Ref<HTMLElement | null>
    onClose: () => void
  }
) {
  let previousFocus: HTMLElement | null = null

  watch(isOpen, async (open) => {
    if (open) {
      previousFocus = typeof document !== 'undefined' ? document.activeElement as HTMLElement : null
      await nextTick()
      const target = options.initialFocus?.value ?? focusableElements(container.value)[0] ?? container.value
      target?.focus()
      return
    }

    await nextTick()
    const restoreTarget = previousFocus?.isConnected
      ? previousFocus
      : options.restoreFocusFallback?.value
    if (restoreTarget?.isConnected) {
      restoreTarget.focus()
    }
    previousFocus = null
  })

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation()
      options.onClose()
      return
    }

    if (event.key !== 'Tab') return

    const focusable = focusableElements(container.value)
    if (focusable.length === 0) {
      event.preventDefault()
      container.value?.focus()
      return
    }

    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement

    if (event.shiftKey && active === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && active === last) {
      event.preventDefault()
      first.focus()
    }
  }

  return { handleKeydown }
}
