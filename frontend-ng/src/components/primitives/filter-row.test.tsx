import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FilterRow } from './filter-row'

describe('FilterRow', () => {
  test('renders a wrapped filter control row with stable slot semantics', () => {
    const html = renderToStaticMarkup(
      <FilterRow>
        <button type='button'>Apply</button>
      </FilterRow>
    )

    expect(html).toContain('data-slot="filter-row"')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('items-center')
    expect(html).toContain('Apply')
  })

  test('supports looser alignment for badge and metadata rows', () => {
    const html = renderToStaticMarkup(
      <FilterRow align='start' className='gap-3'>
        <span>Claude</span>
      </FilterRow>
    )

    expect(html).toContain('items-start')
    expect(html).toContain('gap-3')
    expect(html).toContain('Claude')
  })
})
