export type UsageWindow = 'today' | '7d' | '30d'

const defaultUsageWindow: UsageWindow = '30d'

const storageKey = 'ae.usage.window'
const usageWindows: readonly UsageWindow[] = ['today', '7d', '30d']

type UsageWindowStorage = Pick<Storage, 'getItem' | 'setItem'>

export function readUsageWindowPreference(storage?: UsageWindowStorage): UsageWindow {
  try {
    const value = (storage ?? globalThis.localStorage).getItem(storageKey)
    return usageWindows.includes(value as UsageWindow) ? value as UsageWindow : defaultUsageWindow
  } catch {
    return defaultUsageWindow
  }
}

export function writeUsageWindowPreference(window: UsageWindow, storage?: UsageWindowStorage) {
  try {
    (storage ?? globalThis.localStorage).setItem(storageKey, window)
  } catch {
    // Browser preference storage is best-effort.
  }
}
