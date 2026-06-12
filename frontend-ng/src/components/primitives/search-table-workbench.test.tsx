import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { SearchTableWorkbench } from './search-table-workbench'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'search-table-workbench.tsx'), 'utf8')

describe('SearchTableWorkbench', () => {
  test('renders the shared search card and framed table shell together', () => {
    const html = renderToStaticMarkup(
      <SearchTableWorkbench
        actions={<button type='button'>Refresh</button>}
        footer={<div>Pager</div>}
        search={<div>Search field</div>}
        searchChildren={<div>Filters</div>}
      >
        <div>Rows</div>
      </SearchTableWorkbench>
    )

    expect(html).toContain('data-slot="search-workbench-card"')
    expect(html).toContain('data-slot="framed-table-card"')
    expect(html).toContain('data-slot="search-action-bar"')
    expect(html).toContain('Search field')
    expect(html).toContain('Filters')
    expect(html).toContain('Rows')
    expect(html).toContain('Pager')
  })

  test('composes the shared workbench primitives instead of page-local shells', () => {
    expect(source).toContain("from '@/components/primitives/framed-table-card'")
    expect(source).toContain("from '@/components/primitives/search-action-bar'")
    expect(source).toContain("from '@/components/primitives/search-workbench-card'")
    expect(source).toContain('<SearchWorkbenchCard>')
    expect(source).toContain('<SearchActionBar actions={actions} search={search}>')
    expect(source).toContain('<FramedTableCard footer={footer}>')
  })
})
