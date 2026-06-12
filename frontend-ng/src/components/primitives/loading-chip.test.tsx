import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { LoadingChip } from './loading-chip'

describe('LoadingChip', () => {
  test('renders a shared inline loading skeleton shell', () => {
    const html = renderToStaticMarkup(
      <LoadingChip ariaLabel='Loading' />
    )

    expect(html).toContain('data-slot="loading-chip"')
    expect(html).toContain('aria-label="Loading"')
    expect(html).toContain('role="status"')
  })
})
