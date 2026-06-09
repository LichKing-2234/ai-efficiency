import { defaultLocale, type Locale } from '@/lib/i18n/messages'

export function number(value: number | undefined | null, locale: Locale = defaultLocale) {
  if (value == null || Number.isNaN(value)) return '-'
  return value.toLocaleString(locale)
}

export function compact(value: number | undefined | null, locale: Locale = defaultLocale) {
  if (value == null || Number.isNaN(value)) return '-'
  return Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

export function dateTime(value: string | undefined | null, locale: Locale = defaultLocale) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString(locale)
}

export function percent(value: number | undefined | null, locale: Locale = defaultLocale) {
  if (value == null || Number.isNaN(value)) return '-'
  return Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }).format(value)
}

export function currency(value: number | undefined | null, locale: Locale = defaultLocale, currencyCode = 'USD') {
  if (value == null || Number.isNaN(value)) return '-'
  return Intl.NumberFormat(locale, { style: 'currency', currency: currencyCode, maximumFractionDigits: 4 }).format(value)
}

export function durationMs(value: number | undefined | null, locale: Locale = defaultLocale) {
  if (value == null || Number.isNaN(value)) return '-'
  if (value < 1000) return `${number(Math.round(value), locale)}ms`
  return `${number(Number((value / 1000).toFixed(2)), locale)}s`
}

export function tokenTotal(row: { input_tokens?: number; output_tokens?: number; cached_input_tokens?: number; reasoning_tokens?: number }) {
  return (row.input_tokens ?? 0) + (row.output_tokens ?? 0) + (row.cached_input_tokens ?? 0) + (row.reasoning_tokens ?? 0)
}
