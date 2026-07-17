import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import {
  onAuthExpiry,
  readBrowserSession,
  replaceBrowserSession,
} from '@/auth/browserSession'
import type { User } from '@/types'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

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

function userResponse(user: User | null | undefined) {
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

describe('Auth Store', () => {
  let pinia: ReturnType<typeof createPinia>

  beforeEach(() => {
    localStorage.clear()
    pinia = createPinia()
    setActivePinia(pinia)
    vi.clearAllMocks()
  })

  afterEach(() => {
    disposePinia(pinia)
  })

  it('starts unauthenticated when no token is persisted', () => {
    const store = useAuthStore()

    expect(store.isAuthenticated).toBe(false)
    expect(store.user).toBeNull()
    expect(store.token).toBeNull()
  })

  it('reads a token that was persisted before startup', () => {
    localStorage.setItem('token', 'saved-token')

    const store = useAuthStore()

    expect(store.token).toBe('saved-token')
    expect(store.isAuthenticated).toBe(true)
  })

  it('keeps the writable token adapter inside the browser-session owner', () => {
    const store = useAuthStore()
    const initialGeneration = readBrowserSession().generation

    store.token = 'token-a'

    expect(readBrowserSession()).toEqual({
      generation: initialGeneration + 1,
      accessToken: 'token-a',
      refreshToken: null,
    })

    store.token = null

    expect(readBrowserSession()).toEqual({
      generation: initialGeneration + 2,
      accessToken: null,
      refreshToken: null,
    })
  })

  it('logout clears both tokens and the current user', () => {
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()
    store.user = alice

    store.logout()

    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('token')).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('logout clears work-item counts from the previous user', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    workItemsApi.getWorkItemCounts.mockResolvedValue({
      data: {
        data: {
          quota_reset_approval_count: 2,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 2,
        },
      },
    })
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const auth = useAuthStore()
    const workItems = useWorkItemsStore()
    await workItems.loadCounts()

    auth.logout()

    expect(workItems.totalCount).toBe(0)
    expect(workItems.loaded).toBe(false)
    expect(workItems.error).toBe('')
  })

  it('two concurrent ensureUser and fetchMe callers share one generation request', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    const pending = deferred<ReturnType<typeof userResponse>>()
    ;(mockGetMe as any).mockReturnValue(pending.promise)
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()

    const ensured = store.ensureUser()
    const fetched = store.fetchMe()

    expect(mockGetMe).toHaveBeenCalledTimes(1)
    pending.resolve(userResponse(alice))

    await expect(ensured).resolves.toEqual(alice)
    await expect(fetched).resolves.toEqual(alice)
    expect(store.user).toEqual(alice)
  })

  it('ensureUser returns an already loaded user without issuing getMe', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()
    store.user = alice

    await expect(store.ensureUser()).resolves.toEqual(alice)

    expect(mockGetMe).not.toHaveBeenCalled()
  })

  it('ensureUser returns null without a token and does not issue getMe', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    const store = useAuthStore()

    await expect(store.ensureUser()).resolves.toBeNull()

    expect(mockGetMe).not.toHaveBeenCalled()
  })

  it('settles a transient getMe failure so the next call retries', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any)
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce(userResponse(alice))
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()

    await expect(store.ensureUser()).resolves.toBeNull()
    await expect(store.ensureUser()).resolves.toEqual(alice)

    expect(mockGetMe).toHaveBeenCalledTimes(2)
    expect(store.user).toEqual(alice)
  })

  it('logout invalidates a pending generation so it cannot restore the user', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    const pending = deferred<ReturnType<typeof userResponse>>()
    ;(mockGetMe as any).mockReturnValue(pending.promise)
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()

    const request = store.ensureUser()
    expect(mockGetMe).toHaveBeenCalledTimes(1)

    store.logout()
    pending.resolve(userResponse(alice))

    await expect(request).resolves.toBeNull()
    expect(store.user).toBeNull()
    expect(readBrowserSession().accessToken).toBeNull()
    expect(readBrowserSession().refreshToken).toBeNull()
  })

  it('normal login B starts a new identity request and invalidates pending A', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    const pendingA = deferred<ReturnType<typeof userResponse>>()
    const pendingB = deferred<ReturnType<typeof userResponse>>()
    ;(mockLogin as any).mockResolvedValue(tokenResponse('token-b', 'refresh-b'))
    ;(mockGetMe as any)
      .mockReturnValueOnce(pendingA.promise)
      .mockReturnValueOnce(pendingB.promise)
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()

    const requestA = store.ensureUser()
    const generationA = readBrowserSession().generation
    const loginB = store.login({ username: 'bob', password: 'test-password', source: 'LDAP' })
    await vi.waitFor(() => expect(mockGetMe).toHaveBeenCalledTimes(2))

    expect(readBrowserSession().generation).toBe(generationA + 1)
    pendingB.resolve(userResponse(bob))
    await expect(loginB).resolves.toEqual(bob)

    pendingA.resolve(userResponse(alice))
    await expect(requestA).resolves.toBeNull()
    expect(store.user).toEqual(bob)
    expect(readBrowserSession().accessToken).toBe('token-b')
  })

  it('same-token normal logins create distinct generations and identity requests', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    const firstIdentity = deferred<ReturnType<typeof userResponse>>()
    const secondIdentity = deferred<ReturnType<typeof userResponse>>()
    ;(mockLogin as any).mockResolvedValue(tokenResponse('token-a', 'refresh-a'))
    ;(mockGetMe as any)
      .mockReturnValueOnce(firstIdentity.promise)
      .mockReturnValueOnce(secondIdentity.promise)
    const store = useAuthStore()

    const firstLogin = store.login({ username: 'alice', password: 'test-password', source: 'SSO' })
    await vi.waitFor(() => expect(mockGetMe).toHaveBeenCalledTimes(1))
    const firstGeneration = readBrowserSession().generation

    const secondLogin = store.login({ username: 'bob', password: 'test-password', source: 'SSO' })
    await vi.waitFor(() => expect(mockGetMe).toHaveBeenCalledTimes(2))
    const secondGeneration = readBrowserSession().generation

    expect(secondGeneration).toBe(firstGeneration + 1)
    secondIdentity.resolve(userResponse(bob))
    await expect(secondLogin).resolves.toEqual(bob)

    firstIdentity.resolve(userResponse(alice))
    await expect(firstLogin).resolves.toBeNull()
    expect(store.user).toEqual(bob)
  })

  it('login stores nested tokens and fetches the current user', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: {
        data: {
          tokens: { access_token: 'token-a', refresh_token: 'refresh-a' },
        },
      },
    })
    ;(mockGetMe as any).mockResolvedValue(userResponse(alice))
    const store = useAuthStore()

    await expect(store.login({ username: 'alice', password: 'test-password', source: 'SSO' })).resolves.toEqual(alice)

    expect(store.token).toBe('token-a')
    expect(store.isAuthenticated).toBe(true)
    expect(localStorage.getItem('token')).toBe('token-a')
    expect(localStorage.getItem('refresh_token')).toBe('refresh-a')
    expect(store.user).toEqual(alice)
  })

  it('login with no refresh token removes the previous generation refresh token', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    replaceBrowserSession({ accessToken: 'old-token', refreshToken: 'old-refresh' })
    ;(mockLogin as any).mockResolvedValue(tokenResponse('token-a'))
    ;(mockGetMe as any).mockResolvedValue(userResponse(alice))
    const store = useAuthStore()

    await store.login({ username: 'alice', password: 'test-password', source: 'SSO' })

    expect(store.token).toBe('token-a')
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('login with no response data leaves the current session unchanged', async () => {
    const { login: mockLogin } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({ data: { data: null } })
    const store = useAuthStore()
    const before = readBrowserSession()

    await expect(store.login({ username: 'alice', password: 'test-password', source: 'SSO' })).resolves.toBeNull()

    expect(readBrowserSession()).toEqual(before)
  })

  it('rejects a login payload with no access token', async () => {
    const { login: mockLogin } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { refresh_token: 'refresh-a' } },
    })
    const store = useAuthStore()

    await expect(store.login({ username: 'alice', password: 'test-password', source: 'SSO' }))
      .rejects.toThrow('login response missing access token')

    expect(store.token).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('devLogin installs its tokens and shares the current-user path', async () => {
    const { devLogin: mockDevLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockDevLogin as any).mockResolvedValue(tokenResponse('dev-token', 'dev-refresh'))
    ;(mockGetMe as any).mockResolvedValue(userResponse(alice))
    const store = useAuthStore()

    await expect(store.devLogin()).resolves.toEqual(alice)

    expect(store.token).toBe('dev-token')
    expect(localStorage.getItem('refresh_token')).toBe('dev-refresh')
    expect(store.user).toEqual(alice)
  })

  it('devLogin with no response data does not replace the session', async () => {
    const { devLogin: mockDevLogin } = await import('@/api/auth')
    ;(mockDevLogin as any).mockResolvedValue({ data: { data: null } })
    const store = useAuthStore()
    const before = readBrowserSession()

    await expect(store.devLogin()).resolves.toBeNull()

    expect(readBrowserSession()).toEqual(before)
  })

  it('sets the user to null after a current non-401 fetchMe error', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any).mockRejectedValue(new Error('temporary failure'))
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()
    store.user = alice

    await expect(store.fetchMe()).resolves.toBeNull()

    expect(store.user).toBeNull()
    expect(store.token).toBe('token-a')
  })

  it('sets the user to null when current response data is missing', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any).mockResolvedValue(userResponse(undefined))
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()

    await expect(store.fetchMe()).resolves.toBeNull()

    expect(store.user).toBeNull()
  })

  it('expires a current mocked 401 once but ignores a stale-generation 401', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    const staleFailure = deferred<ReturnType<typeof userResponse>>()
    const expiryEvents: Array<{ expiredGeneration: number; clearedGeneration: number }> = []
    const stop = onAuthExpiry((event) => expiryEvents.push(event))
    ;(mockGetMe as any)
      .mockRejectedValueOnce({ response: { status: 401 } })
      .mockReturnValueOnce(staleFailure.promise)
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a' })
    const store = useAuthStore()
    const generationA = readBrowserSession().generation

    await expect(store.ensureUser()).resolves.toBeNull()

    expect(expiryEvents).toHaveLength(1)
    expect(expiryEvents[0].expiredGeneration).toBe(generationA)
    expect(readBrowserSession().accessToken).toBeNull()

    replaceBrowserSession({ accessToken: 'token-b', refreshToken: 'refresh-b' })
    const staleRequest = store.ensureUser()
    replaceBrowserSession({ accessToken: 'token-a', refreshToken: 'refresh-a2' })
    staleFailure.reject({ response: { status: 401 } })

    await expect(staleRequest).resolves.toBeNull()
    expect(expiryEvents).toHaveLength(1)
    expect(readBrowserSession().accessToken).toBe('token-a')
    expect(readBrowserSession().refreshToken).toBe('refresh-a2')
    stop()
  })
})
