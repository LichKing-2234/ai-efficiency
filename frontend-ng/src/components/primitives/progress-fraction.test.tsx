import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ProgressFraction } from './progress-fraction'

describe('ProgressFraction', () => {
  test('renders ready and total counts with shared setup progress typography', () => {
    const html = renderToStaticMarkup(<ProgressFraction ready={2} total={4} />)

    expect(html).toContain('data-slot="progress-fraction"')
    expect(html).toContain('data-slot="progress-fraction-total"')
    expect(html).toContain('2')
    expect(html).toContain('/4')
    expect(html).toContain('text-[11px]')
    expect(html).toContain('text-[var(--ink-3)]')
  })
})
