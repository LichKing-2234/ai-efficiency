import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { RatioMeter } from './ratio-meter'

describe('RatioMeter', () => {
  test('renders a compact part over total ratio with bounded visual share', () => {
    const html = renderToStaticMarkup(<RatioMeter part={3} total={12} />)

    expect(html).toContain('data-slot="ratio-meter"')
    expect(html).toContain('data-slot="ratio-meter-track"')
    expect(html).toContain('data-slot="ratio-meter-fill"')
    expect(html).toContain('data-slot="ratio-meter-value"')
    expect(html).toContain('3/12')
    expect(html).toContain('width:25%')
  })

  test('renders an empty dash when the total is zero', () => {
    const html = renderToStaticMarkup(<RatioMeter part={0} total={0} emptyLabel='-' />)

    expect(html).toContain('data-empty="true"')
    expect(html).toContain('>-<')
    expect(html).toContain('width:0%')
  })

  test('uses shared action-group layout for the meter shell', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./ratio-meter.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/meter-track'")
    expect(source).toContain('<ActionGroup')
    expect(source).toContain('<MeterTrack')
    expect(source).toContain("dataSlot='ratio-meter'")
    expect(source).toContain("className='h-1.5 max-w-[88px] flex-1'")
    expect(source).toContain("className='mono tnum min-w-[54px] text-[11.5px] text-[var(--ink-2)]'")
    expect(source).not.toContain("className={cn('flex min-w-0 items-center gap-2', className)}")
    expect(source).not.toContain("className='h-1.5 max-w-20 flex-1 overflow-hidden rounded-full bg-[var(--surface-inset)]'")
  })
})
