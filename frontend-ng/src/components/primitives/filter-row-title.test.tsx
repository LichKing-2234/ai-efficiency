import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FilterRowTitle } from './filter-row-title'

describe('FilterRowTitle', () => {
  test('renders compact toolbar title and description slots', () => {
    const html = renderToStaticMarkup(<FilterRowTitle title='Usage Analytics' description='Track token usage.' />)

    expect(html).toContain('data-slot="filter-row-title"')
    expect(html).toContain('data-slot="filter-row-title-text"')
    expect(html).toContain('data-slot="filter-row-title-description"')
    expect(html).toContain('Usage Analytics')
    expect(html).toContain('Track token usage.')
    expect(html).toContain('text-[12px] text-[var(--ink-3)]')
  })

  test('omits description without rendering an empty slot', () => {
    const html = renderToStaticMarkup(<FilterRowTitle title='Usage Analytics' />)

    expect(html).toContain('Usage Analytics')
    expect(html).not.toContain('data-slot="filter-row-title-description"')
  })

  test('renders compact muted labels for filter rows', () => {
    const html = renderToStaticMarkup(<FilterRowTitle variant='label' title='Merged in' />)

    expect(html).toContain('data-slot="filter-row-title"')
    expect(html).toContain('data-slot="filter-row-title-label"')
    expect(html).toContain('Merged in')
    expect(html).toContain('text-[12px] text-[var(--ink-3)]')
    expect(html).not.toContain('data-slot="filter-row-title-text"')
  })

  test('keeps description rhythm inside the primitive description slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./filter-row-title.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("className='text-[12px] text-[var(--ink-3)]'")
    expect(source).toContain("className='text-[12px] text-[var(--ink-3)]' data-slot='filter-row-title-label'")
    expect(source).toContain("className='font-semibold text-[14px]' data-slot='filter-row-title-text'")
    expect(source).not.toContain("className='mt-0.5 text-muted-foreground text-xs'")
    expect(source).not.toContain("<div className={cn('min-w-0', className)} data-slot='filter-row-title'>")
  })
})
