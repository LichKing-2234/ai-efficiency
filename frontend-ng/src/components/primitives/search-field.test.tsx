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

  test('supports toolbar sizing for filter bars', () => {
    const html = renderToStaticMarkup(
      <SearchField
        ariaLabel='Search users'
        clearLabel='Clear search'
        onChange={() => undefined}
        onClear={() => undefined}
        placeholder='Search users'
        value=''
        width='toolbar'
      />
    )

    expect(html).toContain('max-w-[320px]')
    expect(html).toContain('flex-1')
  })

  test('uses the tighter reference inset search chrome', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./search-field.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("InputGroup className={cn('h-[34px] min-w-0 rounded-[var(--r-md)] border-[var(--line)] bg-[var(--surface-inset)] px-[1px] shadow-none'")
    expect(source).toContain("width === 'toolbar' && 'max-w-[320px] flex-1'")
    expect(source).toContain("<InputGroupButton aria-label={clearLabel} onClick={onClear} size='icon-xs'>")
    expect(source).not.toContain("InputGroup className={cn('h-9 min-w-0 bg-[var(--surface-inset)]'")
    expect(source).not.toContain("'min-w-64 flex-1 sm:max-w-md'")
  })
})
