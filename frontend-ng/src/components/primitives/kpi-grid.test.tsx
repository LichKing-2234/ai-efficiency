import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { KpiGrid } from './kpi-grid'

describe('KpiGrid', () => {
  test('renders the shared reference KPI layout utility', () => {
    const html = renderToStaticMarkup(
      <KpiGrid>
        <div>Repositories</div>
        <div>Usage</div>
      </KpiGrid>
    )

    expect(html).toContain('data-slot="kpi-grid"')
    expect(html).toContain('kpi-grid')
    expect(html).toContain('Repositories')
    expect(html).toContain('Usage')
  })

  test('merges layout-only classes without replacing the design utility', () => {
    const html = renderToStaticMarkup(
      <KpiGrid className='stagger'>
        <div>Events</div>
      </KpiGrid>
    )

    expect(html).toContain('kpi-grid')
    expect(html).toContain('stagger')
  })
})
