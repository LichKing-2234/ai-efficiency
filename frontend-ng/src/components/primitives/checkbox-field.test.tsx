import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CheckboxField } from './checkbox-field'

describe('CheckboxField', () => {
  test('renders a labeled horizontal checkbox field', () => {
    const html = renderToStaticMarkup(
      <CheckboxField
        checked
        id='sample-checkbox'
        label='Sample'
        onCheckedChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field"')
    expect(html).toContain('data-orientation="horizontal"')
    expect(html).toContain('id="sample-checkbox"')
    expect(html).toContain('for="sample-checkbox"')
    expect(html).toContain('role="checkbox"')
    expect(html).toContain('aria-checked="true"')
  })
})
