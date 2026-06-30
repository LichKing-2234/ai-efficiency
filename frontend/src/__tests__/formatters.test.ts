import { describe, expect, it } from 'vitest'
import { formatCompactNumber, formatTokenCount } from '@/utils/formatters'

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
})
