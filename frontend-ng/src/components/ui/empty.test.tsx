import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Empty } from './empty'

describe('Empty', () => {
  test('renders compact empty states without page-local padding overrides', () => {
    const html = renderToStaticMarkup(<Empty size='compact'>No matched PRs</Empty>)

    expect(html).toContain('data-size="compact"')
    expect(html).toContain('p-4')
    expect(html).not.toContain(' size="compact"')
    expect(html).toContain('No matched PRs')
  })
})
