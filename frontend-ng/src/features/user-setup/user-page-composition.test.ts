import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-page.tsx'), 'utf8')

describe('User setup page composition', () => {
  test('uses shared action grouping for access group selectors', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup wrap className='justify-start sm:justify-end'>")
    expect(source).not.toContain('actions={(selectedProvider?.groups ?? []).map((group) => (')
  })

  test('uses shared card content stacks for setup card bodies', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-2'>")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-4'>")
  })
})
