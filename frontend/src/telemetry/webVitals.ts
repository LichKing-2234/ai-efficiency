import type { Metric } from 'web-vitals'
import { submitWebVital, type WebVitalMetric, type WebVitalPayload } from '@/api/telemetry'

const staticRoutes = new Set([
  '/login',
  '/oauth/authorize',
  '/oauth/device',
  '/usage',
  '/work-items',
  '/usage/team',
  '/usage/quota-reset',
  '/repos',
  '/user',
  '/admin/users',
  '/admin/directory/offboarding',
  '/settings',
])

type SubmitWebVital = (payload: WebVitalPayload, token: string) => Promise<void>
type WebVitalsModule = Pick<typeof import('web-vitals'), 'onLCP' | 'onINP' | 'onCLS' | 'onTTFB'>

export interface WebVitalsReportingOptions {
  token?: string | null
  path?: string
  sampleRate?: number
  random?: () => number
  submit?: SubmitWebVital
  loadWebVitals?: () => Promise<WebVitalsModule>
}

export interface WebVitalsReadyRouter {
  isReady: () => Promise<void>
  currentRoute: { value: { path: string } }
}

export type WebVitalsAfterRouterReadyOptions = Omit<WebVitalsReportingOptions, 'token' | 'path'>

export function readWebVitalsSampleRate(raw: string | undefined): number {
  if (raw === undefined || raw.trim() === '') {
    return 0.1
  }
  const parsed = Number(raw)
  if (!Number.isFinite(parsed)) {
    return 0.1
  }
  return Math.min(1, Math.max(0, parsed))
}

export function normalizeWebVitalsRoute(raw: string): string {
  if (!raw || raw.length > 256 || !raw.startsWith('/') || raw.startsWith('//')) {
    return 'unmatched'
  }
  const delimiter = raw.search(/[?#]/)
  let path = delimiter >= 0 ? raw.slice(0, delimiter) : raw
  if (path.length > 1 && path.endsWith('/')) {
    path = path.slice(0, -1)
  }
  if (staticRoutes.has(path)) {
    return path
  }
  const parts = path.split('/')
  if (parts.length === 3 && parts[1] === 'repos' && parts[2]) {
    return '/repos/:id'
  }
  if (parts.length === 4 && parts[1] === 'usage' && parts[2] === 'members' && parts[3]) {
    return '/usage/members/:user_id'
  }
  return 'unmatched'
}

export function startWebVitalsReporting(options: WebVitalsReportingOptions = {}): boolean {
  const token = options.token === undefined ? localStorage.getItem('token') : options.token
  if (!token) {
    return false
  }
  const sampleRate = options.sampleRate ?? readWebVitalsSampleRate(import.meta.env.VITE_WEB_VITALS_SAMPLE_RATE)
  const random = options.random ?? Math.random
  if (random() >= sampleRate) {
    return false
  }
  const route = normalizeWebVitalsRoute(options.path ?? window.location.pathname)
  const submit = options.submit ?? submitWebVital
  const loadWebVitals = options.loadWebVitals ?? (() => import('web-vitals'))
  const report = (metric: Metric) => {
    const payload: WebVitalPayload = {
      metric: metric.name as WebVitalMetric,
      value: metric.value,
      route,
      navigation_type: metric.navigationType,
    }
    void submit(payload, token).catch(() => undefined)
  }

  void loadWebVitals()
    .then(({ onLCP, onINP, onCLS, onTTFB }) => {
      onLCP(report)
      onINP(report)
      onCLS(report)
      onTTFB(report)
    })
    .catch(() => undefined)
  return true
}

export async function startWebVitalsReportingAfterRouterReady(
  router: WebVitalsReadyRouter,
  options: WebVitalsAfterRouterReadyOptions = {},
): Promise<boolean> {
  await router.isReady()
  return startWebVitalsReporting({ ...options, path: router.currentRoute.value.path })
}
