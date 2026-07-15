import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

const axiosHarness = vi.hoisted(() => ({
  requestFn: null as ((config: any) => any) | null,
  responseFn: null as ((response: any) => any) | null,
  responseErrFn: null as ((error: any) => Promise<any>) | null,
  axiosPost: vi.fn(),
  clientInstance: null as any,
  getHandler: null as ((config: any) => any) | null,
  postHandler: null as ((url: string, data?: unknown, config?: any) => any) | null,
  retryHandler: null as ((config: any) => any) | null,
  retryRequestGate: null as Promise<void> | null,
}))

vi.mock('axios', () => {
  const mockInstance: any = vi.fn(async (config: any) => {
    await (axiosHarness.retryRequestGate ?? Promise.resolve())
    const request = await axiosHarness.requestFn!(config)
    if (!axiosHarness.retryHandler) {
      throw new Error(`No retry handler installed for ${request?.url ?? 'unknown request'}`)
    }
    return axiosHarness.retryHandler(request)
  })

  mockInstance.interceptors = {
    request: {
      use: vi.fn((onFulfilled: any) => {
        axiosHarness.requestFn = onFulfilled
      }),
    },
    response: {
      use: vi.fn((onFulfilled: any, onRejected: any) => {
        axiosHarness.responseFn = onFulfilled
        axiosHarness.responseErrFn = onRejected
      }),
    },
  }

  mockInstance.get = vi.fn(async (url: string, config: any = {}) => {
    let request = {
      ...config,
      url,
      method: 'get',
      headers: config.headers ?? {},
    }
    request = await axiosHarness.requestFn!(request)
    if (!axiosHarness.getHandler) {
      throw new Error(`No GET handler installed for ${url}`)
    }
    return axiosHarness.getHandler(request)
  })

  mockInstance.post = vi.fn(async (url: string, data?: unknown, config: any = {}) => {
    let request = {
      ...config,
      url,
      method: 'post',
      data,
      headers: config.headers ?? {},
    }
    request = await axiosHarness.requestFn!(request)
    if (!axiosHarness.postHandler) {
      throw new Error(`No POST handler installed for ${url}`)
    }
    return axiosHarness.postHandler(url, data, request)
  })

  mockInstance.put = vi.fn()
  mockInstance.delete = vi.fn()
  axiosHarness.clientInstance = mockInstance

  return {
    default: {
      create: vi.fn(() => mockInstance),
      post: axiosHarness.axiosPost,
    },
  }
})

import client from '@/api/client'
import { installAuthNavigationGuards } from '@/router/authGuard'
import { useAuthStore } from '@/stores/auth'
import {
  clearBrowserSession,
  onAuthExpiry,
  readBrowserSession,
  replaceBrowserSession,
} from '@/auth/browserSession'
import type { User } from '@/types'

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason?: unknown) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const alice: User = {
  id: 1,
  username: 'alice',
  email: 'alice@example.com',
  role: 'admin',
  auth_source: 'sso',
}

const bob: User = {
  id: 2,
  username: 'bob',
  email: 'bob@example.org',
  role: 'viewer',
  auth_source: 'ldap',
}

function userResponse(user: User) {
  return { data: { data: user } }
}

function tokenResponse(accessToken: string, refreshToken?: string) {
  return {
    data: {
      data: {
        token: accessToken,
        ...(refreshToken ? { refresh_token: refreshToken } : {}),
      },
    },
  }
}

function refreshResponse(accessToken: string, refreshToken?: string) {
  return {
    data: {
      data: {
        tokens: {
          access_token: accessToken,
          ...(refreshToken ? { refresh_token: refreshToken } : {}),
        },
      },
    },
  }
}

function rejectWith401(config: any) {
  const error = { response: { status: 401 }, config }
  return axiosHarness.responseErrFn!(error)
}

function routeComponent(name: string) {
  return { template: `<div data-route-skeleton="${name}">${name}</div>` }
}

function createRouteHarness(options: { oauthDeviceComponent?: Promise<any> } = {}) {
  const loaders = {
    login: vi.fn(() => Promise.resolve(routeComponent('login'))),
    oauthAuthorize: vi.fn(() => Promise.resolve(routeComponent('oauth-authorize'))),
    oauthDevice: vi.fn(() => options.oauthDeviceComponent ?? Promise.resolve(routeComponent('oauth-device'))),
    usage: vi.fn(() => Promise.resolve(routeComponent('usage'))),
    repos: vi.fn(() => Promise.resolve(routeComponent('repos'))),
  }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'Login', component: loaders.login, meta: { public: true } },
      {
        path: '/oauth/authorize',
        name: 'OAuthAuthorize',
        component: loaders.oauthAuthorize,
        meta: { public: true },
      },
      {
        path: '/oauth/device',
        name: 'OAuthDevice',
        component: loaders.oauthDevice,
        meta: { public: true, redirectOnAuthExpiry: true },
      },
      { path: '/', name: 'Dashboard', component: routeComponent('dashboard') },
      { path: '/usage', name: 'Usage', component: loaders.usage },
      { path: '/repos', name: 'RepoList', component: loaders.repos },
    ],
  })
  const dispose = installAuthNavigationGuards(router)
  return { router, loaders, dispose }
}

describe('Axios client interceptors', () => {
  let pinia: ReturnType<typeof createPinia>
  const routeDisposers: Array<() => void> = []

  beforeEach(() => {
    localStorage.clear()
    pinia = createPinia()
    setActivePinia(pinia)
    axiosHarness.getHandler = null
    axiosHarness.postHandler = null
    axiosHarness.retryHandler = null
    axiosHarness.retryRequestGate = null
    axiosHarness.axiosPost.mockReset()
    axiosHarness.clientInstance.mockClear()
    axiosHarness.clientInstance.get.mockClear()
    axiosHarness.clientInstance.post.mockClear()
    axiosHarness.clientInstance.put.mockClear()
    axiosHarness.clientInstance.delete.mockClear()
  })

  afterEach(() => {
    while (routeDisposers.length) {
      routeDisposers.pop()!()
    }
    disposePinia(pinia)
  })

  describe('request interceptor', () => {
    it('stamps the browser generation and bearer token on authenticated requests', () => {
      const session = replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const config = { url: '/repos', headers: {} as Record<string, string> }

      const result = axiosHarness.requestFn!(config)

      expect(result.headers.Authorization).toBe('Bearer token-a')
      expect(result._authGeneration).toBe(session.generation)
    })

    it('does not stamp or authorize an uncredentialed request', () => {
      const config = { url: '/auth/options', headers: {} as Record<string, string> }

      const result = axiosHarness.requestFn!(config)

      expect(result.headers.Authorization).toBeUndefined()
      expect(result._authGeneration).toBeUndefined()
    })
  })

  describe('response interceptor', () => {
    it('passes through successful responses', () => {
      const response = { status: 200, data: { message: 'ok' } }

      expect(axiosHarness.responseFn!(response)).toBe(response)
    })

    it('keeps logout authoritative while a generation A refresh is pending', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      const expiryEvents: unknown[] = []
      const stop = onAuthExpiry((event) => expiryEvents.push(event))
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const auth = useAuthStore()
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.getHandler = rejectWith401

      const requestA = auth.ensureUser()
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      auth.logout()
      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))

      await expect(requestA).resolves.toBeNull()
      expect(readBrowserSession().accessToken).toBeNull()
      expect(readBrowserSession().refreshToken).toBeNull()
      expect(axiosHarness.clientInstance).not.toHaveBeenCalled()
      expect(expiryEvents).toHaveLength(0)
      stop()
    })

    it('keeps normal login B authoritative while refresh A is pending', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const auth = useAuthStore()
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.getHandler = rejectWith401

      const requestA = auth.ensureUser()
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      axiosHarness.postHandler = (url) => {
        expect(url).toBe('/auth/login')
        return tokenResponse('token-b', 'refresh-b')
      }
      axiosHarness.getHandler = () => userResponse(bob)
      await expect(auth.login({ username: 'bob', password: 'test-password', source: 'LDAP' })).resolves.toEqual(bob)

      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))
      await expect(requestA).resolves.toBeNull()

      expect(auth.token).toBe('token-b')
      expect(auth.user).toEqual(bob)
      expect(localStorage.getItem('refresh_token')).toBe('refresh-b')
      expect(axiosHarness.clientInstance).not.toHaveBeenCalled()
    })

    it('keeps Dev Login authoritative while refresh A is pending', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const auth = useAuthStore()
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.getHandler = rejectWith401

      const requestA = auth.ensureUser()
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      axiosHarness.postHandler = (url) => {
        expect(url).toBe('/auth/dev-login')
        return tokenResponse('dev-token', 'dev-refresh')
      }
      axiosHarness.getHandler = () => userResponse(alice)
      await expect(auth.devLogin()).resolves.toEqual(alice)

      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))
      await expect(requestA).resolves.toBeNull()

      expect(auth.token).toBe('dev-token')
      expect(auth.user).toEqual(alice)
      expect(localStorage.getItem('refresh_token')).toBe('dev-refresh')
      expect(axiosHarness.clientInstance).not.toHaveBeenCalled()
    })

    it('ignores a 401 stamped by generation A after login B replaces it', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const requestA = axiosHarness.requestFn!({ url: '/repos', headers: {} })
      const expiryEvents: unknown[] = []
      const stop = onAuthExpiry((event) => expiryEvents.push(event))

      replaceBrowserSession({ accessToken: 'token-b', refreshToken: 'refresh-b' })
      const error = { response: { status: 401 }, config: requestA }

      await expect(axiosHarness.responseErrFn!(error)).rejects.toBe(error)
      expect(axiosHarness.axiosPost).not.toHaveBeenCalled()
      expect(readBrowserSession().accessToken).toBe('token-b')
      expect(readBrowserSession().refreshToken).toBe('refresh-b')
      expect(expiryEvents).toHaveLength(0)
      stop()
    })

    it('rotates A to A2, retries auth me once, and preserves its identity flight', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const generationA = readBrowserSession().generation
      const auth = useAuthStore()
      axiosHarness.axiosPost.mockResolvedValue(refreshResponse('token-a2', 'refresh-a2'))
      axiosHarness.getHandler = rejectWith401
      axiosHarness.retryHandler = (config) => {
        expect(config.url).toBe('/auth/me')
        expect(config._authGeneration).toBe(generationA)
        return userResponse(alice)
      }

      const first = auth.ensureUser()
      const second = auth.fetchMe()

      await expect(first).resolves.toEqual(alice)
      await expect(second).resolves.toEqual(alice)
      expect(axiosHarness.clientInstance.get).toHaveBeenCalledTimes(1)
      expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1)
      expect(axiosHarness.clientInstance).toHaveBeenCalledTimes(1)
      expect(auth.token).toBe('token-a2')
      expect(localStorage.getItem('token')).toBe('token-a2')
      expect(localStorage.getItem('refresh_token')).toBe('refresh-a2')
      expect(readBrowserSession().generation).toBe(generationA)
      expect(auth.user).toEqual(alice)
    })

    it('rejects retry A if replacement B lands after A2 rotation but before retry dispatch', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      const retryGate = deferred<void>()
      const retryAdapter = vi.fn(() => ({ status: 200, data: { ok: true } }))
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const generationA = readBrowserSession().generation
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.getHandler = rejectWith401
      axiosHarness.retryRequestGate = retryGate.promise
      axiosHarness.retryHandler = retryAdapter

      const requestA = client.get('/repos')
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))
      await vi.waitFor(() => {
        expect(readBrowserSession()).toEqual({
          generation: generationA,
          accessToken: 'token-a2',
          refreshToken: 'refresh-a2',
        })
        expect(axiosHarness.clientInstance).toHaveBeenCalledTimes(1)
      })

      const sessionB = replaceBrowserSession({ accessToken: 'token-b', refreshToken: 'refresh-b' })
      retryGate.resolve(undefined)

      await expect(requestA).rejects.toBeDefined()
      expect(retryAdapter).not.toHaveBeenCalled()
      expect(readBrowserSession()).toEqual(sessionB)
    })

    it('rejects retry A if logout clears A2 after rotation but before retry dispatch', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      const retryGate = deferred<void>()
      const retryAdapter = vi.fn(() => ({ status: 200, data: { ok: true } }))
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const generationA = readBrowserSession().generation
      const auth = useAuthStore()
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.getHandler = rejectWith401
      axiosHarness.retryRequestGate = retryGate.promise
      axiosHarness.retryHandler = retryAdapter

      const requestA = client.get('/repos')
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))
      await vi.waitFor(() => {
        expect(readBrowserSession()).toEqual({
          generation: generationA,
          accessToken: 'token-a2',
          refreshToken: 'refresh-a2',
        })
        expect(axiosHarness.clientInstance).toHaveBeenCalledTimes(1)
      })

      auth.logout()
      retryGate.resolve(undefined)

      await expect(requestA).rejects.toBeDefined()
      expect(retryAdapter).not.toHaveBeenCalled()
      expect(readBrowserSession().accessToken).toBeNull()
      expect(readBrowserSession().refreshToken).toBeNull()
    })

    it('shares one refresh flight between two 401 responses in the same generation', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.getHandler = rejectWith401
      axiosHarness.retryHandler = (config) => ({ status: 200, data: { url: config.url } })

      const first = client.get('/repos')
      const second = client.get('/events')
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))

      await expect(first).resolves.toEqual({ status: 200, data: { url: '/repos' } })
      await expect(second).resolves.toEqual({ status: 200, data: { url: '/events' } })
      expect(axiosHarness.clientInstance).toHaveBeenCalledTimes(2)
      expect(axiosHarness.axiosPost).toHaveBeenCalledWith('/api/v1/auth/refresh', {
        refresh_token: 'refresh-a',
      })
    })

    it('expires a matching session once after final refresh failure without navigating', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const generationA = readBrowserSession().generation
      const auth = useAuthStore()
      const expiryEvents: Array<{ expiredGeneration: number; clearedGeneration: number }> = []
      const stop = onAuthExpiry((event) => expiryEvents.push(event))
      const initialHref = window.location.href
      axiosHarness.axiosPost.mockRejectedValue(new Error('refresh failed'))
      axiosHarness.getHandler = rejectWith401

      await expect(auth.ensureUser()).resolves.toBeNull()

      expect(expiryEvents).toHaveLength(1)
      expect(expiryEvents[0].expiredGeneration).toBe(generationA)
      expect(readBrowserSession().generation).toBe(expiryEvents[0].clearedGeneration)
      expect(readBrowserSession().accessToken).toBeNull()
      expect(readBrowserSession().refreshToken).toBeNull()
      expect(window.location.href).toBe(initialHref)
      stop()
    })

    it('does not refresh or expire credential endpoints or requests sent without credentials', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const credentialRequest = axiosHarness.requestFn!({ url: '/auth/login', headers: {} })
      const credentialError = { response: { status: 401 }, config: credentialRequest }

      await expect(axiosHarness.responseErrFn!(credentialError)).rejects.toBe(credentialError)

      clearBrowserSession()
      const uncredentialedRequest = axiosHarness.requestFn!({ url: '/repos', headers: {} })
      replaceBrowserSession({ accessToken: 'token-b', refreshToken: 'refresh-b' })
      const uncredentialedError = { response: { status: 401 }, config: uncredentialedRequest }

      await expect(axiosHarness.responseErrFn!(uncredentialedError)).rejects.toBe(uncredentialedError)
      expect(axiosHarness.axiosPost).not.toHaveBeenCalled()
      expect(readBrowserSession().accessToken).toBe('token-b')
      expect(readBrowserSession().refreshToken).toBe('refresh-b')
    })

    it('expires a matching credentialed session when no refresh token exists', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: null })
      const request = axiosHarness.requestFn!({ url: '/repos', headers: {} })
      const error = { response: { status: 401 }, config: request }

      await expect(axiosHarness.responseErrFn!(error)).rejects.toBe(error)

      expect(axiosHarness.axiosPost).not.toHaveBeenCalled()
      expect(readBrowserSession().accessToken).toBeNull()
    })

    it('does not change a valid session for non-401 or network errors', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const serverError = { response: { status: 500 }, config: { url: '/repos' } }
      const networkError = { message: 'Network Error' }

      await expect(axiosHarness.responseErrFn!(serverError)).rejects.toBe(serverError)
      await expect(axiosHarness.responseErrFn!(networkError)).rejects.toBe(networkError)

      expect(readBrowserSession().accessToken).toBe('token-a')
      expect(axiosHarness.axiosPost).not.toHaveBeenCalled()
    })
  })

  describe('route-owned auth expiry policy', () => {
    it.each([
      {
        path: '/usage',
        expectedName: 'Login',
        expectedRedirect: '/usage',
      },
      {
        path: '/login?redirect=/repos',
        expectedName: 'Login',
        expectedRedirect: '/repos',
      },
      {
        path: '/oauth/authorize?client_id=client-test',
        expectedName: 'OAuthAuthorize',
        expectedRedirect: undefined,
      },
      {
        path: '/oauth/device',
        expectedName: 'Login',
        expectedRedirect: '/oauth/device',
      },
    ])('applies failed-refresh expiry to confirmed destination $path without hard navigation', async ({
      path,
      expectedName,
      expectedRedirect,
    }) => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const initialHref = window.location.href
      axiosHarness.getHandler = rejectWith401
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      const harness = createRouteHarness()
      routeDisposers.push(harness.dispose)

      await harness.router.push(path)
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))
      refresh.reject(new Error('refresh failed'))

      await vi.waitFor(() => expect(readBrowserSession().accessToken).toBeNull())
      await vi.waitFor(() => expect(harness.router.currentRoute.value.name).toBe(expectedName))

      expect(harness.router.currentRoute.value.query.redirect).toBe(expectedRedirect)
      expect(window.location.href).toBe(initialHref)
    })

    it('does not replay a handled expiry on a later tokenless OAuth Device navigation', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      axiosHarness.getHandler = rejectWith401
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      const harness = createRouteHarness()
      routeDisposers.push(harness.dispose)
      const replaceSpy = vi.spyOn(harness.router, 'replace')

      await harness.router.push('/oauth/device')
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))
      refresh.reject(new Error('refresh failed'))
      await vi.waitFor(() => expect(harness.router.currentRoute.value.name).toBe('Login'))
      expect(harness.router.currentRoute.value.query.redirect).toBe('/oauth/device')
      replaceSpy.mockClear()

      await harness.router.push('/oauth/device')
      await new Promise((resolve) => setTimeout(resolve, 0))

      expect(readBrowserSession().accessToken).toBeNull()
      expect(harness.router.currentRoute.value.name).toBe('OAuthDevice')
      expect(replaceSpy).not.toHaveBeenCalled()
    })

    it('does not lose a newer expiry while an older expiry callback is queued', async () => {
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: null })
      const auth = useAuthStore(pinia)
      auth.user = alice
      const harness = createRouteHarness()
      routeDisposers.push(harness.dispose)
      const replaceSpy = vi.spyOn(harness.router, 'replace')
      await harness.router.push('/oauth/device')

      const requestA = axiosHarness.requestFn!({ url: '/repos', headers: {} })
      const errorA = { response: { status: 401 }, config: requestA }
      const failureA = axiosHarness.responseErrFn!(errorA).catch((error) => error)

      replaceBrowserSession({ accessToken: 'token-b', refreshToken: null })
      const requestB = axiosHarness.requestFn!({ url: '/repos', headers: {} })
      const errorB = { response: { status: 401 }, config: requestB }
      const failureB = axiosHarness.responseErrFn!(errorB).catch((error) => error)

      await expect(Promise.all([failureA, failureB])).resolves.toEqual([errorA, errorB])
      await vi.waitFor(() => expect(harness.router.currentRoute.value.name).toBe('Login'))

      expect(harness.router.currentRoute.value.query.redirect).toBe('/oauth/device')
      expect(replaceSpy).toHaveBeenCalledTimes(1)
      expect(readBrowserSession().accessToken).toBeNull()
    })

    it('holds an expiry published during navigation for the destination that eventually confirms', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      const oauthDeviceComponent = deferred<any>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const auth = useAuthStore(pinia)
      auth.user = alice
      const harness = createRouteHarness({ oauthDeviceComponent: oauthDeviceComponent.promise })
      routeDisposers.push(harness.dispose)

      await harness.router.push('/usage')
      expect(harness.router.currentRoute.value.fullPath).toBe('/usage')

      replaceBrowserSession({ accessToken: 'token-b', refreshToken: 'refresh-b' })
      axiosHarness.getHandler = rejectWith401
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      const deviceNavigation = harness.router.push('/oauth/device')
      await vi.waitFor(() => expect(harness.loaders.oauthDevice).toHaveBeenCalledTimes(1))
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      refresh.reject(new Error('refresh failed'))
      await vi.waitFor(() => expect(readBrowserSession().accessToken).toBeNull())
      expect(harness.router.currentRoute.value.fullPath).toBe('/usage')

      oauthDeviceComponent.resolve(routeComponent('oauth-device'))
      await deviceNavigation
      await vi.waitFor(() => expect(harness.router.currentRoute.value.name).toBe('Login'))
      expect(harness.router.currentRoute.value.query.redirect).toBe('/oauth/device')

      await harness.router.push('/oauth/device')
      await new Promise((resolve) => setTimeout(resolve, 0))
      expect(harness.router.currentRoute.value.name).toBe('OAuthDevice')
    })

    it('keeps a pending Login follow-up valid across same-generation A to A2 refresh', async () => {
      const refresh = deferred<ReturnType<typeof refreshResponse>>()
      replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
      const generationA = readBrowserSession().generation
      const auth = useAuthStore(pinia)
      axiosHarness.getHandler = rejectWith401
      axiosHarness.axiosPost.mockReturnValue(refresh.promise)
      axiosHarness.retryHandler = (config) => {
        expect(config.url).toBe('/auth/me')
        expect(config._authGeneration).toBe(generationA)
        return userResponse(alice)
      }
      const harness = createRouteHarness()
      routeDisposers.push(harness.dispose)
      const replaceSpy = vi.spyOn(harness.router, 'replace')

      await harness.router.push('/login?redirect=/repos')
      expect(harness.router.currentRoute.value.name).toBe('Login')
      await vi.waitFor(() => expect(axiosHarness.axiosPost).toHaveBeenCalledTimes(1))

      refresh.resolve(refreshResponse('token-a2', 'refresh-a2'))

      await vi.waitFor(() => expect(harness.router.currentRoute.value.fullPath).toBe('/repos'))
      expect(replaceSpy).toHaveBeenCalledTimes(1)
      expect(readBrowserSession()).toEqual({
        generation: generationA,
        accessToken: 'token-a2',
        refreshToken: 'refresh-a2',
      })
      expect(auth.user).toEqual(alice)
      expect(window.location.href).not.toContain('/repos')
    })
  })
})
