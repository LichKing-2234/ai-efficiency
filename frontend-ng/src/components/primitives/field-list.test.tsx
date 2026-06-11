import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FieldItem, FieldList } from './field-list'

describe('FieldList', () => {
  test('renders compact label value rows with mono and truncation variants', () => {
    const html = renderToStaticMarkup(
      <FieldList>
        <FieldItem label='Commit' mono value='abcdef1234567890' />
        <FieldItem label='Source' truncate value='src/index.ts' />
      </FieldList>
    )

    expect(html).toContain('data-slot="field-list"')
    expect(html).toContain('data-slot="field-item"')
    expect(html).toContain('data-slot="field-item-label"')
    expect(html).toContain('data-slot="field-item-value"')
    expect(html).toContain('Commit')
    expect(html).toContain('abcdef1234567890')
    expect(html).toContain('mono')
    expect(html).toContain('break-all')
    expect(html).toContain('truncate')
  })

  test('keeps field item label and value rhythm inside semantic slots', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./field-list.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align='start'")
    expect(source).toContain("className='w-24 shrink-0 text-[12px] text-[var(--ink-3)]'")
    expect(source).toContain("className={cn('min-w-0 flex-1 text-right text-[12.5px] font-medium', mono && 'mono break-all text-[11.5px]', truncate && 'truncate')}")
    expect(source).toContain("className={cn('border-b border-[var(--line-faint)] px-[12px] py-[9px] last:border-b-0', className)}")
    expect(source).not.toContain("const fieldItemLabelClass = 'w-24 shrink-0 text-muted-foreground text-xs'")
    expect(source).not.toContain("const fieldItemValueClass = 'min-w-0 flex-1 text-right text-sm'")
  })
})
