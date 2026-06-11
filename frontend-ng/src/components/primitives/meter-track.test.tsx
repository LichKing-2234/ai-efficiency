import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { MeterTrack } from './meter-track'

describe('MeterTrack', () => {
  test('renders the shared compact meter track shell with a stable slot', () => {
    const html = renderToStaticMarkup(<MeterTrack>fill</MeterTrack>)

    expect(html).toContain('data-slot="meter-track"')
    expect(html).toContain('max-w-[88px]')
    expect(html).toContain('bg-[var(--surface-inset)]')
    expect(html).toContain('fill')
  })

  test('sources the meter shell from a shared primitive instead of local wrappers', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./meter-track.tsx', import.meta.url), 'utf8')
    )

    expect(source).not.toContain("className='h-1.5 max-w-20 flex-1 overflow-hidden rounded-full bg-[var(--surface-inset)]'")
    expect(source).toContain("dataSlot = 'meter-track'")
  })
})
