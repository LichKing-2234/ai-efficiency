import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_TOOLBAR_SELECT_PAGES = [
  'features/admin-users/admin-users-page.tsx',
  'features/events/events-page.tsx',
  'features/repos/repo-detail-page.tsx',
  'features/repos/repos-page.tsx'
]

describe('Toolbar select composition', () => {
  test('uses ToolbarSelect for list filter and pager selects', () => {
    const offenders = STANDARDIZED_TOOLBAR_SELECT_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("from '@/components/ui/select'")
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
