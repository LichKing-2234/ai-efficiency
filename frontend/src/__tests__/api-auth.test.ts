import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => {
  return {
    default: {
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
    },
  }
})

import client from '@/api/client'
import { login, devLogin, refresh, getMe } from '@/api/auth'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('auth API', () => {
  it('login calls POST /auth/login with credentials', async () => {
    mockClient.post.mockResolvedValue({
      data: { data: { token: 'jwt-token', refresh_token: 'rt' } },
    })
    const req = { username: 'admin', password: 'pass', source: 'SSO' }
    await login(req)
    expect(mockClient.post).toHaveBeenCalledWith('/auth/login', req)
  })

  it('devLogin calls POST /auth/dev-login', async () => {
    mockClient.post.mockResolvedValue({
      data: { data: { token: 'dev-token', refresh_token: 'dev-rt' } },
    })
    await devLogin()
    expect(mockClient.post).toHaveBeenCalledWith('/auth/dev-login')
  })

  it('refresh calls POST /auth/refresh with refresh_token', async () => {
    mockClient.post.mockResolvedValue({
      data: { data: { token: 'new-token' } },
    })
    await refresh('old-refresh-token')
    expect(mockClient.post).toHaveBeenCalledWith('/auth/refresh', { refresh_token: 'old-refresh-token' })
  })

  it('getMe calls GET /auth/me', async () => {
    mockClient.get.mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'a@b.com', role: 'admin' } },
    })
    await getMe()
    expect(mockClient.get).toHaveBeenCalledWith('/auth/me')
  })

  it('login returns response data', async () => {
    const responseData = { token: 'jwt-token', refresh_token: 'rt' }
    mockClient.post.mockResolvedValue({ data: { data: responseData } })
    const result = await login({ username: 'u', password: 'p', source: 'SSO' })
    expect(result.data.data).toEqual(responseData)
  })

  it('devLogin returns response data', async () => {
    const responseData = { token: 'dev-token', refresh_token: 'dev-rt' }
    mockClient.post.mockResolvedValue({ data: { data: responseData } })
    const result = await devLogin()
    expect(result.data.data).toEqual(responseData)
  })

  it('refresh returns new token', async () => {
    mockClient.post.mockResolvedValue({ data: { data: { token: 'refreshed' } } })
    const result = await refresh('rt')
    expect(result.data.data.token).toBe('refreshed')
  })

  it('getMe returns user data', async () => {
    const user = { id: 1, username: 'admin', email: 'a@b.com', role: 'admin' }
    mockClient.get.mockResolvedValue({ data: { data: user } })
    const result = await getMe()
    expect(result.data.data).toEqual(user)
  })
})
