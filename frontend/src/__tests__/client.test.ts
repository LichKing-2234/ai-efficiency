import { describe, it, expect, vi, beforeEach } from 'vitest'

// Use a mutable container object so assignments inside vi.mock work
const interceptors = vi.hoisted(() => ({
  requestFn: null as ((config: any) => any) | null,
  responseFn: null as ((res: any) => any) | null,
  responseErrFn: null as ((err: any) => any) | null,
  axiosPost: vi.fn(),
  clientInstance: null as any,
}))

vi.mock('axios', () => {
  const mockInstance: any = vi.fn()
  mockInstance.interceptors = {
    request: {
      use: vi.fn((onFulfilled: any) => {
        interceptors.requestFn = onFulfilled
      }),
    },
    response: {
      use: vi.fn((onFulfilled: any, onRejected: any) => {
        interceptors.responseFn = onFulfilled
        interceptors.responseErrFn = onRejected
      }),
    },
  }
  mockInstance.get = vi.fn()
  mockInstance.post = vi.fn()
  mockInstance.put = vi.fn()
  mockInstance.delete = vi.fn()
  interceptors.clientInstance = mockInstance

  return {
    default: {
      create: vi.fn(() => mockInstance),
      post: interceptors.axiosPost,
    },
  }
})

// Import client to trigger the interceptor registration
import '@/api/client'

describe('Axios client interceptors', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  describe('request interceptor', () => {
    it('adds Bearer token from localStorage when token exists', () => {
      localStorage.setItem('token', 'my-jwt-token')
      const config = { headers: {} as Record<string, string> }
      const result = interceptors.requestFn!(config)
      expect(result.headers.Authorization).toBe('Bearer my-jwt-token')
    })

    it('does not add Authorization header when no token', () => {
      const config = { headers: {} as Record<string, string> }
      const result = interceptors.requestFn!(config)
      expect(result.headers.Authorization).toBeUndefined()
    })
  })

  describe('response interceptor', () => {
    it('passes through successful responses', () => {
      const response = { status: 200, data: { message: 'ok' } }
      const result = interceptors.responseFn!(response)
      expect(result).toBe(response)
    })

    it('refreshes token and retries the original request on 401 response', async () => {
      localStorage.setItem('token', 'old-token')
      localStorage.setItem('refresh_token', 'refresh-token')

      interceptors.axiosPost.mockResolvedValue({
        data: {
          data: {
            tokens: {
              access_token: 'new-token',
              refresh_token: 'new-refresh-token',
            },
          },
        },
      })
      const retriedResponse = { status: 200, data: { ok: true } }
      interceptors.clientInstance.mockResolvedValue(retriedResponse)

      const result = await interceptors.responseErrFn!({
        response: { status: 401 },
        config: { url: '/repos', headers: {} },
      })

      expect(interceptors.axiosPost).toHaveBeenCalledWith('/api/v1/auth/refresh', {
        refresh_token: 'refresh-token',
      })
      expect(interceptors.clientInstance).toHaveBeenCalledWith(
        expect.objectContaining({
          url: '/repos',
          headers: expect.objectContaining({
            Authorization: 'Bearer new-token',
          }),
          _retry: true,
        })
      )
      expect(localStorage.getItem('token')).toBe('new-token')
      expect(localStorage.getItem('refresh_token')).toBe('new-refresh-token')
      expect(result).toBe(retriedResponse)
    })

    it('clears tokens and redirects when refresh fails', async () => {
      localStorage.setItem('token', 'old-token')
      localStorage.setItem('refresh_token', 'refresh-token')

      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        writable: true,
        value: { ...originalLocation, href: '' },
      })

      interceptors.axiosPost.mockRejectedValue(new Error('refresh failed'))

      const error = { response: { status: 401 }, config: { url: '/repos', headers: {} } }
      await expect(interceptors.responseErrFn!(error)).rejects.toEqual(error)

      expect(localStorage.getItem('token')).toBeNull()
      expect(localStorage.getItem('refresh_token')).toBeNull()
      expect(window.location.href).toBe('/login')

      Object.defineProperty(window, 'location', {
        writable: true,
        value: originalLocation,
      })
    })

    it('does not clear token on non-401 errors', async () => {
      localStorage.setItem('token', 'valid-token')

      const error = { response: { status: 500 } }
      await expect(interceptors.responseErrFn!(error)).rejects.toEqual(error)

      expect(localStorage.getItem('token')).toBe('valid-token')
    })

    it('does not redirect on 401 for auth endpoints', async () => {
      localStorage.setItem('token', 'old-token')
      localStorage.setItem('refresh_token', 'old-refresh-token')

      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        writable: true,
        value: { ...originalLocation, href: '/current' },
      })

      const error = { response: { status: 401 }, config: { url: '/auth/login' } }
      await expect(interceptors.responseErrFn!(error)).rejects.toEqual(error)

      // Token should NOT be cleared for auth endpoints
      expect(localStorage.getItem('token')).toBe('old-token')
      expect(localStorage.getItem('refresh_token')).toBe('old-refresh-token')
      expect(window.location.href).toBe('/current')
      expect(interceptors.axiosPost).not.toHaveBeenCalled()

      Object.defineProperty(window, 'location', {
        writable: true,
        value: originalLocation,
      })
    })

    it('handles error without response object', async () => {
      const error = { message: 'Network Error' }
      await expect(interceptors.responseErrFn!(error)).rejects.toEqual(error)
    })
  })
})
