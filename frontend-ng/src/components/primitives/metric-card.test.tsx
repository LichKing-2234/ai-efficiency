import { renderToStaticMarkup } from 'react-dom/server'
import { GaugeIcon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { KpiCard, MetricCard } from './metric-card'

describe('KpiCard', () => {
  test('is the canonical KPI primitive and keeps MetricCard as a compatibility alias', () => {
    expect(MetricCard).toBe(KpiCard)

    const html = renderToStaticMarkup(
      <KpiCard
        accent
        delta={12}
        icon={GaugeIcon}
        label='AI PR share'
        sparkline={[1, 3, 2, 5]}
        value='42%'
      />
    )

    expect(html).toContain('AI PR share')
    expect(html).toContain('42%')
    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('text-[var(--ai-deep)]')
  })
})
