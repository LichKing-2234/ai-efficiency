import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_CONTROL_GRID_PAGES = [
  'features/admin-users/admin-subscription-form.tsx',
  'features/repos/repo-detail-page.tsx'
]

describe('Control grid composition', () => {
  test('uses ControlGrid instead of feature-local responsive control grid classes', () => {
    const offenders = STANDARDIZED_CONTROL_GRID_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return (
        source.includes("grid gap-3 md:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]") ||
        source.includes("grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto_auto]")
      )
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
