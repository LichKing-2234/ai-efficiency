import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { OptionList } from './option-list'

describe('OptionList', () => {
  test('renders selectable option rows with primary and secondary labels', () => {
    const html = renderToStaticMarkup(
      <OptionList
        ariaLabel='User search results'
        items={[
          { id: '1', label: 'alice@example.com', description: 'admin · 12' },
          { id: '2', label: 'bob@example.org' }
        ]}
        onSelect={() => undefined}
      />
    )

    expect(html).toContain('data-slot="option-list"')
    expect(html).toContain('aria-label="User search results"')
    expect(html).toContain('alice@example.com')
    expect(html).toContain('admin · 12')
    expect(html).toContain('bob@example.org')
  })
})
