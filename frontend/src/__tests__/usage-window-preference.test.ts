import { describe, expect, it, vi } from 'vitest'
import { readUsageWindowPreference, writeUsageWindowPreference } from '@/utils/usageWindowPreference'

describe('Usage Window Preference', () => {
  it.each(['today', '7d', '30d'] as const)('restores the saved %s window', (window) => {
    localStorage.setItem('ae.usage.window', window)

    expect(readUsageWindowPreference(localStorage)).toBe(window)
  })

  it.each([null, 'custom', 'TODAY'])('falls back without rewriting an unsupported value %s', (value) => {
    const storage = {
      getItem() { return value },
      setItem: vi.fn(),
    }

    expect(readUsageWindowPreference(storage)).toBe('30d')
    expect(storage.setItem).not.toHaveBeenCalled()
  })

  it('falls back to 30 days when browser storage cannot be read', () => {
    const storage = {
      getItem() {
        throw new DOMException('storage denied', 'SecurityError')
      },
      setItem() {},
    }

    expect(readUsageWindowPreference(storage)).toBe('30d')
  })

  it('stores an explicitly selected window', () => {
    writeUsageWindowPreference('7d', localStorage)

    expect(readUsageWindowPreference(localStorage)).toBe('7d')
  })

  it('ignores a browser storage write failure', () => {
    const storage = {
      getItem() { return null },
      setItem() {
        throw new DOMException('storage denied', 'QuotaExceededError')
      },
    }

    expect(() => writeUsageWindowPreference('today', storage)).not.toThrow()
  })

  it('falls back when browser storage itself is inaccessible', () => {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')!
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new DOMException('storage denied', 'SecurityError')
      },
    })

    try {
      expect(readUsageWindowPreference()).toBe('30d')
      expect(() => writeUsageWindowPreference('7d')).not.toThrow()
    } finally {
      Object.defineProperty(globalThis, 'localStorage', descriptor)
    }
  })
})
