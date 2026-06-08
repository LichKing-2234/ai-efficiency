import { api } from '@/lib/api'
import type { User } from '@/lib/api/types'

export async function ensureAuthenticatedUser(): Promise<User> {
  try {
    return await api.auth.me()
  } catch (firstError) {
    await api.auth.bootstrap()
    try {
      return await api.auth.me()
    } catch {
      throw firstError
    }
  }
}

