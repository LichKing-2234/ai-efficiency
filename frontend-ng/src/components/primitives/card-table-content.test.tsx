import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CardTableContent } from './card-table-content'

describe('CardTableContent', () => {
  test('renders edge-to-edge table content inside a card', () => {
    const html = renderToStaticMarkup(
      <CardTableContent>
        <span>Cost table</span>
      </CardTableContent>
    )

    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('data-layout="table"')
    expect(html).toContain('px-0')
    expect(html).toContain('pb-0')
    expect(html).toContain('Cost table')
  })

  test('supports fully flush table cards for settings-style data grids', () => {
    const html = renderToStaticMarkup(
      <CardTableContent variant='flush'>
        <span>Settings table</span>
      </CardTableContent>
    )

    expect(html).toContain('data-layout="table"')
    expect(html).toContain('data-variant="flush"')
    expect(html).toContain('p-0')
    expect(html).toContain('Settings table')
  })
})
