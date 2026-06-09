import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TokenBreakdown } from './token-breakdown'

describe('TokenBreakdown', () => {
  test('renders a stacked token bar with legend rows and formatted values', () => {
    const html = renderToStaticMarkup(
      <TokenBreakdown
        items={[
          { label: 'Input', value: 30, color: 'var(--viz-input)' },
          { label: 'Output', value: 10, color: 'var(--viz-output)' }
        ]}
        valueFormatter={(value) => `${value} tok`}
      />
    )

    expect(html).toContain('data-slot="token-breakdown"')
    expect(html).toContain('data-slot="token-breakdown-bar"')
    expect(html).toContain('data-slot="token-breakdown-segment"')
    expect(html).toContain('data-slot="token-breakdown-row"')
    expect(html).toContain('width:75%')
    expect(html).toContain('width:25%')
    expect(html).toContain('Input')
    expect(html).toContain('30 tok')
    expect(html).toContain('Output')
    expect(html).toContain('10 tok')
  })

  test('keeps zero totals render-safe without NaN widths', () => {
    const html = renderToStaticMarkup(
      <TokenBreakdown
        items={[
          { label: 'Cache', value: 0, color: 'var(--viz-cache)' },
          { label: 'Reasoning', value: 0, color: 'var(--viz-reason)' }
        ]}
        valueFormatter={String}
      />
    )

    expect(html).toContain('width:0%')
    expect(html).not.toContain('NaN')
    expect(html).not.toContain('Infinity')
  })
})
