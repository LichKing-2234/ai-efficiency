import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TokenMeter } from './token-meter'

describe('TokenMeter', () => {
  test('renders a compact token value with a bounded visual share', () => {
    const html = renderToStaticMarkup(<TokenMeter label='4.2K' value={42} max={100} />)

    expect(html).toContain('data-slot="token-meter"')
    expect(html).toContain('data-slot="token-meter-track"')
    expect(html).toContain('data-slot="token-meter-fill"')
    expect(html).toContain('data-slot="token-meter-value"')
    expect(html).toContain('4.2K')
    expect(html).toContain('width:42%')
  })

  test('keeps non-zero values visible and zero values empty', () => {
    const nonZero = renderToStaticMarkup(<TokenMeter label='1' value={1} max={1000} />)
    const zero = renderToStaticMarkup(<TokenMeter label='0' value={0} max={1000} />)

    expect(nonZero).toContain('width:4%')
    expect(zero).toContain('width:0%')
  })

  test('uses shared action-group layout for the meter shell', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./token-meter.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/meter-track'")
    expect(source).toContain('<ActionGroup')
    expect(source).toContain('<MeterTrack')
    expect(source).toContain("dataSlot='token-meter'")
    expect(source).toContain("className={cn('gap-2', className)}")
    expect(source).toContain("className='h-1.5 max-w-[88px] flex-1'")
    expect(source).toContain("className='mono tnum min-w-[54px] text-right text-[11.5px] text-[var(--ink-2)]'")
    expect(source).not.toContain("className={cn('flex min-w-0 items-center gap-2', className)}")
    expect(source).not.toContain("className='mono tnum min-w-12 text-[var(--ink-2)] text-xs'")
  })
})
