import { describe, it, expect, vi, beforeEach } from 'vitest'

// Use a mutable container object so assignments inside vi.mock work
const interceptors = vi.hoisted(() => ({
  requestFn: null as ((config: any) => any) | null,
  responseFn: null as ((res: any) => any) | null,
  responseErrFn: null as ((err: any) => any) | null,
}))

vi.mock('axios', () => {
  const mockInstance = {
    interceptors: {
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
    },
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  }

  return {
    default: {
      create: vi.fn(() => mockInstance),
    },
  }
})

// Import client to trigger the interceptor registration
import '@/api/client'

describe('Axios client interceptors', () => {
  beforeEach(() => {
    localStorage.clear()
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

    it('clears token and redirects on 401 response', async () => {
      localStorage.setItem('token', 'old-token')

      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        writable: true,
        value: { ...originalLocation, href: '' },
      })

      const error = { response: { status: 401 }, config: { url: '/repos' } }
      await expect(interceptors.responseErrFn!(error)).rejects.toEqual(error)

      expect(localStorage.getItem('token')).toBeNull()
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

      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        writable: true,
        value: { ...originalLocation, href: '/current' },
      })

      const error = { response: { status: 401 }, config: { url: '/auth/login' } }
      await expect(interceptors.responseErrFn!(error)).rejects.toEqual(error)

      // Token should NOT be cleared for auth endpoints
      expect(localStorage.getItem('token')).toBe('old-token')
      expect(window.location.href).toBe('/current')

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
