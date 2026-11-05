import { onBeforeUnmount, ref, type Ref } from 'vue'

export interface ComboPosition {
  left: number
  top: number
  width: number
}

/** Tracks a teleported dropdown under its trigger, including scroll/resize. */
export function useComboPosition(trigger: Ref<HTMLElement | null>) {
  const position = ref<ComboPosition>({ left: 0, top: 0, width: 240 })

  function update() {
    const element = trigger.value
    if (!element) return
    const rect = element.getBoundingClientRect()
    position.value = {
      left: Math.max(8, rect.left),
      top: rect.bottom + 4,
      width: rect.width,
    }
  }

  function start() {
    update()
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
  }

  function stop() {
    window.removeEventListener('scroll', update, true)
    window.removeEventListener('resize', update)
  }

  onBeforeUnmount(stop)

  return { position, start, stop, update }
}
