import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { buildSparklinePath, buildStackedAreaLayers, type StackedAreaKey, type StackedAreaPoint } from './charts'
import { BarsH } from './charts'

describe('buildSparklinePath', () => {
  test('builds a stable sparkline for flat data without NaN coordinates', () => {
    const path = buildSparklinePath([5, 5, 5], 90, 30)

    expect(path.line).toBe('M0.0 27.0 L45.0 27.0 L90.0 27.0')
    expect(path.area).not.toContain('NaN')
    expect(path.last).toEqual([90, 27])
  })
})

describe('buildStackedAreaLayers', () => {
  test('stacks token series in key order and returns one layer per key', () => {
    type Point = StackedAreaPoint & { date: string; input: number; output: number }
    const keys: Array<StackedAreaKey<Point>> = [
      { key: 'input', label: 'Input', color: 'var(--viz-input)' },
      { key: 'output', label: 'Output', color: 'var(--viz-output)' }
    ]
    const result = buildStackedAreaLayers({
      series: [
        { date: '2026-06-01', input: 10, output: 5 },
        { date: '2026-06-02', input: 20, output: 10 }
      ],
      keys,
      width: 120,
      height: 80,
      pad: { left: 10, right: 10, top: 10, bottom: 10 }
    })

    expect(result.layers).toHaveLength(2)
    expect(result.layers.map((layer) => layer.key)).toEqual(['input', 'output'])
    expect(result.totals).toEqual([15, 30])
    expect(result.layers[0].area).toContain('Z')
    expect(result.layers[1].path).toContain('L110.0')
  })
})

describe('StackedAreaChart composition', () => {
  test('keeps tooltip row rhythm inside the chart tooltip row slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./charts.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from './action-group'")
    expect(source).toContain("className='mt-1 text-[11.5px]'")
    expect(source).not.toContain("className='mt-1 flex items-center gap-2 text-xs'")
    expect(source).not.toContain("const stackedAreaTooltipRowClass = 'mt-1 flex items-center gap-2 text-xs'")
    expect(source).toContain('<ActionGroup')
  })

  test('keeps horizontal bar layout on shared stack and action primitives', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./charts.tsx', import.meta.url), 'utf8')
    )

    expect(source).not.toContain("className={cn('flex flex-col gap-3.5', className)}")
    expect(source).not.toContain("className='mb-1.5 flex items-baseline justify-between gap-3'")
    expect(source).not.toContain("className='flex min-w-0 items-center gap-2 font-medium text-[12.5px]'")
  })

  test('keeps stacked-area tooltip card density on explicit token sizing', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./charts.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className='pointer-events-none absolute top-2 min-w-40 rounded-[var(--r-sm)] border border-[var(--line-strong)] bg-[var(--surface)] p-[14px]'")
    expect(source).not.toContain("className='pointer-events-none absolute top-2 min-w-40 rounded-[var(--r-sm)] border border-[var(--line-strong)] bg-[var(--surface)] p-3 shadow-[var(--sh-lg)]'")
  })

  test('keeps ring container layout on shared stack primitives', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./charts.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from './stack'")
    expect(source).toContain('<Stack')
    expect(source).toContain("dataSlot='ring'")
    expect(source).not.toContain("className={cn('relative grid place-items-center', className)}")
    expect(source).not.toContain("className='absolute inset-0 grid place-items-center'")
  })

  test('uses explicit share ratios for horizontal bar widths when share data is available', () => {
    const html = renderToStaticMarkup(
      createElement(BarsH, {
        rows: [
          { label: 'Model A', value: 120, share: 0.25, color: 'var(--viz-input)' },
          { label: 'Model B', value: 240, share: 0.5, color: 'var(--viz-output)' }
        ]
      })
    )

    expect(html).toContain('width:25%')
    expect(html).toContain('width:50%')
    expect(html).not.toContain('width:100%')
  })
})
