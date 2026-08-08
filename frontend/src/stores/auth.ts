import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import { devLogin as apiDevLogin, getMe, login as apiLogin } from '@/api/auth'
import { resetSessionResources } from '@/stores/sessionResources'
import {
  clearBrowserSession,
  expireBrowserSession,
  onBrowserSessionTransition,
  readBrowserSession,
  replaceBrowserSession,
} from '@/auth/browserSession'
import type { AuthTokenPayload } from '@/api/auth'
import type { User, LoginRequest } from '@/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const tokenValue = ref<string | null>(readBrowserSession().accessToken)
  let currentUserRequest: CurrentUserRequest | null = null

  const stopSessionTransition = onBrowserSessionTransition((event) => {
    tokenValue.value = event.current.accessToken
    if (event.kind !== 'rotate') {
      user.value = null
      currentUserRequest = null
      resetSessionResources()
    }
  })
  onScopeDispose(stopSessionTransition)

  const token = computed<string | null>({
    get: () => tokenValue.value,
    set: (value) => {
      if (value) {
        replaceBrowserSession({ accessToken: value, refreshToken: null })
      } else {
        clearBrowserSession()
      }
    },
  })

  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  async function installLoginPayload(payload: AuthTokenPayload): Promise<User | null> {
    const accessToken = payload.tokens?.access_token || payload.token
    const refreshToken = payload.tokens?.refresh_token || payload.refresh_token || null
    if (!accessToken) {
      throw new Error('login response missing access token')
    }

    replaceBrowserSession({ accessToken, refreshToken })
    return ensureUser()
  }

  async function login(req: LoginRequest): Promise<User | null> {
    const res = await apiLogin(req)
    const data = res.data.data
    if (!data) {
      return null
    }
    return installLoginPayload(data)
  }

  async function devLogin(): Promise<User | null> {
    const res = await apiDevLogin()
    const data = res.data.data
    if (!data) {
      return null
    }
    return installLoginPayload(data)
  }

  function fetchMe(): Promise<User | null> {
    const captured = readBrowserSession()
    if (!captured.accessToken) {
      user.value = null
      return Promise.resolve(null)
    }
    if (currentUserRequest?.generation === captured.generation) {
      return currentUserRequest.promise
    }

    let request!: CurrentUserRequest
    const promise = (async (): Promise<User | null> => {
      try {
        const res = await getMe()
        if (readBrowserSession().generation !== captured.generation) {
          return null
        }
        const currentUser = res.data.data ?? null
        user.value = currentUser
        return currentUser
      } catch (error: any) {
        if (readBrowserSession().generation !== captured.generation) {
          return null
        }
        user.value = null
        if (error?.response?.status === 401) {
          expireBrowserSession(captured.generation)
        }
        return null
      } finally {
        if (currentUserRequest === request) {
          currentUserRequest = null
        }
      }
    })()
    request = { generation: captured.generation, promise }
    currentUserRequest = request
    return promise
  }

  function ensureUser(): Promise<User | null> {
    if (user.value) {
      return Promise.resolve(user.value)
    }
    if (!readBrowserSession().accessToken) {
      return Promise.resolve(null)
    }
    return fetchMe()
  }

  function logout() {
    clearBrowserSession()
  }

  return { user, token, isAuthenticated, isAdmin, login, devLogin, logout, fetchMe, ensureUser }
})

type CurrentUserRequest = {
  generation: number
  promise: Promise<User | null>
}
