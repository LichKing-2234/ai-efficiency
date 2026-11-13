import { describe, expect, it } from 'vitest'
import { formatCompactNumber, formatCredits, formatTokenCount, formatUsagePair } from '@/utils/formatters'

describe('formatters', () => {
  it('formats token counts with compact K M B suffixes', () => {
    expect(formatTokenCount(null)).toBe('-')
    expect(formatTokenCount(undefined)).toBe('-')
    expect(formatTokenCount(900)).toBe('900')
    expect(formatTokenCount(12_900)).toBe('12.9K')
    expect(formatTokenCount(12_285_557_755)).toBe('12.29B')
    expect(formatTokenCount(6_052_813_773)).toBe('6.05B')
    expect(formatTokenCount(805_033_680)).toBe('805.03M')
  })

  it('trims insignificant decimals from compact numbers', () => {
    expect(formatCompactNumber(1_000)).toBe('1K')
    expect(formatCompactNumber(1_500)).toBe('1.5K')
    expect(formatCompactNumber(2_000_000)).toBe('2M')
    expect(formatCompactNumber(2_010_000)).toBe('2.01M')
  })

  it('formats credits and the Token/Credits pair', () => {
    expect(formatCredits(null)).toBe('--')
    expect(formatCredits(0.0556)).toBe('0.06')
    expect(formatCredits(1234.5)).toBe('1,234.50')
    expect(formatUsagePair(3_490_000_000, 0.0556)).toBe('3.49B/0.06')
    expect(formatUsagePair(null, 0.06)).toBe('--/0.06')
    expect(formatUsagePair(12_900, null)).toBe('12.9K/--')
  })
})
