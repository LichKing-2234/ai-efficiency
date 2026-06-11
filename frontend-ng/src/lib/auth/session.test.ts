import { beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError } from '@/lib/api/client'

const me = vi.fn()
const bootstrap = vi.fn()

vi.mock('@/lib/api', () => ({
  api: {
    auth: {
      me,
      bootstrap
    }
  }
}))

describe('ensureAuthenticatedUser', () => {
  beforeEach(() => {
    me.mockReset()
    bootstrap.mockReset()
  })

  test('bootstraps and retries only for 401 auth failures', async () => {
    const unauthenticated = new ApiError(401, 'not signed in', null)
    const user = { id: 1, role: 'admin' }
    me.mockRejectedValueOnce(unauthenticated).mockResolvedValueOnce(user)
    bootstrap.mockResolvedValue({ message: 'ok' })

    const { ensureAuthenticatedUser } = await import('./session')

    await expect(ensureAuthenticatedUser()).resolves.toEqual(user)
    expect(bootstrap).toHaveBeenCalledTimes(1)
    expect(me).toHaveBeenCalledTimes(2)
  })

  test('does not bootstrap when backend proxy is unavailable', async () => {
    const unavailable = new ApiError(502, 'backend is unavailable from frontend proxy', null)
    me.mockRejectedValue(unavailable)

    const { ensureAuthenticatedUser } = await import('./session')

    await expect(ensureAuthenticatedUser()).rejects.toBe(unavailable)
    expect(bootstrap).not.toHaveBeenCalled()
    expect(me).toHaveBeenCalledTimes(1)
  })
})
