const sessionResourceResetters = new Map<string, () => void>()

export function registerSessionResourceReset(key: string, reset: () => void) {
  sessionResourceResetters.set(key, reset)
}

export function resetSessionResources() {
  for (const reset of sessionResourceResetters.values()) reset()
}
