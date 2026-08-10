import { shallowRef, watch, type Ref } from 'vue'
import en from 'element-plus/es/locale/lang/en'
import type { Locale } from '@/i18n'

export function useElementPlusLocale(locale: Ref<Locale>) {
  const elementLocale = shallowRef(en)
  let requestID = 0

  watch(locale, (next) => {
    const currentRequest = ++requestID
    if (next === 'en-US') {
      elementLocale.value = en
      return
    }

    void import('element-plus/es/locale/lang/zh-cn').then((module) => {
      if (currentRequest === requestID) {
        elementLocale.value = module.default
      }
    }).catch(() => undefined)
  }, { immediate: true })

  return elementLocale
}
