import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { OptionList } from './option-list'

describe('OptionList', () => {
  test('renders selectable option rows with primary and secondary labels', () => {
    const html = renderToStaticMarkup(
      <OptionList
        ariaLabel='User search results'
        items={[
          { id: '1', label: 'alice@example.com', description: 'admin · 12' },
          { id: '2', label: 'bob@example.org' }
        ]}
        onSelect={() => undefined}
      />
    )

    expect(html).toContain('data-slot="option-list"')
    expect(html).toContain('aria-label="User search results"')
    expect(html).toContain('alice@example.com')
    expect(html).toContain('admin · 12')
    expect(html).toContain('bob@example.org')
  })

  test('keeps description rhythm inside the primitive description slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./option-list.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("className='block truncate text-[11px] text-[var(--ink-4)]'")
    expect(source).toContain("className={cn('rounded-[var(--r-md)] border border-border bg-[var(--surface)] p-[10px]', className)}")
    expect(source).toContain("className='min-w-0 rounded-[var(--r-sm)] px-[10px] py-[8px] text-left text-[12.5px] transition hover:bg-[var(--surface-inset)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'")
    expect(source).toContain("className='block truncate font-medium text-[12.5px]'")
    expect(source).not.toContain("className='mt-0.5 block truncate text-muted-foreground text-xs'")
    expect(source).not.toContain("className={cn('rounded-[var(--r-md)] border border-border bg-card p-[10px]', className)}")
    expect(source).not.toContain("className={cn('flex flex-col gap-1 rounded-[var(--r-md)] border border-border bg-card p-2 shadow-[var(--sh-sm)]', className)}")
  })
})
