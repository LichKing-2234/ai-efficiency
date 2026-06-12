import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { SubmitCancelActions } from './submit-cancel-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'submit-cancel-actions.tsx'), 'utf8')

describe('SubmitCancelActions', () => {
  test('renders the standard cancel-submit button pair through shared form actions', () => {
    const html = renderToStaticMarkup(
      <SubmitCancelActions
        cancelLabel='Cancel'
        submitLabel='Create'
        onCancel={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('data-slot="form-actions"')
    expect(html).toContain('Cancel')
    expect(html).toContain('Create')
  })

  test('sources the pair from shared form-actions and button primitives', () => {
    expect(source).toContain("from '@/components/primitives/form-actions'")
    expect(source).toContain("from '@/components/ui/button'")
    expect(source).toContain('<FormActions>')
    expect(source).toContain("<Button variant='outline'")
  })
})
