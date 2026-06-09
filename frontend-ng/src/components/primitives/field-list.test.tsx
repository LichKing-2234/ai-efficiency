import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FieldItem, FieldList } from './field-list'

describe('FieldList', () => {
  test('renders compact label value rows with mono and truncation variants', () => {
    const html = renderToStaticMarkup(
      <FieldList>
        <FieldItem label='Commit' mono value='abcdef1234567890' />
        <FieldItem label='Source' truncate value='src/index.ts' />
      </FieldList>
    )

    expect(html).toContain('data-slot="field-list"')
    expect(html).toContain('data-slot="field-item"')
    expect(html).toContain('Commit')
    expect(html).toContain('abcdef1234567890')
    expect(html).toContain('mono')
    expect(html).toContain('break-all')
    expect(html).toContain('truncate')
  })
})
