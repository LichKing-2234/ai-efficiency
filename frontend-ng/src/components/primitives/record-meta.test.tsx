import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { RecordMeta } from './record-meta'

describe('RecordMeta', () => {
  test('renders dense monospace metadata with a stable slot', () => {
    const html = renderToStaticMarkup(<RecordMeta>git@example/repo</RecordMeta>)

    expect(html).toContain('data-slot="record-meta"')
    expect(html).toContain('mono')
    expect(html).toContain('truncate')
    expect(html).toContain('git@example/repo')
  })

  test('supports wrapping long metadata values', () => {
    const html = renderToStaticMarkup(<RecordMeta wrap>https://example.com/api</RecordMeta>)

    expect(html).toContain('break-all')
    expect(html).not.toContain('truncate')
  })
})
