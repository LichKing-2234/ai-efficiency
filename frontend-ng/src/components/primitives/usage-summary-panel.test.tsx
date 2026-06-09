import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { UsageSummaryPanel } from './usage-summary-panel'

describe('UsageSummaryPanel', () => {
  test('renders metric tiles and status actions in a shared inset surface', () => {
    const html = renderToStaticMarkup(
      <UsageSummaryPanel
        actions={<button type='button'>Refresh</button>}
        metrics={[
          { label: 'Input', value: '12K', numeric: true },
          { label: 'Output', value: '4K', numeric: true },
          { label: 'Credits', value: '8', accent: 'ai' }
        ]}
        status={<span>ready</span>}
        summary='total 16K tokens'
      />
    )

    expect(html).toContain('data-slot="usage-summary-panel"')
    expect(html).toContain('Input')
    expect(html).toContain('12K')
    expect(html).toContain('ready')
    expect(html).toContain('total 16K tokens')
    expect(html).toContain('Refresh')
  })
})
