import { readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

const STANDARDIZED_STACK_PAGES = [
  'features/user-setup/user-page.tsx',
  'features/user-usage/user-usage-panel.tsx'
]

describe('Stack composition', () => {
  test('uses Stack for standardized page and panel vertical rhythm', () => {
    const offenders = STANDARDIZED_STACK_PAGES.filter((file) => {
      const source = readFileSync(join(ROOT, file), 'utf8')

      return (
        source.includes("<div className='flex flex-col gap-4'>") ||
        source.includes("<div className='stagger flex flex-col gap-4'>")
      )
    }).map((file) => relative(ROOT, join(ROOT, file)))

    expect(offenders).toEqual([])
  })
})
