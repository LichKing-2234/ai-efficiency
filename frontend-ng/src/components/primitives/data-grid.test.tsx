import { renderToStaticMarkup } from 'react-dom/server'
import { ChevronRightIcon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { DataGrid, DataGridCell, DataGridHeader, DataGridHeaderCell, DataGridIdentityCell, DataGridPrimaryLink, DataGridRecordCell, DataGridRow, DataGridRowAffordance, DataGridStatusRow } from './data-grid'

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
    expect(html).toContain('text-[12.5px]')
    expect(html).toContain('text-[var(--ink-3)]')
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
    expect(html).toContain('text-[var(--ink-3)]')
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
    expect(html).toContain('text-[var(--ink-3)]')
    expect(html).toContain('text-[13px]')
    expect(html).toContain('text-[11px]')
    expect(html).not.toContain('block truncate text-muted-foreground text-xs')
  })

  test('renders standardized identity cells with avatar and description slots', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow columns='1fr'>
          <DataGridIdentityCell description='alice@example.com' value='Alice'>Alice</DataGridIdentityCell>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-identity-cell"')
    expect(html).toContain('data-slot="identity-avatar"')
    expect(html).toContain('data-slot="data-grid-cell-primary"')
    expect(html).toContain('Alice')
    expect(html).toContain('alice@example.com')
    expect(html).toContain('justify-start')
    expect(html).toContain('min-w-0')
    expect(html).toContain('gap-2')
  })

  test('sources identity row layout from shared action-group composition', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./data-grid.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain('<ActionGroup')
    expect(source).toContain("dataSlot='data-grid-identity-cell'")
    expect(source).not.toContain("className={cn('flex min-w-0 items-center gap-3', className)}")
  })

  test('keeps data-grid source on explicit ink tokens for status and metadata copy', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./data-grid.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("'justify-center text-center text-[12.5px] text-[var(--ink-3)]'")
    expect(source).toContain("'block text-[11px] text-[var(--ink-3)]'")
    expect(source).toContain("muted && 'text-[11px] text-[var(--ink-3)]'")
    expect(source).toContain("tone === 'metadata' && 'text-[11px] text-[var(--ink-3)]'")
    expect(source).toContain("tone === 'subtle' && 'text-[11.5px] text-[var(--ink-3)]'")
    expect(source).not.toContain("tone === 'metadata' && 'text-muted-foreground text-xs'")
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
    expect(html).toContain('text-[13px]')
    expect(html).toContain('hover:text-[var(--ai-deep)]')
  })

  test('renders standardized record cells with mono metadata', () => {
    const html = renderToStaticMarkup(
      <DataGrid>
        <DataGridRow columns='1fr'>
          <DataGridRecordCell description='https://example.com/platform/repo.git'>
            <DataGridPrimaryLink href='/repos/42'>Platform Repository</DataGridPrimaryLink>
          </DataGridRecordCell>
        </DataGridRow>
      </DataGrid>
    )

    expect(html).toContain('data-slot="data-grid-record-cell"')
    expect(html).toContain('data-slot="data-grid-primary-link"')
    expect(html).toContain('data-slot="record-meta"')
    expect(html).toContain('Platform Repository')
    expect(html).toContain('https://example.com/platform/repo.git')
    expect(html).toContain('min-w-0')
    expect(html).toContain('mono')
    expect(html).toContain('truncate')
  })

  test('renders standardized row affordances for inspectable rows', () => {
    const defaultHtml = renderToStaticMarkup(
      <DataGridRowAffordance>
        <ChevronRightIcon />
      </DataGridRowAffordance>
    )
    const mutedHtml = renderToStaticMarkup(
      <DataGridRowAffordance tone='muted'>
        <ChevronRightIcon />
      </DataGridRowAffordance>
    )

    expect(defaultHtml).toContain('data-slot="data-grid-row-affordance"')
    expect(defaultHtml).toContain('size-8')
    expect(defaultHtml).toContain('items-center')
    expect(defaultHtml).toContain('justify-center')
    expect(defaultHtml).toContain('justify-self-end')
    expect(defaultHtml).toContain('text-[var(--ink-4)]')
    expect(defaultHtml).toContain('size-4')
    expect(mutedHtml).toContain('text-[var(--ink-3)]')
  })
})
