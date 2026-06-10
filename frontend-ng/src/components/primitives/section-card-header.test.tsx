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
    expect(html).toContain('justify-between')
  })

  test('omits optional description and actions without empty controls', () => {
    const html = renderToStaticMarkup(<SectionCardHeader title='Organization Login' />)

    expect(html).toContain('Organization Login')
    expect(html).not.toContain('data-slot="card-description"')
    expect(html).not.toContain('justify-end')
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
    expect(html).toContain('text-muted-foreground')
    expect(html).toContain('text-sm')
  })
})
