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
    expect(html).toContain('data-slot="muted-inset-note"')
  })

  test('supports compact section-card options and alternate metric layouts', () => {
    const html = renderToStaticMarkup(
      <SummaryMetricsPanel
        actions={<button type='button'>Refresh</button>}
        description='Current build and update channel.'
        gap='compact'
        metricsClassName='split-equal min-[920px]:grid-cols-3'
        metricsColumns={3}
        title='Version'
      >
        <InfoTile label='Current' value='v2.8.0' />
        <InfoTile label='Latest' value='v2.8.0' />
        <InfoTile label='Mode' value='prod' />
      </SummaryMetricsPanel>
    )

    expect(html).toContain('Refresh')
    expect(html).toContain('Current build and update channel.')
    expect(html).toContain('split-equal min-[920px]:grid-cols-3')
    expect(html).toContain('flex flex-col gap-2')
  })
})
