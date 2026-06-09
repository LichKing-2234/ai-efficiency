import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CommandAccordion } from './command-accordion'

describe('CommandAccordion', () => {
  test('renders a shared collapsible command section with command content', () => {
    const html = renderToStaticMarkup(
      <CommandAccordion title='Windows installer'>
        <span data-test='command-row'>$ powershell install.ps1</span>
      </CommandAccordion>
    )

    expect(html).toContain('data-slot="command-accordion"')
    expect(html).toContain('Windows installer')
  })

  test('can render default-open command content for server-rendered checks', () => {
    const html = renderToStaticMarkup(
      <CommandAccordion defaultOpen title='Windows installer'>
        <span data-test='command-row'>$ powershell install.ps1</span>
      </CommandAccordion>
    )

    expect(html).toContain('$ powershell install.ps1')
  })
})
