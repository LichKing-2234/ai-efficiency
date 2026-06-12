import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { InfoTile } from '@/components/primitives/info-tile'
import { DetailSummaryStack } from './detail-summary-stack'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'detail-summary-stack.tsx'), 'utf8')

describe('DetailSummaryStack', () => {
  test('renders shared slide-over summary structure for statuses and metrics', () => {
    const html = renderToStaticMarkup(
      <DetailSummaryStack
        statuses={<span>bound</span>}
        metrics={<InfoTile label='Tokens' value='1.2M' />}
      >
        <div>Details</div>
      </DetailSummaryStack>
    )

    expect(html).toContain('data-slot="slide-over-stack"')
    expect(html).toContain('data-slot="status-cluster"')
    expect(html).toContain('data-slot="info-tile-grid"')
    expect(html).toContain('bound')
    expect(html).toContain('Tokens')
    expect(html).toContain('1.2M')
    expect(html).toContain('Details')
  })

  test('composes shared slide-over, status, and info-tile primitives', () => {
    expect(source).toContain("from '@/components/primitives/info-tile'")
    expect(source).toContain("from '@/components/primitives/slide-over-stack'")
    expect(source).toContain("from '@/components/primitives/status-cluster'")
    expect(source).toContain('<SlideOverStack>')
    expect(source).toContain('{statuses ? <StatusCluster>{statuses}</StatusCluster> : null}')
    expect(source).toContain('<InfoTileGrid columns={3}>')
  })
})
