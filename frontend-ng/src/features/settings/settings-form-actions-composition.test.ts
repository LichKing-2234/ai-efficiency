import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const root = dirname(fileURLToPath(import.meta.url))
const relaySource = readFileSync(resolve(root, 'relay-provider-form.tsx'), 'utf8')
const scmSource = readFileSync(resolve(root, 'scm-provider-form.tsx'), 'utf8')
const credentialSource = readFileSync(resolve(root, 'credential-form.tsx'), 'utf8')

describe('Settings form action composition', () => {
  test('uses shared submit-cancel actions instead of page-local button pairs for CRUD dialogs', () => {
    for (const source of [relaySource, scmSource, credentialSource]) {
      expect(source).toContain("from '@/components/primitives/managed-form-footer'")
      expect(source).toContain('<ManagedFormFooter')
      expect(source).not.toContain('<ActionGroup>')
      expect(source).not.toContain("<Button variant='outline'")
    }
  })

  test('routes CRUD form errors through the shared managed footer instead of local alert loops', () => {
    for (const source of [relaySource, scmSource, credentialSource]) {
      expect(source).not.toContain("errors.filter((message): message is string => !!message).map((message) => (")
      expect(source).not.toContain("<AppAlert key={message} tone='error' title={message} />")
    }
  })

  test('uses shared segmented field wrappers for CRUD segmented choices instead of local field-label shells', () => {
    for (const source of [scmSource, credentialSource]) {
      expect(source).toContain("from '@/components/primitives/segmented-field'")
      expect(source).toContain('<SegmentedField')
      expect(source).not.toContain('FieldLabel')
      expect(source).not.toContain("from '@/components/primitives/labeled-segmented-control'")
    }
  })
})
