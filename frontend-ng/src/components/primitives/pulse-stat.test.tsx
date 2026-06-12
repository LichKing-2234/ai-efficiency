import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { PulseStat } from './pulse-stat'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'pulse-stat.tsx'), 'utf8')

describe('PulseStat', () => {
  test('renders compact overview pulse rows with shared spark bars', () => {
    const html = renderToStaticMarkup(
      <PulseStat color='var(--ai)' label='Spend today' value='$142.80' values={[2, 3, 5, 8]} />
    )

    expect(html).toContain('Spend today')
    expect(html).toContain('$142.80')
    expect(html).toContain('text-[11px]')
    expect(html).toContain('text-[19px]')
    expect(html).toContain('<svg')
    expect(html).toContain('min-w-[112px] flex-1')
  })

  test('keeps reference pulse density in the shared primitive', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("from '@/components/primitives/charts'")
    expect(source).toContain("from '@/lib/utils'")
    expect(source).toContain("className='px-[16px] py-[12px]'")
    expect(source).toContain("className='tnum mb-[6px] text-[19px] font-[680] tracking-[-0.02em]'")
    expect(source).toContain("mb-[6px]")
    expect(source).toContain('export function PulseStat(')
  })
})
