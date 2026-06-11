import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { CardPagerFooter } from './card-pager-footer'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'card-pager-footer.tsx'), 'utf8')

describe('CardPagerFooter', () => {
  test('renders pagination summary and previous next actions in a card footer', () => {
    const html = renderToStaticMarkup(
      <CardPagerFooter
        summary='Page 1 of 4'
        previous={<button type='button'>Previous</button>}
        next={<button type='button'>Next</button>}
      />
    )

    expect(html).toContain('data-slot="card-footer"')
    expect(html).toContain('data-slot="card-pager-footer"')
    expect(html).toContain('Page 1 of 4')
    expect(html).toContain('Previous')
    expect(html).toContain('Next')
    expect(html).toContain('sm:justify-end')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('border-border')
    expect(html).toContain('border-t')
    expect(html).toContain('p-[18px]')
  })

  test('passes layout class names through to the card footer slot', () => {
    const html = renderToStaticMarkup(
      <CardPagerFooter
        className='border-t p-3'
        summary='Page 2 of 5'
        previous={<button type='button'>Previous</button>}
        next={<button type='button'>Next</button>}
      />
    )

    expect(html).toContain('data-slot="card-footer"')
    expect(html).toContain('border-t')
    expect(html).toContain('p-3')
  })

  test('renders page metadata through a shared muted slot', () => {
    const html = renderToStaticMarkup(
      <CardPagerFooter
        meta='Page 1 of 4'
        summary='20 records'
        previous={<button type='button'>Previous</button>}
        next={<button type='button'>Next</button>}
      />
    )

    expect(html).toContain('data-slot="card-pager-footer-meta"')
    expect(html).toContain('Page 1 of 4')
    expect(html).toContain('text-[11.5px]')
    expect(html).toContain('text-[var(--ink-3)]')
  })

  test('uses shared action grouping for pager controls', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).not.toContain("<div className='flex items-center gap-2'>")
  })

  test('uses shared action-group shell for the footer content row', () => {
    expect(source).toContain("<ActionGroup align='responsive-end'")
    expect(source).toContain("dataSlot='card-pager-footer'")
    expect(source).toContain("className='w-full items-center text-[12px]'")
    expect(source).toContain("className='text-[12px] text-[var(--ink-3)]'")
    expect(source).toContain("className='text-[11.5px] text-[var(--ink-3)]'")
    expect(source).not.toContain("className={cn('flex-wrap justify-between gap-3 text-sm', className)}")
  })
})
