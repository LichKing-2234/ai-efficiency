import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InfoTile } from '@/components/primitives/info-tile'
import { SummaryMetricsPanel } from './summary-metrics-panel'

describe('SummaryMetricsPanel', () => {
  test('renders shared 4-column summary metrics with an optional note', () => {
    const html = renderToStaticMarkup(
      <SummaryMetricsPanel note='Usage refresh 4/7' title='Latest sync job'>
        <InfoTile label='Status' value='Running' />
        <InfoTile label='Phase' value='usage' />
        <InfoTile label='Fetched' value='12' />
        <InfoTile label='Processed' value='8/12' />
      </SummaryMetricsPanel>
    )

    expect(html).toContain('Latest sync job')
    expect(html).toContain('data-slot="info-tile-grid"')
    expect(html).toContain('min-[920px]:grid-cols-4')
    expect(html).toContain('Usage refresh 4/7')
  })
})
