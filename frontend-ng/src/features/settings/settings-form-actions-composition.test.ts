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
      expect(source).toContain("from '@/components/primitives/submit-cancel-actions'")
      expect(source).toContain('<SubmitCancelActions')
      expect(source).not.toContain('<ActionGroup>')
      expect(source).not.toContain("<Button variant='outline'")
    }
  })
})
