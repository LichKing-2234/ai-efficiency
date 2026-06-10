import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InsetPanel } from './inset-panel'

describe('InsetPanel', () => {
  test('renders muted and comfortable inset content variants', () => {
    const html = renderToStaticMarkup(
      <>
        <InsetPanel muted>Background sync pending</InsetPanel>
        <InsetPanel comfortable>Provider response</InsetPanel>
      </>
    )

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Background sync pending')
    expect(html).toContain('Provider response')
    expect(html).toContain('text-muted-foreground')
    expect(html).toContain('p-4')
    expect(html).toContain('leading-7')
  })

  test('supports a flush variant for details nested inside framed surfaces', () => {
    const html = renderToStaticMarkup(<InsetPanel flush>Expanded details</InsetPanel>)

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Expanded details')
    expect(html).toContain('rounded-none')
    expect(html).toContain('border-x-0')
    expect(html).toContain('border-t-0')
    expect(html).toContain('p-4')
  })

  test('supports a stacked content variant for form previews', () => {
    const html = renderToStaticMarkup(
      <InsetPanel stack>
        <span>Repository preview</span>
        <span>Clone URL</span>
      </InsetPanel>
    )

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Repository preview')
    expect(html).toContain('Clone URL')
    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-3')
    expect(html).not.toContain('class="flex flex-col gap-3 text-sm"')
  })
})
