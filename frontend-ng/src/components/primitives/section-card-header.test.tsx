import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SectionCardHeader } from './section-card-header'

function TerminalIcon(props: React.SVGProps<SVGSVGElement>) {
  return <svg data-testid='terminal-icon' {...props} />
}

describe('SectionCardHeader', () => {
  test('renders a card section title with description and actions', () => {
    const html = renderToStaticMarkup(
      <SectionCardHeader
        title='AI Services'
        description='Configure model providers.'
        actions={<button type='button'>Add</button>}
      />
    )

    expect(html).toContain('data-slot="card-header"')
    expect(html).toContain('data-slot="card-title"')
    expect(html).toContain('AI Services')
    expect(html).toContain('Configure model providers.')
    expect(html).toContain('Add')
    expect(html).toContain('data-slot="section-card-header-content"')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('min-[920px]:justify-end')
  })

  test('omits optional description and actions without empty controls', () => {
    const html = renderToStaticMarkup(<SectionCardHeader title='Organization Login' />)

    expect(html).toContain('Organization Login')
    expect(html).not.toContain('data-slot="card-description"')
    expect(html).toContain('justify-start')
  })

  test('passes layout class names through to the card header slot', () => {
    const html = renderToStaticMarkup(<SectionCardHeader title='Selected scope' className='gap-4' />)

    expect(html).toContain('data-slot="card-header"')
    expect(html).toContain('gap-4')
  })

  test('renders standardized leading icon titles', () => {
    const html = renderToStaticMarkup(<SectionCardHeader leading={TerminalIcon} title='CLI workflow' />)

    expect(html).toContain('data-slot="section-card-title-row"')
    expect(html).toContain('data-slot="section-card-leading-icon"')
    expect(html).toContain('CLI workflow')
    expect(html).not.toContain("class=\"flex items-center gap-2\"")
  })

  test('renders standardized live title indicator', () => {
    const html = renderToStaticMarkup(<SectionCardHeader live title='Recent usage' />)

    expect(html).toContain('data-slot="section-card-live-indicator"')
    expect(html).toContain('live-dot')
    expect(html).toContain('Recent usage')
  })

  test('renders standardized muted metadata without page-local text classes', () => {
    const html = renderToStaticMarkup(<SectionCardHeader title='Selected scope' meta='42 repositories' />)

    expect(html).toContain('data-slot="section-card-meta"')
    expect(html).toContain('42 repositories')
    expect(html).toContain('text-[12px]')
    expect(html).toContain('text-[var(--ink-3)]')
  })

  test('sources title and action layout from shared stack and action group primitives', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./section-card-header.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("dataSlot='section-card-header-content'")
    expect(source).toContain("dataSlot='section-card-title-row'")
    expect(source).toContain("<ActionGroup align='start' className='min-w-0 gap-[9px]'")
    expect(source).toContain("className='w-full gap-3'")
    expect(source).toContain("className='text-[14px] font-[650] leading-none'")
    expect(source).toContain("className='mt-0.5 text-[12px] text-[var(--ink-3)]'")
    expect(source).toContain("className='shrink-0 gap-2.5'")
    expect(source).not.toContain("className={cn('flex items-start justify-between gap-3', actions ? 'flex-col sm:flex-row sm:items-center' : 'items-center')}")
    expect(source).not.toContain("className='flex shrink-0 items-center justify-end gap-2'")
    expect(source).not.toContain("className='inline-flex min-w-0 items-center gap-2'")
    expect(source).not.toContain("className={actions ? 'sm:flex-row sm:items-center sm:justify-between' : undefined}")
  })
})
