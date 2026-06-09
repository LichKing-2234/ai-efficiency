import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CardPagerFooter } from './card-pager-footer'

describe('CardPagerFooter', () => {
  test('renders pagination summary and previous next actions in a card footer', () => {
    const html = renderToStaticMarkup(
      <CardPagerFooter
        summary='Page 1 of 4'
        previous={<button type='button'>Previous</button>}
        next={<button type='button'>Next</button>}
      />
    )

    expect(html).toContain('data-slot="card-footer"')
    expect(html).toContain('data-slot="card-pager-footer"')
    expect(html).toContain('Page 1 of 4')
    expect(html).toContain('Previous')
    expect(html).toContain('Next')
    expect(html).toContain('justify-between')
  })
})
