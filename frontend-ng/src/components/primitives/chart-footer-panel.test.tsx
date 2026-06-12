import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ChartFooterPanel } from './chart-footer-panel'

describe('ChartFooterPanel', () => {
  test('renders the shared bordered chart footer shell with stable slots', () => {
    const html = renderToStaticMarkup(
      <ChartFooterPanel label='Token consumption'>
        <div>Chart body</div>
      </ChartFooterPanel>
    )

    expect(html).toContain('data-slot="chart-footer-panel"')
    expect(html).toContain('data-slot="chart-footer-panel-label"')
    expect(html).toContain('Token consumption')
    expect(html).toContain('Chart body')
  })
})
