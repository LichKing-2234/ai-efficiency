import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { UsageActivityRow } from './usage-activity-row'

describe('UsageActivityRow', () => {
  test('renders compact usage metadata with optional first-row spacing', () => {
    const html = renderToStaticMarkup(
      <UsageActivityRow
        bound
        credit='12'
        endedAt='2026-06-09 10:30'
        first
        requests='3 req'
        statusLabel='Bound'
        title='org/repo'
        tokens='4.2K tok'
        tool='codex'
      />
    )

    expect(html).toContain('data-slot="usage-activity-row"')
    expect(html).toContain('data-state="bound"')
    expect(html).toContain('org/repo')
    expect(html).toContain('2026-06-09 10:30')
    expect(html).toContain('4.2K tok')
    expect(html).toContain('Bound')
    expect(html).toContain('12')
    expect(html).toContain('3 req')
    expect(html).not.toContain('border-t')
  })

  test('renders unbound rows with a separator after the first item', () => {
    const html = renderToStaticMarkup(
      <UsageActivityRow
        bound={false}
        credit='0'
        endedAt='2026-06-09 10:31'
        requests='1 req'
        statusLabel='Needs binding'
        title='session.jsonl'
        tokens='900 tok'
        tool='claude'
      />
    )

    expect(html).toContain('data-state="unbound"')
    expect(html).toContain('Needs binding')
    expect(html).toContain('border-t')
  })
})
