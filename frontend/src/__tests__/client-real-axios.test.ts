import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import client from '@/api/client'
import {
  clearBrowserSession,
  readBrowserSession,
  replaceBrowserSession,
} from '@/auth/browserSession'

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function refreshResponse(accessToken: string, refreshToken: string) {
  return {
    data: {
      data: {
        tokens: {
          access_token: accessToken,
          refresh_token: refreshToken,
        },
      },
    },
  }
}

function unauthorized(config: InternalAxiosRequestConfig) {
  return Object.assign(new Error('unauthorized'), {
    config,
    response: {
      status: 401,
      statusText: 'Unauthorized',
      headers: {},
      config,
      data: null,
    },
  })
}

describe('real Axios retry adapter boundary', () => {
  const originalAdapter = client.defaults.adapter
  const requestInterceptors: number[] = []

  beforeEach(() => {
    localStorage.clear()
    clearBrowserSession()
  })

  afterEach(() => {
    while (requestInterceptors.length) {
      client.interceptors.request.eject(requestInterceptors.pop()!)
    }
    client.defaults.adapter = originalAdapter
    vi.restoreAllMocks()
  })

  it.each([
    {
      name: 'replacement login B',
      invalidate: () => replaceBrowserSession({ accessToken: 'token-b', refreshToken: 'refresh-b' }),
      expectedToken: 'token-b',
    },
    {
      name: 'logout',
      invalidate: () => clearBrowserSession(),
      expectedToken: null,
    },
  ])('blocks an A retry when $name lands after interceptor validation but before adapter dispatch', async ({
    invalidate,
    expectedToken,
  }) => {
    const refresh = deferred<ReturnType<typeof refreshResponse>>()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const generationA = readBrowserSession().generation
    const adapter = vi.fn<AxiosAdapter>((config) => {
      if (adapter.mock.calls.length === 1) {
        return Promise.reject(unauthorized(config))
      }
      return Promise.resolve({
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
        data: { ok: true },
      })
    })
    client.defaults.adapter = adapter
    vi.spyOn(axios, 'post').mockReturnValue(refresh.promise as any)
    const interceptor = client.interceptors.request.use((config) => {
      if ((config as InternalAxiosRequestConfig & { _retry?: boolean })._retry) {
        queueMicrotask(() => queueMicrotask(invalidate))
      }
      return config
    })
    requestInterceptors.push(interceptor)

    const request = client.get('/repos')
    await vi.waitFor(() => expect(axios.post).toHaveBeenCalledTimes(1))
    refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))

    const outcome = await request.then(
      (value) => ({ value }),
      (error) => ({ error }),
    )

    expect(outcome).toHaveProperty('error')
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(readBrowserSession().accessToken).toBe(expectedToken)
    expect(readBrowserSession().generation).not.toBe(generationA)
  })

  it('dispatches one legal same-generation A to A2 retry through the custom adapter', async () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const generationA = readBrowserSession().generation
    const adapter = vi.fn<AxiosAdapter>((config) => {
      if (adapter.mock.calls.length === 1) {
        return Promise.reject(unauthorized(config))
      }
      return Promise.resolve({
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
        data: { ok: true },
      })
    })
    client.defaults.adapter = adapter
    vi.spyOn(axios, 'post').mockResolvedValue(refreshResponse('token-a2', 'refresh-a2') as any)

    await expect(client.get('/repos')).resolves.toMatchObject({ data: { ok: true } })

    expect(adapter).toHaveBeenCalledTimes(2)
    const retryConfig = adapter.mock.calls[1][0] as InternalAxiosRequestConfig & { _authGeneration?: number }
    expect(retryConfig._authGeneration).toBe(generationA)
    expect(retryConfig.headers.Authorization).toBe('Bearer token-a2')
    expect(readBrowserSession()).toEqual({
      generation: generationA,
      accessToken: 'token-a2',
      refreshToken: 'refresh-a2',
    })
  })
})
