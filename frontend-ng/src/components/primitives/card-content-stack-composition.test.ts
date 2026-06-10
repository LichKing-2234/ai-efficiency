import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_CARD_CONTENT_STACK_PAGES = [
  'features/admin-users/admin-users-page.tsx',
  'features/home/home-page.tsx',
  'features/repos/repo-detail-page.tsx',
  'features/settings/settings-page.tsx'
]

describe('Card content stack composition', () => {
  test('uses CardContentStack for standardized stacked card bodies', () => {
    const offenders = STANDARDIZED_CARD_CONTENT_STACK_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("<CardContent className='flex flex-col gap-3'>")
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
