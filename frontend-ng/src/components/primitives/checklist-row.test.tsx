import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ChecklistRow } from './checklist-row'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'checklist-row.tsx'), 'utf8')

describe('ChecklistRow', () => {
  test('renders ready and pending checklist states with optional action', () => {
    const html = renderToStaticMarkup(
      <>
        <ChecklistRow label='Account' ok value='Ready' />
        <ChecklistRow action={<a href='/user'>Fix</a>} label='AI access' ok={false} value='Needs setup' />
      </>
    )

    expect(html).toContain('data-slot="checklist-row"')
    expect(html).toContain('Account')
    expect(html).toContain('Ready')
    expect(html).toContain('AI access')
    expect(html).toContain('Needs setup')
    expect(html).toContain('Fix')
    expect(html).toContain('data-state="ready"')
    expect(html).toContain('data-state="pending"')
  })

  test('renders plain status values without forcing badge chrome', () => {
    const html = renderToStaticMarkup(<ChecklistRow label='Repository reporting' ok value='Configured' />)

    expect(html).toContain('Configured')
    expect(html).toContain('data-slot="checklist-row-value"')
    expect(html).not.toContain('data-slot="badge"')
  })

  test('uses shared row primitives for checklist shell and trailing actions', () => {
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("from './action-group'")
    expect(source).toContain("<FilterRow")
    expect(source).toContain("<ActionGroup align='start'")
    expect(source).toContain("rounded-[var(--r-sm)] border border-[var(--line-faint)] bg-[var(--surface-inset)] px-[11px] py-[9px] text-[12px]")
    expect(source).toContain("className='min-w-0 text-[var(--ink-3)]'")
    expect(source).toContain("className='truncate text-[12.5px] font-medium'")
    expect(source).toContain("'shrink-0 text-[12px] font-medium'")
    expect(source).not.toContain("from '@/components/ui/badge'")
    expect(source).not.toContain("className='flex min-w-0 items-center gap-2 text-muted-foreground'")
    expect(source).not.toContain("className='flex shrink-0 items-center gap-2'")
  })
})
