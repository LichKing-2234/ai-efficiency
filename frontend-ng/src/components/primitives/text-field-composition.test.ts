import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const SETTINGS_TEXT_FORMS = [
  'features/events/events-page.tsx',
  'features/oauth/oauth-pages.tsx',
  'features/repos/repo-create-form.tsx',
  'features/settings/ldap-settings-form.tsx',
  'features/settings/relay-provider-form.tsx'
]

describe('Text field composition', () => {
  test('uses TextField in standardized text-control surfaces instead of raw text controls', () => {
    const offenders = SETTINGS_TEXT_FORMS.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("from '@/components/ui/input'") ||
        source.includes("from '@/components/ui/textarea'") ||
        source.includes('FieldLabel')
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
