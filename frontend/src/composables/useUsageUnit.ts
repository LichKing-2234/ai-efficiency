import { ref, watch } from 'vue'

export type UsageUnit = 'token' | 'credits'

const DEFAULT_STORAGE_KEY = 'ae.usage.unit'

function readUnit(storageKey: string): UsageUnit {
  try {
    return localStorage.getItem(storageKey) === 'credits' ? 'credits' : 'token'
  } catch {
    return 'token'
  }
}

// useUsageUnit keeps one unit preference per storage key so every table on the
// page switches together and the choice survives reloads.
export function useUsageUnit(storageKey: string = DEFAULT_STORAGE_KEY) {
  const unit = ref<UsageUnit>(readUnit(storageKey))
  watch(unit, (value) => {
    try {
      localStorage.setItem(storageKey, value)
    } catch {
      // Preference persistence is best effort.
    }
  })
  return { unit }
}
