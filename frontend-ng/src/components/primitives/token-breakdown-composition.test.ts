import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'token-breakdown.tsx'), 'utf8')

describe('TokenBreakdown composition', () => {
  test('uses Stack for legend vertical rhythm', () => {
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("<Stack gap='compact'>")
    expect(source).not.toContain("<div className='flex flex-col gap-2'>")
  })
})
