import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ChartLegend } from './chart-legend'

describe('ChartLegend', () => {
  test('renders inline swatch legend items with stable slots', () => {
    const html = renderToStaticMarkup(
      <ChartLegend
        items={[
          { label: 'Input', color: 'var(--viz-input)' },
          { label: 'Output', color: 'var(--viz-output)' }
        ]}
      />
    )

    expect(html).toContain('data-slot="chart-legend"')
    expect(html).toContain('data-slot="chart-legend-item"')
    expect(html).toContain('data-slot="chart-legend-swatch"')
    expect(html).toContain('background:var(--viz-input)')
    expect(html).toContain('Input')
    expect(html).toContain('Output')
  })

  test('supports compact wrapping and custom class names', () => {
    const html = renderToStaticMarkup(
      <ChartLegend
        className='justify-end'
        compact
        items={[{ label: 'Cache Read', color: 'var(--viz-reason)' }]}
      />
    )

    expect(html).toContain('justify-end')
    expect(html).toContain('gap-3')
    expect(html).toContain('Cache Read')
  })
})
