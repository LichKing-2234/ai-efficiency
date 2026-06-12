import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { PageHeader, PageToolbar } from './page'

describe('PageHeader', () => {
  test('renders title variant as the page heading surface', () => {
    const html = renderToStaticMarkup(
      <PageHeader title='Usage Analytics' description='Track AI usage.' actions={<button type='button'>Refresh</button>} />
    )

    expect(html).toContain('<h1')
    expect(html).toContain('Usage Analytics')
    expect(html).toContain('Track AI usage.')
    expect(html).toContain('Refresh')
  })

  test('renders toolbar variant as an action row without duplicate page copy', () => {
    const html = renderToStaticMarkup(
      <PageHeader title='Settings' description='Configure providers.' actions={<button type='button'>Add</button>} variant='toolbar' />
    )

    expect(html).not.toContain('<h1')
    expect(html).not.toContain('Settings')
    expect(html).not.toContain('Configure providers.')
    expect(html).toContain('Add')
    expect(html).toContain('justify-end')
  })

  test('omits empty toolbar headers when no actions are provided', () => {
    const html = renderToStaticMarkup(
      <PageHeader title='My Setup' description='Configure local credentials.' variant='toolbar' />
    )

    expect(html).toBe('')
  })

  test('renders PageToolbar as a right-aligned action-only primitive', () => {
    const html = renderToStaticMarkup(
      <PageToolbar>
        <button type='button'>Sync PRs</button>
      </PageToolbar>
    )

    expect(html).toContain('Sync PRs')
    expect(html).toContain('justify-end')
    expect(html).not.toContain('<h1')
  })

  test('keeps title description rhythm inside the page header description slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./page.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("dataSlot='page-header'")
    expect(source).toContain("<ActionGroup align='responsive-end'")
    expect(source).toContain("className='min-[920px]:items-end'")
    expect(source).toContain("const pageHeaderDescriptionClass = 'mt-1 max-w-3xl text-[12px] text-[var(--ink-3)]'")
    expect(source).not.toContain("className='mt-1 max-w-3xl text-muted-foreground text-sm'")
    expect(source).not.toContain("className='flex items-center justify-end gap-2'")
    expect(source).not.toContain("className='md:flex-row md:items-end md:justify-between'")
  })
})
