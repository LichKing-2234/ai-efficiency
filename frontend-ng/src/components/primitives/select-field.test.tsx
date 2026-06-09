import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SelectField } from './select-field'

describe('SelectField', () => {
  test('renders a labeled select with grouped items', () => {
    const html = renderToStaticMarkup(
      <SelectField
        id='sample-select'
        label='Sample'
        options={[
          { label: 'Alpha', value: 'alpha' },
          { label: 'Beta', value: 'beta' }
        ]}
        value='alpha'
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field"')
    expect(html).toContain('for="sample-select"')
    expect(html).toContain('id="sample-select"')
    expect(html).toContain('data-slot="select-trigger"')
    expect(html).toContain('aria-label="Alpha"')
  })

  test('supports placeholder and disabled options', () => {
    const html = renderToStaticMarkup(
      <SelectField
        id='disabled-select'
        label='Disabled'
        options={[{ disabled: true, label: 'Unavailable', value: 'none' }]}
        placeholder='Choose'
        value='none'
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('aria-label="Unavailable"')
  })
})
