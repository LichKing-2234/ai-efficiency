import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isChunkLoadError, reloadOnceForChunkError } from '@/utils/chunkReload'

describe('chunkReload', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('detects dynamic import chunk failures', () => {
    expect(isChunkLoadError(new Error('Loading chunk 12 failed'))).toBe(true)
    expect(isChunkLoadError(new Error('Loading CSS chunk 7 failed'))).toBe(true)
    expect(isChunkLoadError(new Error('boom'))).toBe(false)
  })

  it('reloads only once within the chunk reload window', () => {
    const reload = vi.fn()
    const error = new Error('Loading chunk 12 failed')

    expect(reloadOnceForChunkError(error, { reload, now: () => 1000 })).toBe(true)
    expect(reloadOnceForChunkError(error, { reload, now: () => 2000 })).toBe(false)
    expect(reloadOnceForChunkError(error, { reload, now: () => 12001 })).toBe(true)
    expect(reload).toHaveBeenCalledTimes(2)
  })

  it('treats invalid stored timestamps as stale and reloads again', () => {
    const reload = vi.fn()
    const error = new Error('Loading chunk 12 failed')
    const storage = {
      getItem: vi.fn().mockReturnValue('not-a-number'),
      setItem: vi.fn(),
    }

    expect(reloadOnceForChunkError(error, { reload, now: () => 1500, storage })).toBe(true)
    expect(storage.setItem).toHaveBeenCalledWith('chunk_reload_attempted', '1500')
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
