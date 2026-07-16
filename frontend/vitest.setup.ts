import { beforeEach } from 'vitest'
import { resetToastsForTest } from './src/composables/useToast'

function createMemoryStorage(): Storage {
  const data = new Map<string, string>()

  return {
    get length() {
      return data.size
    },
    clear() {
      data.clear()
    },
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null
    },
    key(index: number) {
      return Array.from(data.keys())[index] ?? null
    },
    removeItem(key: string) {
      data.delete(key)
    },
    setItem(key: string, value: string) {
      data.set(key, String(value))
    },
  }
}

const local = createMemoryStorage()
const session = createMemoryStorage()

Object.defineProperty(globalThis, 'localStorage', {
  value: local,
  configurable: true,
})

Object.defineProperty(globalThis, 'sessionStorage', {
  value: session,
  configurable: true,
})

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'localStorage', {
    value: local,
    configurable: true,
  })
  Object.defineProperty(window, 'sessionStorage', {
    value: session,
    configurable: true,
  })
}

const { preloadI18nForTest, setLocale } = await import('./src/i18n')
await preloadI18nForTest()

beforeEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  void setLocale('en-US')
  resetToastsForTest()
})
