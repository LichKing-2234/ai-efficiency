import { describe, expect, it, vi } from 'vitest'
import enUS from '@/locales/en-US'
import zhCN from '@/locales/zh-CN'
import { createI18nControllerForTest, type Locale } from '@/i18n'
import type { Messages } from '@/locales/en-US'

const EN_MESSAGES = {
  'app.title': 'Synthetic English',
  'nav.languageToggle': 'Chinese',
} as unknown as Messages

const ZH_MESSAGES = {
  'app.title': 'Synthetic Chinese',
  'nav.languageToggle': 'English',
} as unknown as Messages

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function createMemoryStorage(initial: Record<string, string> = {}) {
  const values = new Map(Object.entries(initial))
  return {
    getItem(key: string) {
      return values.get(key) ?? null
    },
    setItem(key: string, value: string) {
      values.set(key, value)
    },
    value(key: string) {
      return values.get(key) ?? null
    },
  }
}

interface HarnessOptions {
  initialStorage?: Record<string, string>
  browserLanguage?: string
  enLoader?: () => Promise<Messages>
  zhLoader?: () => Promise<Messages>
}

function createHarness(options: HarnessOptions = {}) {
  const storage = createMemoryStorage(options.initialStorage)
  const documentElement = { lang: 'en-US' }
  const enLoader = vi.fn(options.enLoader ?? (() => Promise.resolve(EN_MESSAGES)))
  const zhLoader = vi.fn(options.zhLoader ?? (() => Promise.resolve(ZH_MESSAGES)))
  const controller = createI18nControllerForTest({
    loaders: {
      'en-US': enLoader,
      'zh-CN': zhLoader,
    },
    storage,
    documentElement,
    browserLanguage: () => options.browserLanguage ?? 'en-US',
  })
  return { controller, documentElement, enLoader, storage, zhLoader }
}

describe('i18n locale loading', () => {
  it('loads only the saved locale before bootstrap resolves', async () => {
    const chinese = deferred<Messages>()
    const { controller, documentElement, enLoader, storage, zhLoader } = createHarness({
      initialStorage: { 'ae.locale': 'zh-CN' },
      zhLoader: () => chinese.promise,
    })

    let settled = false
    const initialization = controller.initializeI18n().then(() => {
      settled = true
    })

    expect(enLoader).not.toHaveBeenCalled()
    expect(zhLoader).toHaveBeenCalledTimes(1)
    expect(settled).toBe(false)
    expect(controller.t('app.title')).toBe('app.title')
    expect(documentElement.lang).toBe('en-US')

    chinese.resolve(ZH_MESSAGES)
    await initialization

    expect(controller.t('app.title')).toBe('Synthetic Chinese')
    expect(controller.locale.value).toBe('zh-CN')
    expect(storage.value('ae.locale')).toBe('zh-CN')
    expect(documentElement.lang).toBe('zh-CN')
  })

  it('deduplicates concurrent loads for the same locale', async () => {
    const chinese = deferred<Messages>()
    const { controller, zhLoader } = createHarness({
      zhLoader: () => chinese.promise,
    })

    const first = controller.setLocale('zh-CN')
    const second = controller.setLocale('zh-CN')

    expect(zhLoader).toHaveBeenCalledTimes(1)
    chinese.resolve(ZH_MESSAGES)
    await Promise.all([first, second])

    expect(controller.locale.value).toBe('zh-CN')
    expect(controller.t('app.title')).toBe('Synthetic Chinese')
  })

  it('keeps copy, locale, storage, and document language stable while loading', async () => {
    const chinese = deferred<Messages>()
    const { controller, documentElement, storage } = createHarness({
      zhLoader: () => chinese.promise,
    })
    await controller.setLocale('en-US')

    const switching = controller.setLocale('zh-CN')

    expect(controller.t('app.title')).toBe('Synthetic English')
    expect(controller.locale.value).toBe('en-US')
    expect(storage.value('ae.locale')).toBe('en-US')
    expect(documentElement.lang).toBe('en-US')

    chinese.resolve(ZH_MESSAGES)
    await switching

    expect(controller.t('app.title')).toBe('Synthetic Chinese')
    expect(controller.locale.value).toBe('zh-CN')
    expect(storage.value('ae.locale')).toBe('zh-CN')
    expect(documentElement.lang).toBe('zh-CN')
  })

  it('commits a cached locale synchronously before its promise resolves', async () => {
    const { controller, documentElement, enLoader, storage, zhLoader } = createHarness()
    await controller.setLocale('en-US')
    await controller.setLocale('zh-CN')

    const switching = controller.setLocale('en-US')

    expect(controller.t('app.title')).toBe('Synthetic English')
    expect(controller.locale.value).toBe('en-US')
    expect(storage.value('ae.locale')).toBe('en-US')
    expect(documentElement.lang).toBe('en-US')
    expect(enLoader).toHaveBeenCalledTimes(1)
    expect(zhLoader).toHaveBeenCalledTimes(1)
    await switching
  })

  it('retains current state after failure and retries the locale loader', async () => {
    const retry = deferred<Messages>()
    const zhLoader = vi.fn<() => Promise<Messages>>()
      .mockRejectedValueOnce(new Error('synthetic locale failure'))
      .mockReturnValueOnce(retry.promise)
    const { controller, documentElement, storage } = createHarness({ zhLoader })
    await controller.setLocale('en-US')

    await expect(controller.setLocale('zh-CN')).rejects.toThrow('synthetic locale failure')

    expect(controller.t('app.title')).toBe('Synthetic English')
    expect(controller.locale.value).toBe('en-US')
    expect(storage.value('ae.locale')).toBe('en-US')
    expect(documentElement.lang).toBe('en-US')

    const switching = controller.setLocale('zh-CN')
    expect(zhLoader).toHaveBeenCalledTimes(2)
    retry.resolve(ZH_MESSAGES)
    await switching
    expect(controller.t('app.title')).toBe('Synthetic Chinese')
  })

  it('allows only the latest different-locale request to commit', async () => {
    const english = deferred<Messages>()
    const chinese = deferred<Messages>()
    const { controller, documentElement, storage } = createHarness({
      enLoader: () => english.promise,
      zhLoader: () => chinese.promise,
    })

    const staleChinese = controller.setLocale('zh-CN')
    const latestEnglish = controller.setLocale('en-US')

    chinese.resolve(ZH_MESSAGES)
    await staleChinese
    expect(controller.t('app.title')).toBe('app.title')
    expect(storage.value('ae.locale')).toBeNull()
    expect(documentElement.lang).toBe('en-US')

    english.resolve(EN_MESSAGES)
    await latestEnglish
    expect(controller.t('app.title')).toBe('Synthetic English')
    expect(controller.locale.value).toBe('en-US')
    expect(storage.value('ae.locale')).toBe('en-US')
  })

  it('does not let a superseded rejection disturb a cached latest locale', async () => {
    const chinese = deferred<Messages>()
    const { controller, documentElement, storage } = createHarness({
      zhLoader: () => chinese.promise,
    })
    await controller.setLocale('en-US')

    const staleChinese = controller.setLocale('zh-CN')
    const latestEnglish = controller.setLocale('en-US')
    await latestEnglish
    chinese.reject(new Error('synthetic stale failure'))
    await expect(staleChinese).rejects.toThrow('synthetic stale failure')

    expect(controller.t('app.title')).toBe('Synthetic English')
    expect(controller.locale.value).toBe('en-US')
    expect(storage.value('ae.locale')).toBe('en-US')
    expect(documentElement.lang).toBe('en-US')
  })

  it('keeps both real dictionaries at the complete 1048-key contract', () => {
    const englishKeys = Object.keys(enUS).sort()
    const chineseKeys = Object.keys(zhCN).sort()

    expect(englishKeys).toHaveLength(1048)
    expect(chineseKeys).toHaveLength(1048)
    expect(chineseKeys).toEqual(englishKeys)
  })

  it('awaits locale initialization before mounting the application', async () => {
    const initialization = deferred<void>()
    const mount = vi.fn()
    const app = { use: vi.fn(), mount }
    const createApp = vi.fn(() => app)
    const createPinia = vi.fn(() => ({ synthetic: 'pinia' }))

    vi.resetModules()
    vi.doMock('@/i18n', () => ({ initializeI18n: () => initialization.promise }))
    vi.doMock('vue', () => ({ createApp }))
    vi.doMock('pinia', () => ({ createPinia }))
    vi.doMock('@/App.vue', () => ({ default: { name: 'SyntheticApp' } }))
    vi.doMock('@/router', () => ({ default: { synthetic: 'router' } }))

    try {
      await import('@/main')

      expect(createApp).not.toHaveBeenCalled()
      expect(mount).not.toHaveBeenCalled()

      initialization.resolve()
      await initialization.promise
      await Promise.resolve()

      expect(createApp).toHaveBeenCalledTimes(1)
      expect(app.use).toHaveBeenCalledTimes(2)
      expect(mount).toHaveBeenCalledTimes(1)
      expect(mount).toHaveBeenCalledWith('#app')
    } finally {
      vi.doUnmock('@/i18n')
      vi.doUnmock('vue')
      vi.doUnmock('pinia')
      vi.doUnmock('@/App.vue')
      vi.doUnmock('@/router')
      vi.resetModules()
    }
  })
})
