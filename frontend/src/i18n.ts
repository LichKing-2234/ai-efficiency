import { computed, ref, shallowRef, type ComputedRef, type Ref } from 'vue'
import type { MessageKey, Messages } from './locales/en-US'

export type { MessageKey, Messages } from './locales/en-US'

export type Locale = 'en-US' | 'zh-CN'

const STORAGE_KEY = 'ae.locale'

type LocaleLoader = () => Promise<Messages>

interface LocaleStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

interface LocaleDocumentElement {
  lang: string
}

interface I18nControllerOptions {
  loaders: Record<Locale, LocaleLoader>
  storage?: LocaleStorage
  documentElement?: LocaleDocumentElement
  browserLanguage?: () => string
}

interface I18nController {
  locale: Ref<Locale>
  initializeI18n(): Promise<void>
  setLocale(next: Locale): Promise<void>
  t(key: MessageKey, params?: Record<string, string | number>): string
  useI18n(): {
    locale: Ref<Locale>
    languageToggleLabel: ComputedRef<string>
    setLocale(next: Locale): Promise<void>
    t(key: MessageKey, params?: Record<string, string | number>): string
    toggleLocale(): void
  }
}

function createI18nController(options: I18nControllerOptions): I18nController {
  const loadedMessages = new Map<Locale, Messages>()
  const inFlightLoads = new Map<Locale, Promise<Messages>>()
  const activeMessages = shallowRef<Messages | null>(null)
  const locale = ref<Locale>('en-US')
  let latestRequest = 0

  function initialLocale(): Locale {
    const saved = options.storage?.getItem(STORAGE_KEY)
    if (saved === 'en-US' || saved === 'zh-CN') {
      return saved
    }
    const browserLanguage = options.browserLanguage?.() ?? ''
    return browserLanguage.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
  }

  function loadLocale(next: Locale): Promise<Messages> {
    const cached = loadedMessages.get(next)
    if (cached) {
      return Promise.resolve(cached)
    }
    const activeLoad = inFlightLoads.get(next)
    if (activeLoad) {
      return activeLoad
    }

    let moduleLoad: Promise<Messages>
    try {
      moduleLoad = options.loaders[next]()
    } catch (error) {
      return Promise.reject(error)
    }

    let pending!: Promise<Messages>
    pending = moduleLoad
      .then((messages) => {
        loadedMessages.set(next, messages)
        return messages
      })
      .finally(() => {
        if (inFlightLoads.get(next) === pending) {
          inFlightLoads.delete(next)
        }
      })
    inFlightLoads.set(next, pending)
    return pending
  }

  function commit(next: Locale, messages: Messages) {
    activeMessages.value = messages
    locale.value = next
    options.storage?.setItem(STORAGE_KEY, next)
    if (options.documentElement) {
      options.documentElement.lang = next
    }
  }

  function setLocale(next: Locale): Promise<void> {
    const requestID = ++latestRequest
    const cached = loadedMessages.get(next)
    if (cached) {
      commit(next, cached)
      return Promise.resolve()
    }
    return loadLocale(next).then((messages) => {
      if (requestID === latestRequest) {
        commit(next, messages)
      }
    })
  }

  function initializeI18n(): Promise<void> {
    return setLocale(initialLocale())
  }

  function t(key: MessageKey, params?: Record<string, string | number>) {
    let value: string = activeMessages.value?.[key as keyof Messages] ?? key
    if (!params) return value
    for (const [paramKey, paramValue] of Object.entries(params)) {
      value = value.split(`{${paramKey}}`).join(String(paramValue))
    }
    return value
  }

  function useI18n() {
    const languageToggleLabel = computed(() => t('nav.languageToggle'))

    function toggleLocale() {
      void setLocale(locale.value === 'en-US' ? 'zh-CN' : 'en-US').catch(() => {})
    }

    return {
      locale,
      languageToggleLabel,
      setLocale,
      t,
      toggleLocale,
    }
  }

  return {
    locale,
    initializeI18n,
    setLocale,
    t,
    useI18n,
  }
}

const localeLoaders: Record<Locale, LocaleLoader> = {
  'en-US': () => import('./locales/en-US').then((module) => module.default),
  'zh-CN': () => import('./locales/zh-CN').then((module) => module.default),
}

const controller = createI18nController({
  loaders: localeLoaders,
  storage: typeof localStorage === 'undefined' ? undefined : localStorage,
  documentElement: typeof document === 'undefined' ? undefined : document.documentElement,
  browserLanguage: () => typeof navigator === 'undefined' ? '' : navigator.language,
})

export function createI18nControllerForTest(options: I18nControllerOptions) {
  return createI18nController(options)
}

export function initializeI18n() {
  return controller.initializeI18n()
}

export function setLocale(next: Locale) {
  return controller.setLocale(next)
}

export function t(key: MessageKey, params?: Record<string, string | number>) {
  return controller.t(key, params)
}

export function useI18n() {
  return controller.useI18n()
}

export async function preloadI18nForTest() {
  await Promise.all([controller.setLocale('en-US'), controller.setLocale('zh-CN')])
  await controller.setLocale('en-US')
}
