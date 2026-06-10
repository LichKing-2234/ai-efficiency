import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_ACTION_GROUP_PAGES = [
  'features/admin-users/admin-users-page.tsx'
]

describe('Action group composition', () => {
  test('uses ActionGroup for row and toolbar action clusters', () => {
    const offenders = STANDARDIZED_ACTION_GROUP_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("className='flex min-w-0 flex-wrap justify-end gap-2'")
        || source.includes("className='ml-auto flex items-center gap-2 text-sm'")
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
