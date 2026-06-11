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

  test('uses Stack for the top-level vertical shell', () => {
    expect(source).toContain("<Stack className={className} dataSlot='token-breakdown' gap='compact'>")
    expect(source).not.toContain("<div className={cn('flex flex-col gap-3', className)} data-slot='token-breakdown'>")
  })

  test('uses shared row primitives for legend rows and bar shell', () => {
    expect(source).toContain("from './action-group'")
    expect(source).not.toContain("<div className='flex h-2.5 overflow-hidden rounded-full bg-[var(--surface-inset)]' data-slot='token-breakdown-bar'>")
    expect(source).not.toContain("<div className='flex items-center gap-2 text-[12.5px]' data-slot='token-breakdown-row'")
  })
})
