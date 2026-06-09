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
})
