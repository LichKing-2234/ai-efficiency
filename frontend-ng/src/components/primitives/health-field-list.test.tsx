import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { HealthFieldItem, HealthFieldList } from './health-field-list'

describe('HealthFieldList', () => {
  test('renders service health rows with semantic status dots', () => {
    const html = renderToStaticMarkup(
      <HealthFieldList>
        <HealthFieldItem label='API' status='healthy' value='ready' />
        <HealthFieldItem label='Worker' status='warning' value='degraded' />
        <HealthFieldItem label='Queue' status='danger' value='blocked' />
      </HealthFieldList>
    )

    expect(html).toContain('data-slot="health-field-list"')
    expect(html).toContain('data-slot="health-field-item"')
    expect(html).toContain('data-slot="health-status-dot"')
    expect(html).toContain('data-status="healthy"')
    expect(html).toContain('data-status="warning"')
    expect(html).toContain('data-status="danger"')
    expect(html).toContain('API')
    expect(html).toContain('ready')
  })

  test('uses shared action-group and stack primitives for health rows', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./health-field-list.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("<ActionGroup align='start'")
    expect(source).toContain("<Stack")
    expect(source).toContain("className='border-b border-[var(--line-faint)] px-[14px] py-[11px] last:border-b-0'")
    expect(source).toContain("className='w-32 shrink-0 text-[13px] font-medium text-[var(--ink-2)]'")
    expect(source).toContain("className={cn('size-2 shrink-0 rounded-full ring-[3px]'")
    expect(source).toContain("danger: 'bg-destructive ring-[color:var(--neg-soft)]'")
    expect(source).toContain("healthy: 'bg-[var(--pos)] ring-[color:var(--pos-soft)]'")
    expect(source).toContain("unknown: 'bg-muted-foreground/45 ring-transparent'")
    expect(source).toContain("warning: 'bg-[var(--warn)] ring-[color:var(--warn-soft)]'")
    expect(source).toContain("cn('min-w-0 flex-1 text-right text-[12.5px] text-[var(--ink-3)]', mono && 'mono break-all text-[12px] text-[var(--ink-2)]', truncate && 'truncate')")
    expect(source).toContain("dataSlot='health-field-value'")
    expect(source).not.toContain('ring-3 ring-transparent')
    expect(source).not.toContain("cn('min-w-0 flex-1 text-right text-sm', mono && 'mono break-all text-xs', truncate && 'truncate')")
    expect(source).not.toContain("className='flex items-center gap-3 border-b border-[var(--line-faint)] px-3 py-2 last:border-b-0'")
    expect(source).not.toContain("className='flex w-28 shrink-0 items-center gap-2 text-muted-foreground text-xs'")
  })
})
