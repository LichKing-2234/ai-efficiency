const CHUNK_RELOAD_KEY = 'chunk_reload_attempted'
const CHUNK_RELOAD_WINDOW_MS = 10000

interface ChunkReloadOptions {
  reload?: () => void
  now?: () => number
  storage?: Pick<Storage, 'getItem' | 'setItem'>
}

export function isChunkLoadError(error: unknown): boolean {
  const err = error as { message?: string; name?: string }
  const message = typeof err?.message === 'string' ? err.message : String(error ?? '')
  return (
    message.includes('Failed to fetch dynamically imported module') ||
    message.includes('Loading chunk') ||
    message.includes('Loading CSS chunk') ||
    err?.name === 'ChunkLoadError'
  )
}

export function reloadOnceForChunkError(error: unknown, options: ChunkReloadOptions = {}) {
  if (!isChunkLoadError(error)) {
    return false
  }

  const storage = options.storage ?? sessionStorage
  const now = options.now ?? (() => Date.now())
  const reload = options.reload ?? (() => window.location.reload())
  const lastReload = storage.getItem(CHUNK_RELOAD_KEY)
  const parsedLastReload = lastReload ? Number.parseInt(lastReload, 10) : Number.NaN
  const hasValidLastReload = Number.isFinite(parsedLastReload)
  const currentTime = now()

  if (!hasValidLastReload || currentTime - parsedLastReload > CHUNK_RELOAD_WINDOW_MS) {
    storage.setItem(CHUNK_RELOAD_KEY, String(currentTime))
    reload()
    return true
  }

  return false
}
