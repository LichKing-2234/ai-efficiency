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
    expect(html).toContain('border-border')
    expect(html).toContain('border-t')
    expect(html).toContain('p-3')
  })

  test('passes layout class names through to the card footer slot', () => {
    const html = renderToStaticMarkup(
      <CardPagerFooter
        className='border-t p-3'
        summary='Page 2 of 5'
        previous={<button type='button'>Previous</button>}
        next={<button type='button'>Next</button>}
      />
    )

    expect(html).toContain('data-slot="card-footer"')
    expect(html).toContain('border-t')
    expect(html).toContain('p-3')
  })
})
