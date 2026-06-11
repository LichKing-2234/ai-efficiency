import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ChartLegend } from './chart-legend'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'chart-legend.tsx'), 'utf8')

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

  test('uses shared row primitives for legend shell and items', () => {
    expect(source).toContain("from './filter-row'")
    expect(source).toContain("from './action-group'")
    expect(source).not.toContain("<div className={cn('flex flex-wrap gap-4', compact && 'gap-3', className)} data-slot='chart-legend'>")
    expect(source).not.toContain("className='flex items-center gap-1.5 text-[12px] text-[var(--ink-2)]'")
  })
})
