import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SearchField } from './search-field'

describe('SearchField', () => {
  test('renders a search textbox with the current value and clear affordance', () => {
    const html = renderToStaticMarkup(
      <SearchField
        ariaLabel='Search records'
        clearLabel='Clear search'
        onChange={() => undefined}
        onClear={() => undefined}
        placeholder='Search by repository'
        value='codex'
      />
    )

    expect(html).toContain('type="search"')
    expect(html).toContain('aria-label="Search records"')
    expect(html).toContain('placeholder="Search by repository"')
    expect(html).toContain('value="codex"')
    expect(html).toContain('aria-label="Clear search"')
  })

  test('omits the clear button when the search value is empty', () => {
    const html = renderToStaticMarkup(
      <SearchField
        ariaLabel='Search records'
        clearLabel='Clear search'
        onChange={() => undefined}
        onClear={() => undefined}
        placeholder='Search by repository'
        value=''
      />
    )

    expect(html).not.toContain('aria-label="Clear search"')
  })
})
