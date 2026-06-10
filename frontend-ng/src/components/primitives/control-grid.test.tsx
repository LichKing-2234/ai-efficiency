import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ControlGrid } from './control-grid'

describe('ControlGrid', () => {
  test('renders dense subscription controls with shared responsive columns', () => {
    const html = renderToStaticMarkup(
      <ControlGrid variant='subscription'>
        <span>Scope</span>
        <span>Operation</span>
      </ControlGrid>
    )

    expect(html).toContain('data-slot="control-grid"')
    expect(html).toContain('grid')
    expect(html).toContain('gap-3')
    expect(html).toContain('md:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]')
    expect(html).toContain('Scope')
  })

  test('renders inline action controls with a fluid first column', () => {
    const html = renderToStaticMarkup(
      <ControlGrid variant='inline-actions'>
        <span>Provider</span>
        <button type='button'>Save</button>
      </ControlGrid>
    )

    expect(html).toContain('lg:grid-cols-[minmax(0,1fr)_auto_auto]')
    expect(html).toContain('Provider')
  })
})
