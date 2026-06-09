import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-usage-panel.tsx'), 'utf8')

describe('User usage panel composition', () => {
  test('uses shared row primitives for usage range and refresh controls', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain('<FilterRow')
    expect(source).toContain('<ActionGroup')
    expect(source).not.toContain("<div className='flex flex-wrap items-center justify-between gap-3'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
  })
})
