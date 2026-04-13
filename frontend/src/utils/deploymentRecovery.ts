const CHUNK_RELOAD_KEY = 'chunk_reload_attempted'
const CHUNK_RELOAD_WINDOW_MS = 10000

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<{ ok: boolean }>

interface RecoveryOptions {
  fetchImpl?: FetchLike
  reload?: () => void
  healthUrl?: string
  retryDelayMs?: number
  maxRetries?: number
}

interface ChunkReloadOptions {
  reload?: () => void
  now?: () => number
  storage?: Pick<Storage, 'getItem' | 'setItem'>
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => {
    window.setTimeout(resolve, ms)
  })
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

export async function waitForServiceRecovery(options: RecoveryOptions = {}) {
  const fetchImpl = options.fetchImpl ?? (fetch as FetchLike)
  const reload = options.reload ?? (() => window.location.reload())
  const healthUrl = options.healthUrl ?? '/api/v1/health'
  const retryDelayMs = options.retryDelayMs ?? 1000
  const maxRetries = options.maxRetries ?? 5

  for (let attempt = 0; attempt < maxRetries; attempt += 1) {
    try {
      const response = await fetchImpl(healthUrl, {
        method: 'GET',
        cache: 'no-cache',
      })
      if (response.ok) {
        reload()
        return
      }
    } catch {
      // Service is still restarting.
    }

    if (attempt < maxRetries - 1) {
      await sleep(retryDelayMs)
    }
  }

  reload()
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
