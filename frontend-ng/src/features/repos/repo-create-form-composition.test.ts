import { readFileSync } from 'node:fs'
import { describe, expect, test } from 'vitest'

const source = readFileSync(new URL('./repo-create-form.tsx', import.meta.url), 'utf8')

describe('RepoCreateForm composition', () => {
  test('uses shared submit-cancel actions instead of a page-local button pair', () => {
    expect(source).toContain("from '@/components/primitives/submit-cancel-actions'")
    expect(source).toContain('<SubmitCancelActions')
    expect(source).not.toContain("<Button variant='outline'")
    expect(source).not.toContain('<ActionGroup>')
  })

  test('uses the shared inset field-list shell for parsed repository metadata', () => {
    expect(source).toContain("from '@/components/primitives/inset-field-list'")
    expect(source).toContain('<InsetFieldList>')
    expect(source).not.toContain('<InsetPanel stack>')
    expect(source).not.toContain('<FieldList>')
  })
})
