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
})
