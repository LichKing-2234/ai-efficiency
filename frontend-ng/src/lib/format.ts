export function number(value: number | undefined | null) {
  if (value == null || Number.isNaN(value)) return '-'
  return value.toLocaleString()
}

export function compact(value: number | undefined | null) {
  if (value == null || Number.isNaN(value)) return '-'
  return Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

export function dateTime(value: string | undefined | null) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

export function percent(value: number | undefined | null) {
  if (value == null || Number.isNaN(value)) return '-'
  return `${Math.round(value * 100)}%`
}

export function tokenTotal(row: { input_tokens?: number; output_tokens?: number; cached_input_tokens?: number; reasoning_tokens?: number }) {
  return (row.input_tokens ?? 0) + (row.output_tokens ?? 0) + (row.cached_input_tokens ?? 0) + (row.reasoning_tokens ?? 0)
}
