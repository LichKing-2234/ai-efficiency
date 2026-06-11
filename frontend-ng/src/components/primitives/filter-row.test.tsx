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
      <FilterRow align='start' gap='lg'>
        <span>Claude</span>
      </FilterRow>
    )

    expect(html).toContain('items-start')
    expect(html).toContain('gap-3')
    expect(html).toContain('Claude')
  })

  test('supports semantic spacing and distribution for toolbar headers', () => {
    const html = renderToStaticMarkup(
      <FilterRow justify='between' gap='lg'>
        <span>Usage</span>
        <button type='button'>Refresh</button>
      </FilterRow>
    )

    expect(html).toContain('justify-between')
    expect(html).toContain('gap-3')
  })

  test('supports compact label typography for dense controls', () => {
    const html = renderToStaticMarkup(
      <FilterRow tone='label'>
        <span>Merged in</span>
      </FilterRow>
    )

    expect(html).toContain('text-[12px]')
    expect(html).toContain('text-[var(--ink-3)]')
    expect(html).toContain('Merged in')
  })

  test('forwards semantic slot and state attributes to the rendered row', () => {
    const html = renderToStaticMarkup(
      <FilterRow data-slot='checklist-row' data-state='ready'>
        <span>Ready</span>
      </FilterRow>
    )

    expect(html).toContain('data-slot="checklist-row"')
    expect(html).toContain('data-state="ready"')
    expect(html).not.toContain('data-slot="filter-row"')
  })

  test('keeps label tone on explicit ink token typography instead of generic text-sm', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./filter-row.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("tone === 'label' ? 'text-[12px] text-[var(--ink-3)]' : undefined")
    expect(source).not.toContain("tone === 'label' ? 'text-sm' : undefined")
  })
})
