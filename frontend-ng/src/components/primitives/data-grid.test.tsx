import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { DataGrid, DataGridCell, DataGridHeader, DataGridHeaderCell, DataGridPrimaryLink, DataGridRow, DataGridStatusRow } from './data-grid'

describe('DataGrid', () => {
  test('renders reference table shells with shared grid template classes', () => {
    const html = renderToStaticMarkup(
      <DataGrid minWidth={820}>
        <DataGridHeader columns='1fr_120px'>
          <span>Name</span>
          <span>Status</span>
        </DataGridHeader>
        <DataGridRow columns='1fr_120px'>
          <span>Repository</span>
          <span>Active</span>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-scroll"')
    expect(html).toContain('data-slot="data-grid"')
    expect(html).toContain('data-slot="data-grid-header"')
    expect(html).toContain('data-slot="data-grid-row"')
    expect(html).toContain('min-width:820px')
    expect(html).toContain('grid-template-columns:1fr 120px')
    expect(html).toContain('Name')
    expect(html).toContain('Repository')
  })

  test('renders standardized header cells with alignment slots', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridHeader columns='1fr_120px'>
          <DataGridHeaderCell>Name</DataGridHeaderCell>
          <DataGridHeaderCell align='right'>Credits</DataGridHeaderCell>
        </DataGridHeader>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-header-cell"')
    expect(html).toContain('Name')
    expect(html).toContain('Credits')
    expect(html).toContain('text-right')
  })

  test('supports button rows using the existing ae row interaction class', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow as='button' columns='1fr' onClick={() => undefined}>
          <span>Open row</span>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('type="button"')
    expect(html).toContain('ae-trow-btn')
    expect(html).toContain('Open row')
  })

  test('supports full width rows for nested empty states', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow columns='1fr_120px' fullWidth>
          <span>No rows</span>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-row"')
    expect(html).toContain('grid-column:1 / -1')
    expect(html).toContain('No rows')
  })

  test('renders standardized status rows for empty and loading table states', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridStatusRow columns='1fr_120px'>No matching rows</DataGridStatusRow>
        <DataGridStatusRow columns='1fr_120px' tone='loading'>Loading details</DataGridStatusRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-status-row"')
    expect(html).toContain('grid-template-columns:1fr 120px')
    expect(html).toContain('grid-column:1 / -1')
    expect(html).toContain('No matching rows')
    expect(html).toContain('Loading details')
    expect(html).toContain('py-10')
    expect(html).toContain('py-4')
    expect(html).not.toContain('px-6 py-10 text-center text-muted-foreground text-sm')
  })

  test('renders standardized dense data cells for numeric and metadata values', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow columns='1fr_120px'>
          <DataGridCell truncate muted>Platform Team</DataGridCell>
          <DataGridCell align='right' numeric tone='muted'>42</DataGridCell>
          <DataGridCell mono truncate tone='subtle'>main</DataGridCell>
          <DataGridCell mono numeric truncate tone='metadata'>abc123</DataGridCell>
          <DataGridCell align='right' emphasis numeric>$12.40</DataGridCell>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-cell"')
    expect(html).toContain('Platform Team')
    expect(html).toContain('text-right')
    expect(html).toContain('tnum')
    expect(html).toContain('mono')
    expect(html).toContain('truncate')
    expect(html).toContain('font-semibold')
    expect(html).toContain('text-muted-foreground')
    expect(html).toContain('text-[var(--ink-2)]')
  })

  test('renders standardized primary and description cell content', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow columns='1fr'>
          <DataGridCell description='alice@example.com' truncate>Alice</DataGridCell>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-cell"')
    expect(html).toContain('data-slot="data-grid-cell-primary"')
    expect(html).toContain('data-slot="data-grid-cell-description"')
    expect(html).toContain('Alice')
    expect(html).toContain('alice@example.com')
    expect(html).toContain('font-semibold')
    expect(html).toContain('text-muted-foreground')
    expect(html).not.toContain('block truncate text-muted-foreground text-xs')
  })

  test('renders standardized primary record links', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow columns='1fr'>
          <DataGridPrimaryLink asChild>
            <a href='/repos/42'>Platform Repository</a>
          </DataGridPrimaryLink>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-primary-link"')
    expect(html).toContain('href="/repos/42"')
    expect(html).toContain('Platform Repository')
    expect(html).toContain('block')
    expect(html).toContain('truncate')
    expect(html).toContain('font-semibold')
    expect(html).toContain('hover:text-[var(--ai-deep)]')
  })
})
