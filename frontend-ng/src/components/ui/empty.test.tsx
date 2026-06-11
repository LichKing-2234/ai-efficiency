import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from './empty'

describe('Empty', () => {
  test('renders compact empty states without page-local padding overrides', () => {
    const html = renderToStaticMarkup(<Empty size='compact'>No matched PRs</Empty>)

    expect(html).toContain('data-size="compact"')
    expect(html).toContain('p-[14px]')
    expect(html).not.toContain(' size="compact"')
    expect(html).toContain('No matched PRs')
  })

  test('keeps empty states on the reference surface and typography hierarchy', () => {
    const html = renderToStaticMarkup(
      <Empty>
        <EmptyHeader>
          <EmptyTitle>No records</EmptyTitle>
          <EmptyDescription>Try another filter.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )

    expect(html).toContain('rounded-[var(--r-lg)]')
    expect(html).toContain('border-dashed')
    expect(html).toContain('bg-[var(--surface-inset)]')
    expect(html).toContain('p-[18px]')
    expect(html).toContain('text-[14px]')
    expect(html).toContain('text-[12px]')
    expect(html).toContain('text-[var(--ink-3)]')
  })
})
