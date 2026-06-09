import i18next from 'i18next'
import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { I18nextProvider, initReactI18next, useTranslation } from 'react-i18next'
import { defaultLocale, isLocale, messages, type Locale, type MessageKey } from './messages'

const COOKIE_NAME = 'ae.locale'
const COOKIE_MAX_AGE_SECONDS = 60 * 60 * 24 * 365

void i18next.use(initReactI18next).init({
  lng: defaultLocale,
  fallbackLng: defaultLocale,
  resources: Object.fromEntries(Object.entries(messages).map(([locale, table]) => [locale, { translation: table }])),
  interpolation: { escapeValue: false }
})

const LocaleContext = createContext<{ locale: Locale; setLocale: (locale: Locale) => void }>({
  locale: defaultLocale,
  setLocale: () => undefined
})

function readLocaleCookie() {
  if (typeof document === 'undefined') return defaultLocale
  const raw = document.cookie
    .split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${COOKIE_NAME}=`))
    ?.slice(COOKIE_NAME.length + 1)
  const decoded = raw ? decodeURIComponent(raw) : null
  return isLocale(decoded) ? decoded : defaultLocale
}

function writeLocaleCookie(locale: Locale) {
  if (typeof document === 'undefined') return
  document.cookie = `${COOKIE_NAME}=${encodeURIComponent(locale)}; Path=/; Max-Age=${COOKIE_MAX_AGE_SECONDS}; SameSite=Lax`
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => readLocaleCookie())

  useEffect(() => {
    void i18next.changeLanguage(locale)
    document.documentElement.lang = locale
    writeLocaleCookie(locale)
  }, [locale])

  const value = useMemo(() => ({ locale, setLocale: setLocaleState }), [locale])
  return (
    <LocaleContext.Provider value={value}>
      <I18nextProvider i18n={i18next}>{children}</I18nextProvider>
    </LocaleContext.Provider>
  )
}

export function useI18n() {
  const { locale, setLocale } = useContext(LocaleContext)
  const { t: rawT } = useTranslation()
  const t = (key: MessageKey, values?: Record<string, string | number>) => rawT(key, values)
  const toggleLocale = () => setLocale(locale === 'en-US' ? 'zh-CN' : 'en-US')
  return { locale, setLocale, toggleLocale, t }
}

export { i18next }
