import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { FormActions } from './form-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'form-actions.tsx'), 'utf8')

describe('FormActions', () => {
  test('renders a shared end-aligned action row for form commands', () => {
    const html = renderToStaticMarkup(
      <FormActions>
        <button type='button'>Cancel</button>
        <button type='submit'>Create</button>
      </FormActions>
    )

    expect(html).toContain('data-slot="form-actions"')
    expect(html).toContain('Cancel')
    expect(html).toContain('Create')
  })

  test('sources the action row from the shared action-group primitive', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align={align} dataSlot='form-actions' wrap={wrap}>")
    expect(source).toContain('export function FormActions(')
  })
})
