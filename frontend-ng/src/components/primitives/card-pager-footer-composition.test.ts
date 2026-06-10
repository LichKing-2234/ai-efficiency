import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_CARD_PAGER_FOOTER_PAGES = [
  'features/admin-users/admin-users-page.tsx',
  'features/events/events-page.tsx',
  'features/repos/repo-detail-page.tsx',
  'features/repos/repos-page.tsx'
]

describe('Card pager footer composition', () => {
  test('keeps card pager border and padding styles inside the primitive', () => {
    const offenders = STANDARDIZED_CARD_PAGER_FOOTER_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("className='border-border border-t p-3'")
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
