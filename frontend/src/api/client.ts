import axios from 'axios'

const apiBaseURL = import.meta.env.VITE_API_URL || '/api/v1'

const client = axios.create({
  baseURL: apiBaseURL,
  timeout: 15000,
  headers: {
    'Content-Type': 'application/json',
  },
})

let refreshPromise: Promise<string | null> | null = null

function clearAuthAndRedirect() {
  localStorage.removeItem('token')
  localStorage.removeItem('refresh_token')
  window.location.href = '/login'
}

function isCredentialAuthEndpoint(url?: string) {
  if (!url) {
    return false
  }
  return url.startsWith('/auth/login') || url.startsWith('/auth/refresh') || url.startsWith('/auth/dev-login')
}

async function refreshAccessToken(): Promise<string | null> {
  const currentRefreshToken = localStorage.getItem('refresh_token')
  if (!currentRefreshToken) {
    return null
  }

  const res = await axios.post(`${apiBaseURL}/auth/refresh`, {
    refresh_token: currentRefreshToken,
  })
  const data = res.data?.data
  const accessToken = data?.tokens?.access_token || data?.token
  const nextRefreshToken = data?.tokens?.refresh_token || data?.refresh_token || currentRefreshToken
  if (!accessToken) {
    return null
  }

  localStorage.setItem('token', accessToken)
  localStorage.setItem('refresh_token', nextRefreshToken)
  return accessToken
}

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config as any
    if (error.response?.status === 401 && !isCredentialAuthEndpoint(originalRequest?.url) && !originalRequest?._retry) {
      originalRequest._retry = true
      try {
        if (!refreshPromise) {
          refreshPromise = refreshAccessToken().finally(() => {
            refreshPromise = null
          })
        }
        const accessToken = await refreshPromise
        if (accessToken) {
          originalRequest.headers = originalRequest.headers || {}
          originalRequest.headers.Authorization = `Bearer ${accessToken}`
          return client(originalRequest)
        }
      } catch {
        // Fall through to logout + redirect below.
      }
      clearAuthAndRedirect()
    }
    return Promise.reject(error)
  }
)

export default client
