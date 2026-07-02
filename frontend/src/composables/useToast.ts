import { readonly, reactive } from 'vue'

export type ToastTone = 'success' | 'error' | 'info'

export interface ToastOptions {
  message: string
  tone?: ToastTone
  durationMs?: number
}

const toast = reactive({
  visible: false,
  message: '',
  tone: 'info' as ToastTone,
})

let toastTimer: number | undefined

function clearToastTimer() {
  if (toastTimer !== undefined) {
    window.clearTimeout(toastTimer)
    toastTimer = undefined
  }
}

function dismissToast() {
  clearToastTimer()
  toast.visible = false
  toast.message = ''
  toast.tone = 'info'
}

function showToast(options: ToastOptions) {
  clearToastTimer()
  toast.visible = true
  toast.message = options.message
  toast.tone = options.tone ?? 'info'
  const durationMs = options.durationMs ?? 3000
  if (durationMs > 0) {
    toastTimer = window.setTimeout(dismissToast, durationMs)
  }
}

export function useToast() {
  return {
    toast: readonly(toast),
    showToast,
    dismissToast,
  }
}

export function resetToastsForTest() {
  dismissToast()
}
