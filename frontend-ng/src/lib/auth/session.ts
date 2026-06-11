import { api } from '@/lib/api'
import { ApiError } from '@/lib/api/client'
import type { User } from '@/lib/api/types'

export async function ensureAuthenticatedUser(): Promise<User> {
  try {
    return await api.auth.me()
  } catch (firstError) {
    if (!(firstError instanceof ApiError) || (firstError.status !== 401 && firstError.status !== 403)) {
      throw firstError
    }
    await api.auth.bootstrap()
    try {
      return await api.auth.me()
    } catch {
      throw firstError
    }
  }
}
