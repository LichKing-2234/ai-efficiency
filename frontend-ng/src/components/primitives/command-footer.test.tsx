import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CommandFooter } from './command-footer'

describe('CommandFooter', () => {
  test('renders compact command helper text with a stable slot', () => {
    const html = renderToStaticMarkup(
      <CommandFooter>
        <CommandFooter.Hint
          keys={<><CommandFooter.Key>↑↓</CommandFooter.Key></>}
          label='Navigate'
        />
        <CommandFooter.Hint
          keys={<><CommandFooter.Key>↵</CommandFooter.Key></>}
          label='Select'
        />
      </CommandFooter>
    )

    expect(html).toContain('data-slot="command-footer"')
    expect(html).toContain('border-t')
    expect(html).toContain('text-[11px]')
    expect(html).toContain('data-slot="command-footer-hint"')
    expect(html).toContain('data-slot="command-footer-key"')
    expect(html).toContain('Navigate')
    expect(html).toContain('Select')
  })
})
