import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { UsageActivityRow } from './usage-activity-row'

describe('UsageActivityRow', () => {
  test('renders compact usage metadata with optional first-row spacing', () => {
    const html = renderToStaticMarkup(
      <UsageActivityRow
        bound
        credit='12'
        endedAt='2026-06-09 10:30'
        first
        requests='3 req'
        statusLabel='Bound'
        title='org/repo'
        tokens='4.2K tok'
        tool='codex'
      />
    )

    expect(html).toContain('data-slot="usage-activity-row"')
    expect(html).toContain('data-slot="usage-activity-content"')
    expect(html).toContain('data-slot="usage-activity-title"')
    expect(html).toContain('data-slot="usage-activity-meta"')
    expect(html).toContain('data-slot="usage-activity-amount"')
    expect(html).toContain('data-state="bound"')
    expect(html).toContain('org/repo')
    expect(html).toContain('2026-06-09 10:30')
    expect(html).toContain('4.2K tok')
    expect(html).toContain('Bound')
    expect(html).toContain('12')
    expect(html).toContain('3 req')
    expect(html).not.toContain('border-t')
  })

  test('renders unbound rows with a separator after the first item', () => {
    const html = renderToStaticMarkup(
      <UsageActivityRow
        bound={false}
        credit='0'
        endedAt='2026-06-09 10:31'
        requests='1 req'
        statusLabel='Needs binding'
        title='session.jsonl'
        tokens='900 tok'
        tool='claude'
      />
    )

    expect(html).toContain('data-state="unbound"')
    expect(html).toContain('Needs binding')
    expect(html).toContain('border-t')
  })

  test('keeps title, metadata, and amount rhythm inside semantic slots', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./usage-activity-row.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/status-badge'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain('<ActionGroup')
    expect(source).toContain("<StatusBadge label={statusLabel} value={bound ? 'bound' : 'unbound'} />")
    expect(source).toContain("dataSlot='usage-activity-row'")
    expect(source).toContain("className={cn('flex items-center gap-3 px-1 py-[11px]', !first && 'border-t border-[var(--line-faint)]', className)}")
    expect(source).toContain("className='block truncate text-[13px] font-[550]'")
    expect(source).toContain("className='hidden w-[88px] text-right tnum sm:block'")
    expect(source).toContain("className='mt-0.5 gap-x-2 gap-y-1 text-[11.5px] text-[var(--ink-3)]'")
    expect(source).toContain("className='text-[11px] text-[var(--ink-3)]'")
    expect(source).not.toContain("const usageActivityContentClass = 'min-w-0 flex-1'")
    expect(source).not.toContain("const usageActivityTitleClass = 'truncate font-semibold text-sm'")
    expect(source).not.toContain("className='mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-muted-foreground'")
    expect(source).not.toContain("const usageActivityAmountClass = 'hidden w-20 text-right tnum sm:block'")
    expect(source).not.toContain("from '@/components/ui/badge'")
    expect(source).not.toContain("<div\n      className={cn('flex items-center gap-3 py-3', !first && 'border-t border-[var(--line-faint)]', className)}")
  })
})
