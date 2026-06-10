import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TextField } from './text-field'

describe('TextField', () => {
  test('renders a labeled input field', () => {
    const html = renderToStaticMarkup(
      <TextField
        id='sample-input'
        label='Sample'
        value='hello'
        onChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field"')
    expect(html).toContain('for="sample-input"')
    expect(html).toContain('id="sample-input"')
    expect(html).toContain('data-slot="input"')
    expect(html).toContain('value="hello"')
  })

  test('supports textarea and input attributes', () => {
    const html = renderToStaticMarkup(
      <TextField
        id='sample-textarea'
        label='Prompt'
        multiline
        placeholder='Write prompt'
        value='hello'
        onChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="textarea"')
    expect(html).toContain('placeholder="Write prompt"')
    expect(html).toContain('hello')
  })

  test('supports semantic field widths', () => {
    const html = renderToStaticMarkup(
      <TextField
        id='sample-datetime'
        label='From'
        type='datetime-local'
        value='2026-06-10T09:00'
        width='datetime'
        onChange={() => undefined}
      />
    )

    expect(html).toContain('w-[220px]')
  })
})
