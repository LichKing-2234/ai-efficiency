import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_DATA_GRID_CHECKBOX_PAGES = [
  'features/admin-users/admin-users-page.tsx'
]

describe('Data grid checkbox composition', () => {
  test('uses DataGridCheckbox for table selection cells instead of raw Checkbox', () => {
    const offenders = STANDARDIZED_DATA_GRID_CHECKBOX_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return source.includes("from '@/components/ui/checkbox'")
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
