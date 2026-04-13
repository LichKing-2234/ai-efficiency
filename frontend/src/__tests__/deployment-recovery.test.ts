import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isChunkLoadError, reloadOnceForChunkError, waitForServiceRecovery } from '@/utils/deploymentRecovery'

describe('deploymentRecovery', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('waits for /api/v1/health to recover and then reloads', async () => {
    const fetchImpl = vi.fn()
      .mockRejectedValueOnce(new Error('down'))
      .mockResolvedValueOnce({ ok: true })
    const reload = vi.fn()

    const promise = waitForServiceRecovery({
      fetchImpl,
      reload,
      retryDelayMs: 1000,
      maxRetries: 3,
    })

    await vi.runAllTimersAsync()
    await promise

    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(fetchImpl).toHaveBeenNthCalledWith(1, '/api/v1/health', {
      method: 'GET',
      cache: 'no-cache',
    })
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads anyway after retries are exhausted', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new Error('still down'))
    const reload = vi.fn()

    const promise = waitForServiceRecovery({
      fetchImpl,
      reload,
      retryDelayMs: 1000,
      maxRetries: 3,
    })

    await vi.runAllTimersAsync()
    await promise

    expect(fetchImpl).toHaveBeenCalledTimes(3)
    expect(reload).toHaveBeenCalledTimes(1)
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
})
