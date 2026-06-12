import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { CompareBar } from './compare-bar'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'compare-bar.tsx'), 'utf8')

describe('CompareBar', () => {
  test('renders label, value, and proportional fill with shared compact layout', () => {
    const html = renderToStaticMarkup(
      <CompareBar color='var(--ai)' label='Actual spend' max={100} value={42} valueLabel='$42.00' />
    )

    expect(html).toContain('Actual spend')
    expect(html).toContain('$42.00')
    expect(html).toContain('bg-[var(--surface-inset)]')
    expect(html).toContain('width:42%')
    expect(html).toContain('h-[10px]')
  })

  test('keeps the comparison bar layout in a reusable primitive', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("from '@/lib/utils'")
    expect(source).toContain("gap='compact'")
    expect(source).toContain("className='flex items-center justify-between gap-3 text-[12px]'")
    expect(source).toContain("className={cn('font-medium text-[var(--ink-2)]', labelClassName)}")
    expect(source).toContain("className='tnum font-semibold text-[12px] text-[var(--ink)]'")
    expect(source).toContain("className='h-[10px] overflow-hidden rounded-[var(--r-full)] bg-[var(--surface-inset)]'")
    expect(source).toContain('export function CompareBar(')
    expect(source).toContain("transition-[width] duration-700 ease-[var(--ease-out)]")
  })
})
