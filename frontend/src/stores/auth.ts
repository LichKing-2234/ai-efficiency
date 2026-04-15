import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login as apiLogin, getMe } from '@/api/auth'
import type { User, LoginRequest } from '@/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const token = ref<string | null>(localStorage.getItem('token'))

  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  async function login(req: LoginRequest) {
    const res = await apiLogin(req)
    const data = res.data.data
    if (data) {
      const accessToken = data.tokens?.access_token || data.token
      const refreshToken = data.tokens?.refresh_token || data.refresh_token
      if (!accessToken) {
        throw new Error('login response missing access token')
      }
      token.value = accessToken
      localStorage.setItem('token', accessToken)
      if (refreshToken) {
        localStorage.setItem('refresh_token', refreshToken)
      }
      await fetchMe()
    }
  }

  async function fetchMe() {
    try {
      const res = await getMe()
      user.value = res.data.data ?? null
    } catch (error: any) {
      user.value = null
      if (error?.response?.status === 401) {
        logout()
      }
    }
  }

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('token')
    localStorage.removeItem('refresh_token')
  }

  return { user, token, isAuthenticated, isAdmin, login, logout, fetchMe }
})
