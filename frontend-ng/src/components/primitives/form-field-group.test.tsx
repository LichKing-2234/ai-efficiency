import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FormFieldGroup } from './form-field-group'

describe('FormFieldGroup', () => {
  test('renders the shared field-group shell for form stacks', async () => {
    const html = renderToStaticMarkup(
      <FormFieldGroup gap='compact'>
        <div>Alpha</div>
        <div>Beta</div>
      </FormFieldGroup>
    )
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./form-field-group.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/ui/field'")
    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('Alpha')
    expect(html).toContain('Beta')
    expect(html).toContain('gap-3')
  })
})
