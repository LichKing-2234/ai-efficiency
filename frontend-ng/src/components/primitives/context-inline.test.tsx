import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ContextInline, ContextInlineItem } from './context-inline'

describe('ContextInline', () => {
  test('renders compact labeled inline metadata with shared separators', () => {
    const html = renderToStaticMarkup(
      <ContextInline>
        <ContextInlineItem label='Group' value='Alpha' />
        <ContextInlineItem label='Platform' value='claude' />
      </ContextInline>
    )

    expect(html).toContain('data-slot="context-inline"')
    expect(html).toContain('data-slot="context-inline-item"')
    expect(html).toContain('data-slot="context-inline-label"')
    expect(html).toContain('data-slot="context-inline-separator"')
    expect(html).toContain('font-medium text-[11px] uppercase tracking-[0.04em] text-[var(--ink-3)]')
    expect(html).toContain('mono')
    expect(html).toContain('Group')
    expect(html).toContain('Alpha')
    expect(html).toContain('Platform')
    expect(html).toContain('claude')
  })
})
