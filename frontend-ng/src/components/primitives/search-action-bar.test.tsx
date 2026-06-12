import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { SearchActionBar } from './search-action-bar'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'search-action-bar.tsx'), 'utf8')

describe('SearchActionBar', () => {
  test('renders a shared search-led toolbar shell with trailing actions', () => {
    const html = renderToStaticMarkup(
      <SearchActionBar
        actions={<button type='button'>Refresh</button>}
        search={<input aria-label='Search users' />}
      />
    )

    expect(html).toContain('data-slot="search-action-bar"')
    expect(html).toContain('data-slot="search-action-bar-search"')
    expect(html).toContain('data-slot="search-action-bar-actions"')
    expect(html).toContain('data-slot="search-action-bar-actions-row"')
    expect(html).toContain('Search users')
    expect(html).toContain('Refresh')
  })

  test('supports stacked secondary filter content under the primary search row', () => {
    const html = renderToStaticMarkup(
      <SearchActionBar
        actions={<button type='button'>Export</button>}
        search={<input aria-label='Search records' />}
      >
        <div>Extra filters</div>
      </SearchActionBar>
    )

    expect(html).toContain('flex-col')
    expect(html).toContain('Extra filters')
    expect(html).toContain('Search records')
  })

  test('uses shared card filter, filter-row, and end-actions primitives for search-plus-actions rhythm', () => {
    expect(source).toContain("from '@/components/primitives/card-filter-bar'")
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/end-actions'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("<CardFilterBar")
    expect(source).toContain("<ActionGroup")
    expect(source).toContain("<FilterRow")
    expect(source).toContain("<EndActions")
    expect(source).not.toContain("className='flex items-center justify-between gap-3'")
  })
})
