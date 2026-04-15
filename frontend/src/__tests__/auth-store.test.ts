import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

// Mock the auth API
vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

describe('Auth Store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })

  it('starts unauthenticated when no token in localStorage', () => {
    const store = useAuthStore()
    expect(store.isAuthenticated).toBe(false)
    expect(store.user).toBeNull()
    expect(store.token).toBeNull()
  })

  it('reads token from localStorage on init', () => {
    localStorage.setItem('token', 'saved-token')
    const store = useAuthStore()
    expect(store.token).toBe('saved-token')
    expect(store.isAuthenticated).toBe(true)
  })

  it('logout clears token and user', () => {
    localStorage.setItem('token', 'test-token')
    localStorage.setItem('refresh_token', 'test-refresh')
    const store = useAuthStore()
    store.user = { id: 1, username: 'admin', email: 'a@b.com', role: 'admin', auth_source: 'sso' }

    store.logout()

    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('token')).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('login stores token and fetches user', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { token: 'new-token', refresh_token: 'new-refresh' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'a@b.com', role: 'admin' } },
    })

    const store = useAuthStore()
    await store.login({ username: 'admin', password: 'pass', source: 'SSO' })

    expect(store.token).toBe('new-token')
    expect(store.isAuthenticated).toBe(true)
    expect(localStorage.getItem('token')).toBe('new-token')
    expect(localStorage.getItem('refresh_token')).toBe('new-refresh')
    expect(store.user?.username).toBe('admin')
  })

  it('fetchMe sets user to null on error', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any).mockRejectedValue(new Error('unauthorized'))

    const store = useAuthStore()
    store.user = { id: 1, username: 'admin', email: 'a@b.com', role: 'admin', auth_source: 'sso' }

    await store.fetchMe()
    expect(store.user).toBeNull()
  })

  it('fetchMe clears stored tokens on unauthorized error', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any).mockRejectedValue({
      response: { status: 401 },
    })

    localStorage.setItem('token', 'saved-token')
    localStorage.setItem('refresh_token', 'saved-refresh')

    const store = useAuthStore()
    store.user = { id: 1, username: 'admin', email: 'a@b.com', role: 'admin', auth_source: 'sso' }

    await store.fetchMe()

    expect(store.user).toBeNull()
    expect(store.token).toBeNull()
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('token')).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  // --- New tests for uncovered branches ---

  it('login with no data does not set token', async () => {
    const { login: mockLogin } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: null },
    })

    const store = useAuthStore()
    await store.login({ username: 'admin', password: 'pass', source: 'SSO' })

    expect(store.token).toBeNull()
    expect(store.isAuthenticated).toBe(false)
  })

  it('login without refresh_token does not store refresh_token', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { token: 'new-token' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'a@b.com', role: 'admin' } },
    })

    const store = useAuthStore()
    await store.login({ username: 'admin', password: 'pass', source: 'SSO' })

    expect(store.token).toBe('new-token')
    expect(localStorage.getItem('token')).toBe('new-token')
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('fetchMe sets user from response data', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 2, username: 'user2', email: 'u2@b.com', role: 'viewer' } },
    })

    const store = useAuthStore()
    await store.fetchMe()

    expect(store.user).toEqual({ id: 2, username: 'user2', email: 'u2@b.com', role: 'viewer' })
  })

  it('fetchMe sets user to null when data is undefined', async () => {
    const { getMe: mockGetMe } = await import('@/api/auth')
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: undefined },
    })

    const store = useAuthStore()
    await store.fetchMe()

    expect(store.user).toBeNull()
  })

  it('login calls fetchMe after setting token', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { token: 'tok', refresh_token: 'rt' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'a@b.com', role: 'admin' } },
    })

    const store = useAuthStore()
    await store.login({ username: 'admin', password: 'pass', source: 'LDAP' })

    expect(mockGetMe).toHaveBeenCalled()
    expect(store.user?.username).toBe('admin')
  })
})
