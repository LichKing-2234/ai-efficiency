import axios from 'axios'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import {
  expireBrowserSession,
  readBrowserSession,
  rotateBrowserSession,
} from '@/auth/browserSession'
import type { BrowserSessionSnapshot } from '@/auth/browserSession'

const apiBaseURL = import.meta.env.VITE_API_URL || '/api/v1'

const client = axios.create({
  baseURL: apiBaseURL,
  timeout: 15000,
  headers: {
    'Content-Type': 'application/json',
  },
})

type AuthenticatedRequestConfig = InternalAxiosRequestConfig & {
  _authGeneration?: number
  _retry?: boolean
}

type RefreshFlight = {
  generation: number
  promise: Promise<string | null>
}

let refreshFlight: RefreshFlight | null = null
const guardedRetryAdapters = new WeakSet<AxiosAdapter>()

function isCredentialAuthEndpoint(url?: string) {
  if (!url) {
    return false
  }
  return url.startsWith('/auth/login') || url.startsWith('/auth/refresh') || url.startsWith('/auth/dev-login')
}

function guardRetryAdapter(config: AuthenticatedRequestConfig) {
  if (typeof config.adapter === 'function' && guardedRetryAdapters.has(config.adapter)) {
    return
  }

  const expectedGeneration = config._authGeneration
  const adapter = axios.getAdapter(config.adapter)
  const guardedAdapter: AxiosAdapter = (adapterConfig) => {
    const session = readBrowserSession()
    if (
      expectedGeneration === undefined
      || session.generation !== expectedGeneration
      || !session.accessToken
    ) {
      throw new Error('authenticated retry session is no longer current')
    }
    adapterConfig.headers.Authorization = `Bearer ${session.accessToken}`
    return adapter(adapterConfig)
  }
  guardedRetryAdapters.add(guardedAdapter)
  config.adapter = guardedAdapter
}

async function refreshAccessToken(captured: BrowserSessionSnapshot): Promise<string | null> {
  if (!captured.refreshToken) {
    return null
  }

  const res = await axios.post(`${apiBaseURL}/auth/refresh`, {
    refresh_token: captured.refreshToken,
  })
  const data = res.data?.data
  const accessToken = data?.tokens?.access_token || data?.token
  if (!accessToken) {
    throw new Error('refresh response missing access token')
  }
  const refreshToken = data?.tokens?.refresh_token || data?.refresh_token || captured.refreshToken
  const rotated = rotateBrowserSession(captured.generation, { accessToken, refreshToken })
  return rotated?.accessToken ?? null
}

client.interceptors.request.use((config) => {
  const session = readBrowserSession()
  const authenticatedConfig = config as AuthenticatedRequestConfig
  if (authenticatedConfig._retry) {
    if (
      authenticatedConfig._authGeneration === undefined
      || authenticatedConfig._authGeneration !== session.generation
      || !session.accessToken
    ) {
      throw new Error('authenticated retry session is no longer current')
    }
    authenticatedConfig.headers.Authorization = `Bearer ${session.accessToken}`
    guardRetryAdapter(authenticatedConfig)
    return authenticatedConfig
  }
  if (session.accessToken) {
    authenticatedConfig.headers.Authorization = `Bearer ${session.accessToken}`
    authenticatedConfig._authGeneration = session.generation
  }
  return authenticatedConfig
})

client.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config as AuthenticatedRequestConfig | undefined
    const capturedGeneration = originalRequest?._authGeneration
    if (
      error.response?.status !== 401
      || !originalRequest
      || capturedGeneration === undefined
      || isCredentialAuthEndpoint(originalRequest.url)
      || originalRequest._retry
    ) {
      return Promise.reject(error)
    }

    const captured = readBrowserSession()
    if (captured.generation !== capturedGeneration || !captured.accessToken) {
      return Promise.reject(error)
    }
    if (!captured.refreshToken) {
      expireBrowserSession(capturedGeneration)
      return Promise.reject(error)
    }

    let flight = refreshFlight
    if (!flight || flight.generation !== capturedGeneration) {
      flight = {
        generation: capturedGeneration,
        promise: refreshAccessToken(captured),
      }
      refreshFlight = flight
    }

    try {
      const accessToken = await flight.promise
      if (!accessToken || readBrowserSession().generation !== capturedGeneration) {
        return Promise.reject(error)
      }

      originalRequest._retry = true
      originalRequest.headers = originalRequest.headers || {} as AuthenticatedRequestConfig['headers']
      originalRequest.headers.Authorization = `Bearer ${accessToken}`
      return client(originalRequest)
    } catch {
      expireBrowserSession(capturedGeneration)
      return Promise.reject(error)
    } finally {
      if (refreshFlight === flight) {
        refreshFlight = null
      }
    }
  },
)

export default client
