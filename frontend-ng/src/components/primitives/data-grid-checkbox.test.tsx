import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { DataGridCheckbox } from './data-grid-checkbox'

describe('DataGridCheckbox', () => {
  test('renders a table selection checkbox with an accessible label', () => {
    const html = renderToStaticMarkup(
      <DataGridCheckbox ariaLabel='Select visible users' checked='indeterminate' onCheckedChange={() => undefined} />
    )

    expect(html).toContain('role="checkbox"')
    expect(html).toContain('aria-label="Select visible users"')
    expect(html).toContain('aria-checked="mixed"')
  })
})
