import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

describe('Settings page composition', () => {
  test('keeps raw form controls out of the page shell', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).not.toContain("from '@/components/ui/input'")
    expect(source).not.toContain("from '@/components/ui/textarea'")
    expect(source).not.toContain("from '@/components/ui/checkbox'")
    expect(source).not.toContain("from '@/components/ui/select'")
    expect(source).not.toContain('FieldLabel')
  })

  test('uses shared action groups for deployment runtime action rows', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('<ActionGroup wrap className=\'justify-start\'>')
    expect(source).not.toContain("<div className='flex gap-2'>")
  })
})
