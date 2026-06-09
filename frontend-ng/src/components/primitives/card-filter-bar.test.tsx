import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CardFilterBar } from './card-filter-bar'

describe('CardFilterBar', () => {
  test('renders a compact wrapped card filter toolbar by default', () => {
    const html = renderToStaticMarkup(
      <CardFilterBar>
        <button type='button'>Apply</button>
      </CardFilterBar>
    )

    expect(html).toContain('data-slot="card-filter-bar"')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('border-b')
    expect(html).toContain('Apply')
  })

  test('supports stacked filter rows for denser filter cards', () => {
    const html = renderToStaticMarkup(
      <CardFilterBar stacked>
        <div>Row one</div>
        <div>Row two</div>
      </CardFilterBar>
    )

    expect(html).toContain('flex-col')
    expect(html).toContain('Row one')
    expect(html).toContain('Row two')
  })
})
