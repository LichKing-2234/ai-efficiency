export type WebVitalMetric = 'LCP' | 'INP' | 'CLS' | 'TTFB'
export type WebVitalNavigationType = 'navigate' | 'reload' | 'back-forward' | 'back-forward-cache' | 'prerender' | 'restore'

export interface WebVitalPayload {
  metric: WebVitalMetric
  value: number
  route: string
  navigation_type: WebVitalNavigationType
}

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Pick<Response, 'ok' | 'status'>>

const apiBaseURL = import.meta.env.VITE_API_URL || '/api/v1'

export async function submitWebVital(payload: WebVitalPayload, token: string, fetchImpl: FetchLike = fetch): Promise<void> {
  if (!token) {
    throw new Error('web vitals access token is required')
  }
  const response = await fetchImpl(`${apiBaseURL}/telemetry/web-vitals`, {
    method: 'POST',
    keepalive: true,
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
  if (!response.ok) {
    throw new Error(`web vitals request failed with status ${response.status}`)
  }
}
