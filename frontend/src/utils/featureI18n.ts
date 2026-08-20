import type { Ref } from 'vue'
import type { Locale, MessageKey } from '@/i18n'

type Translate = (key: MessageKey, params?: Record<string, string | number>) => string
type FeatureMessages = Readonly<Record<Locale, Readonly<Record<string, string>>>>

function formatMessage(value: string, params?: Record<string, string | number>) {
  if (!params) return value
  return Object.entries(params).reduce(
    (text, [key, parameter]) => text.split(`{${key}}`).join(String(parameter)),
    value,
  )
}

export function createFeatureTranslator(
  locale: Ref<Locale>,
  baseT: Translate,
  prefix: string,
  messages: FeatureMessages,
): Translate {
  return (key, params) => {
    if (key.startsWith(prefix)) {
      const message = messages[locale.value][key]
      if (message) return formatMessage(message, params)
    }
    return baseT(key, params)
  }
}
