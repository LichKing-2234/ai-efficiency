import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SelectableCard } from './selectable-card'

describe('SelectableCard', () => {
  test('renders selectable card semantics and active state', () => {
    const html = renderToStaticMarkup(
      <SelectableCard active onClick={() => undefined}>
        Provider Alpha
      </SelectableCard>
    )

    expect(html).toContain('type="button"')
    expect(html).toContain('aria-pressed="true"')
    expect(html).toContain('data-active="true"')
    expect(html).toContain('Provider Alpha')
  })
})
