import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ValueComparison } from './value-comparison'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'value-comparison.tsx'), 'utf8')

describe('ValueComparison', () => {
  test('renders current and comparison values in a shared compact row', () => {
    const html = renderToStaticMarkup(<ValueComparison current='$12.34' previous='$56.78' />)

    expect(html).toContain('$12.34')
    expect(html).toContain('$56.78')
    expect(html).toContain('data-slot="value-comparison"')
    expect(html).toContain('line-through')
  })

  test('keeps comparison typography in a reusable primitive', () => {
    expect(source).toContain("data-slot='value-comparison'")
    expect(source).toContain("className='flex items-baseline gap-3'")
    expect(source).toContain("className='tnum text-[30px] font-[680] leading-none tracking-[-0.02em]'")
    expect(source).toContain("className='text-[12px] text-[var(--ink-3)] line-through'")
    expect(source).toContain('export function ValueComparison(')
  })
})
