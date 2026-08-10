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
    /Failed to fetch dynamically imported module|Loading(?: CSS)? chunk/.test(message) ||
    err?.name === 'ChunkLoadError'
  )
}

export function reloadOnceForChunkError(error: unknown, options: ChunkReloadOptions = {}) {
  if (!isChunkLoadError(error)) {
    return false
  }

  const storage = options.storage ?? sessionStorage
  const now = options.now ?? Date.now
  const reload = options.reload ?? (() => window.location.reload())
  const lastReload = Number.parseInt(storage.getItem(CHUNK_RELOAD_KEY) ?? '', 10)
  const currentTime = now()

  if (!Number.isFinite(lastReload) || currentTime - lastReload > CHUNK_RELOAD_WINDOW_MS) {
    storage.setItem(CHUNK_RELOAD_KEY, String(currentTime))
    reload()
    return true
  }

  return false
}
