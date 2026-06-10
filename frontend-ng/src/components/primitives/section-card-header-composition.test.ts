import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_SECTION_CARD_HEADER_PAGES = [
  'features/home/home-page.tsx',
  'features/repos/repo-detail-page.tsx',
  'features/user-setup/user-page.tsx'
]

describe('SectionCardHeader composition', () => {
  test('keeps title icon and live indicator layout inside the primitive', () => {
    const offenders = STANDARDIZED_SECTION_CARD_HEADER_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("title={<span className='flex items-center gap-2'>")
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
