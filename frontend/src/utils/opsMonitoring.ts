const BEIJING_TIME_ZONE = 'Asia/Shanghai'

const beijingPartsFormatter = new Intl.DateTimeFormat('en-US', {
  timeZone: BEIJING_TIME_ZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

function beijingParts(value: string | null | undefined): Record<string, string> | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts: Record<string, string> = {}
  for (const part of beijingPartsFormatter.formatToParts(date)) {
    if (part.type !== 'literal') parts[part.type] = part.value
  }
  return parts
}

/** `YYYY-MM-DD HH:mm:ss` in Asia/Shanghai, or an empty string for a missing/invalid timestamp. */
export function formatBeijingDateTime(value: string | null | undefined): string {
  const parts = beijingParts(value)
  if (!parts) return ''
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}

/** `HH:mm` in Asia/Shanghai, or an empty string for a missing/invalid timestamp. */
export function formatBeijingClock(value: string | null | undefined): string {
  const parts = beijingParts(value)
  if (!parts) return ''
  return `${parts.hour}:${parts.minute}`
}
