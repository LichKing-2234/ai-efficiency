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
})
