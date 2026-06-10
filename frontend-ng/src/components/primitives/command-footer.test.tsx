import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CommandFooter } from './command-footer'

describe('CommandFooter', () => {
  test('renders compact command helper text with a stable slot', () => {
    const html = renderToStaticMarkup(<CommandFooter>Navigate and select</CommandFooter>)

    expect(html).toContain('data-slot="command-footer"')
    expect(html).toContain('border-t')
    expect(html).toContain('text-[11px]')
    expect(html).toContain('Navigate and select')
  })
})
