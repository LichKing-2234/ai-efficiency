import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SearchWorkbenchCard } from './search-workbench-card'

describe('SearchWorkbenchCard', () => {
  test('renders the shared framed search workbench shell with stable slots', () => {
    const html = renderToStaticMarkup(
      <SearchWorkbenchCard>
        <div>Filters</div>
      </SearchWorkbenchCard>
    )

    expect(html).toContain('data-slot="search-workbench-card"')
    expect(html).toContain('data-slot="framed-card"')
    expect(html).toContain('Filters')
  })
})
