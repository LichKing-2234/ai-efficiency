import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'home-page.tsx'), 'utf8')

describe('Home page cost section composition', () => {
  test('uses the shared comparison-value primitive for the cost efficiency headline', () => {
    expect(source).toContain("from '@/components/primitives/value-comparison'")
    expect(source).toContain('<ValueComparison current={currency(totalActualCost, locale)} previous={currency(totalStandardCost, locale)} />')
    expect(source).not.toContain("<div className='flex items-baseline gap-3'>")
    expect(source).not.toContain("<div className='tnum text-3xl font-semibold leading-none'>{currency(totalActualCost, locale)}</div>")
    expect(source).not.toContain("<div className='text-[12px] text-[var(--ink-3)] line-through'>{currency(totalStandardCost, locale)}</div>")
  })
})
