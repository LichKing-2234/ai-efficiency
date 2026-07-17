import type { Metric } from 'web-vitals'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const vitalMocks = vi.hoisted(() => ({
  callbacks: new Map<string, (metric: Metric) => void>(),
  onLCP: vi.fn((callback: (metric: Metric) => void) => vitalMocks.callbacks.set('LCP', callback)),
  onINP: vi.fn((callback: (metric: Metric) => void) => vitalMocks.callbacks.set('INP', callback)),
  onCLS: vi.fn((callback: (metric: Metric) => void) => vitalMocks.callbacks.set('CLS', callback)),
  onTTFB: vi.fn((callback: (metric: Metric) => void) => vitalMocks.callbacks.set('TTFB', callback)),
}))

vi.mock('web-vitals', () => ({
  onLCP: vitalMocks.onLCP,
  onINP: vitalMocks.onINP,
  onCLS: vitalMocks.onCLS,
  onTTFB: vitalMocks.onTTFB,
}))

import { submitWebVital } from '@/api/telemetry'
import {
  normalizeWebVitalsRoute,
  readWebVitalsSampleRate,
  startWebVitalsReporting,
  startWebVitalsReportingAfterRouterReady,
} from '@/telemetry/webVitals'

function metric(name: 'LCP' | 'INP' | 'CLS' | 'TTFB', value: number, navigationType: Metric['navigationType']): Metric {
  return {
    name,
    value,
    delta: value,
    entries: [],
    id: `private-${name}-id`,
    navigationType,
    rating: 'good',
  }
}

describe('web vitals reporting', () => {
  beforeEach(() => {
    vitalMocks.callbacks.clear()
    vitalMocks.onLCP.mockClear()
    vitalMocks.onINP.mockClear()
    vitalMocks.onCLS.mockClear()
    vitalMocks.onTTFB.mockClear()
    localStorage.clear()
  })

  it('samples once and reports four metrics against the initial normalized route', async () => {
    const submit = vi.fn().mockResolvedValue(undefined)
    const started = startWebVitalsReporting({
      token: 'test-access-token',
      path: '/repos/44?email=alice@example.com#private',
      sampleRate: 0.1,
      random: () => 0.05,
      submit,
    })

    expect(started).toBe(true)
    await vi.waitFor(() => expect(vitalMocks.onLCP).toHaveBeenCalledOnce())
    expect(vitalMocks.onINP).toHaveBeenCalledOnce()
    expect(vitalMocks.onCLS).toHaveBeenCalledOnce()
    expect(vitalMocks.onTTFB).toHaveBeenCalledOnce()

    vitalMocks.callbacks.get('LCP')!(metric('LCP', 2500, 'navigate'))
    vitalMocks.callbacks.get('INP')!(metric('INP', 180, 'reload'))
    vitalMocks.callbacks.get('CLS')!(metric('CLS', 0.08, 'back-forward-cache'))
    vitalMocks.callbacks.get('TTFB')!(metric('TTFB', 700, 'back-forward'))

    await vi.waitFor(() => expect(submit).toHaveBeenCalledTimes(4))
    expect(submit.mock.calls.map(([payload]) => payload)).toEqual([
      { metric: 'LCP', value: 2500, route: '/repos/:id', navigation_type: 'navigate' },
      { metric: 'INP', value: 180, route: '/repos/:id', navigation_type: 'reload' },
      { metric: 'CLS', value: 0.08, route: '/repos/:id', navigation_type: 'back-forward-cache' },
      { metric: 'TTFB', value: 700, route: '/repos/:id', navigation_type: 'back-forward' },
    ])
    const serialized = JSON.stringify(submit.mock.calls)
    expect(serialized).not.toContain('private-LCP-id')
    expect(serialized).not.toContain('alice@example.com')
    expect(serialized).not.toContain('44')
  })

  it('waits for the final initial route and refreshed token before reporting', async () => {
    let resolveReady!: () => void
    const ready = new Promise<void>((resolve) => {
      resolveReady = resolve
    })
    const router = {
      isReady: () => ready,
      currentRoute: { value: { path: '/' } },
    }
    const submit = vi.fn().mockResolvedValue(undefined)
    localStorage.setItem('token', 'expired-access-token')

    const started = startWebVitalsReportingAfterRouterReady(router, {
      sampleRate: 1,
      random: () => 0,
      submit,
    })
    expect(vitalMocks.onLCP).not.toHaveBeenCalled()

    router.currentRoute.value.path = '/usage'
    localStorage.setItem('token', 'refreshed-access-token')
    resolveReady()

    await expect(started).resolves.toBe(true)
    await vi.waitFor(() => expect(vitalMocks.onLCP).toHaveBeenCalledOnce())
    vitalMocks.callbacks.get('LCP')!(metric('LCP', 2500, 'navigate'))
    await vi.waitFor(() => expect(submit).toHaveBeenCalledOnce())
    expect(submit).toHaveBeenCalledWith(
      { metric: 'LCP', value: 2500, route: '/usage', navigation_type: 'navigate' },
      'refreshed-access-token',
    )
  })

  it('does not register callbacks without auth or when the page is not sampled', () => {
    expect(startWebVitalsReporting({ token: null, sampleRate: 1, random: () => 0 })).toBe(false)
    expect(startWebVitalsReporting({ token: 'token', sampleRate: 0.1, random: () => 0.1 })).toBe(false)
    expect(vitalMocks.onLCP).not.toHaveBeenCalled()
    expect(vitalMocks.onINP).not.toHaveBeenCalled()
    expect(vitalMocks.onCLS).not.toHaveBeenCalled()
    expect(vitalMocks.onTTFB).not.toHaveBeenCalled()
  })

  it('normalizes only the closed route table and clamps sample configuration', () => {
    expect(normalizeWebVitalsRoute('/usage/members/42?email=alice@example.com')).toBe('/usage/members/:user_id')
    expect(normalizeWebVitalsRoute('/private/alice@example.com')).toBe('unmatched')
    expect(readWebVitalsSampleRate(undefined)).toBe(0.1)
    expect(readWebVitalsSampleRate('0.25')).toBe(0.25)
    expect(readWebVitalsSampleRate('-1')).toBe(0)
    expect(readWebVitalsSampleRate('2')).toBe(1)
    expect(readWebVitalsSampleRate('invalid')).toBe(0.1)
  })
})

describe('web vitals transport', () => {
  it('sends an authenticated keepalive request with the exact payload', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 202 })
    const payload = { metric: 'LCP' as const, value: 2500, route: '/repos/:id', navigation_type: 'navigate' as const }

    await submitWebVital(payload, 'test-access-token', fetchImpl)

    expect(fetchImpl).toHaveBeenCalledOnce()
    const [url, options] = fetchImpl.mock.calls[0]
    expect(url).toBe('/api/v1/telemetry/web-vitals')
    expect(options).toMatchObject({
      method: 'POST',
      keepalive: true,
      headers: {
        Authorization: 'Bearer test-access-token',
        'Content-Type': 'application/json',
      },
    })
    expect(JSON.parse(options.body)).toEqual(payload)
    expect(Object.keys(JSON.parse(options.body))).toEqual(['metric', 'value', 'route', 'navigation_type'])
  })

  it('rejects missing auth and non-success responses without reading response content', async () => {
    const fetchImpl = vi.fn()
    const payload = { metric: 'CLS' as const, value: 0.1, route: '/usage', navigation_type: 'reload' as const }
    await expect(submitWebVital(payload, '', fetchImpl)).rejects.toThrow('access token')
    expect(fetchImpl).not.toHaveBeenCalled()

    fetchImpl.mockResolvedValue({ ok: false, status: 429 })
    await expect(submitWebVital(payload, 'token', fetchImpl)).rejects.toThrow('429')
  })
})
