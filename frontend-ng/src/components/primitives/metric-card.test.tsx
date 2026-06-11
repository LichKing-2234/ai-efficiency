import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { GaugeIcon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { KpiCard, MetricCard } from './metric-card'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'metric-card.tsx'), 'utf8')

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

  test('uses the shared card content stack for standardized KPI body rhythm', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("<CardContentStack className='p-[18px]'>")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-3 p-[18px]'>")
  })

  test('uses shared stack and action primitives for KPI header and value rows', () => {
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("from './action-group'")
    expect(source).toContain("className='min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--ink-3)]'")
    expect(source).toContain("className='gap-3 text-[11.5px] text-[var(--ink-3)]'")
    expect(source).toContain("className={cn('tnum font-semibold text-3xl leading-none tracking-tight'")
    expect(source).not.toContain("<div className='flex items-center gap-2'>")
    expect(source).not.toContain("<div className='flex items-end justify-between gap-3'>")
  })
})
