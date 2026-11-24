import type { UsageWindow } from '@/utils/usageWindowPreference'

export type UsageRangeValue = UsageWindow | 'custom'

export interface UsageRangeSelection {
  range: UsageRangeValue
  startDate: string
  endDate: string
}

const presetLookbackDays: Record<UsageWindow, number> = {
  today: 0,
  '7d': 6,
  '30d': 29,
}

export function formatDateInput(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function presetSelection(range: UsageWindow, today: Date = new Date()): UsageRangeSelection {
  const end = new Date(today)
  const start = new Date(today)
  start.setDate(end.getDate() - presetLookbackDays[range])
  return { range, startDate: formatDateInput(start), endDate: formatDateInput(end) }
}

export function customSelection(startDate: string, endDate: string): UsageRangeSelection {
  if (!startDate || !endDate) return presetSelection('30d')
  if (startDate > endDate) return { range: 'custom', startDate: endDate, endDate }
  return { range: 'custom', startDate, endDate }
}

export function isSingleDaySelection(selection: UsageRangeSelection): boolean {
  return selection.startDate === selection.endDate
}

export function rangeGranularity(selection: UsageRangeSelection): 'hour' | 'day' {
  return isSingleDaySelection(selection) ? 'hour' : 'day'
}

/** Enforcement window implied by the selection, mirroring the backend span rule. */
export function quotaWindowForSelection(selection: UsageRangeSelection): 'daily' | 'weekly' | 'monthly' {
  if (selection.range === 'today') return 'daily'
  if (selection.range === '7d') return 'weekly'
  if (selection.range === '30d') return 'monthly'
  const days = Math.round(
    (Date.parse(`${selection.endDate}T00:00:00Z`) - Date.parse(`${selection.startDate}T00:00:00Z`)) / 86_400_000,
  ) + 1
  if (days <= 1) return 'daily'
  if (days <= 7) return 'weekly'
  return 'monthly'
}
