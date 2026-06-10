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

  test('supports block-end alignment for grid-aligned controls', () => {
    const html = renderToStaticMarkup(
      <CheckboxField
        align='block-end'
        checked={false}
        id='grid-checkbox'
        label='Grid aligned'
        onCheckedChange={() => undefined}
      />
    )

    expect(html).toContain('data-align="block-end"')
    expect(html).toContain('min-h-14')
    expect(html).toContain('items-end')
    expect(html).toContain('pb-1')
  })
})
