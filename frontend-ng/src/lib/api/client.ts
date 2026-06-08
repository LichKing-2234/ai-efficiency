import type { ApiResponse } from '@/lib/api/types'

export class ApiError extends Error {
  status: number
  payload: unknown

  constructor(status: number, message: string, payload: unknown) {
    super(message)
    this.status = status
    this.payload = payload
  }
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path.startsWith('/api/') ? path : `/api/v1${path}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers || {})
    },
    ...init
  })
  const payload = (await res.json().catch(() => null)) as ApiResponse<T> | null
  if (!res.ok) {
    throw new ApiError(res.status, payload?.message || `Request failed with ${res.status}`, payload)
  }
  return payload?.data as T
}

export function encodeQuery(params?: Record<string, unknown>) {
  if (!params) return ''
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue
    query.set(key, String(value))
  }
  const text = query.toString()
  return text ? `?${text}` : ''
}
