import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { StatusWithReason } from './status-with-reason'

describe('StatusWithReason', () => {
  test('renders a status badge with optional truncated reason copy', () => {
    const html = renderToStaticMarkup(
      <StatusWithReason
        reason='Refresh failed because the source usage window is not ready yet'
        reasonClassName='max-w-48'
        value='refresh_failed'
      />
    )

    expect(html).toContain('data-slot="status-with-reason"')
    expect(html).toContain('refresh failed')
    expect(html).toContain('Refresh failed because')
    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-1')
    expect(html).toContain('max-w-48')
    expect(html).toContain('truncate')
  })

  test('omits empty reason copy without rendering an empty row', () => {
    const html = renderToStaticMarkup(<StatusWithReason value='completed' />)

    expect(html).toContain('completed')
    expect(html).not.toContain('data-slot="status-with-reason-copy"')
  })

  test('renders compact metadata next to the status badge', () => {
    const html = renderToStaticMarkup(<StatusWithReason inline meta='3/10' metaNumeric value='running' />)

    expect(html).toContain('data-slot="status-with-reason-meta"')
    expect(html).toContain('3/10')
    expect(html).toContain('tnum')
    expect(html).toContain('flex-row')
  })
})
