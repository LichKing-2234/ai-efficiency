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
    expect(html).toContain('data-slot="usage-summary-panel-footer"')
    expect(html).toContain('data-slot="usage-summary-panel-meta"')
    expect(html).toContain('data-slot="usage-summary-panel-actions"')
    expect(html).toContain('Input')
    expect(html).toContain('12K')
    expect(html).toContain('ready')
    expect(html).toContain('total 16K tokens')
    expect(html).toContain('Refresh')
  })

  test('keeps footer rhythm inside semantic slots instead of raw layout classes', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./usage-summary-panel.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("<FilterRow className='mt-[12px] border-t border-[var(--line)] pt-[12px] text-[12px]' dataSlot='usage-summary-panel-footer' justify='between' gap='lg'>")
    expect(source).toContain("{summary ? <span className='text-[11.5px] text-[var(--ink-3)]'>{summary}</span> : null}")
    expect(source).toContain("<InsetPanel className={cn('bg-[var(--surface)] p-[14px]', className)} dataSlot='usage-summary-panel'>")
    expect(source).not.toContain("className='mt-4 flex flex-wrap items-center justify-between gap-3 text-sm'")
    expect(source).not.toContain("className='flex min-w-0 flex-wrap items-center gap-2'")
    expect(source).not.toContain("className='flex flex-wrap gap-2'")
  })

  test('uses shared info tile grid for summary metrics instead of a local grid wrapper', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./usage-summary-panel.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/info-tile'")
    expect(source).toContain('InfoTileGrid')
    expect(source).not.toContain("<div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-6'>")
  })
})
