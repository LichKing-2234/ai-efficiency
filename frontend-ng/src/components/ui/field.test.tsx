import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FieldGroup } from './field'

describe('FieldGroup', () => {
  test('supports compact form rhythm without page-local gap classes', () => {
    const html = renderToStaticMarkup(
      <FieldGroup gap='compact'>
        <div>Repository URL</div>
      </FieldGroup>
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('gap-3')
    expect(html).not.toContain('gap-5')
  })
})
