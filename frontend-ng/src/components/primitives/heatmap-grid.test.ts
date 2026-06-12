import { describe, expect, test } from 'vitest'
import { buildHeatmapCells } from './heatmap-grid'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'heatmap-grid.tsx'), 'utf8')

describe('heatmap grid helpers', () => {
  test('builds a fixed seven day by twenty four hour grid', () => {
    const cells = buildHeatmapCells([
      { day: 0, hour: 9, value: 8 },
      { day: 6, hour: 23, value: 4 }
    ])

    expect(cells).toHaveLength(168)
    expect(cells.find((cell) => cell.day === 0 && cell.hour === 9)).toMatchObject({ value: 8, intensity: 1 })
    expect(cells.find((cell) => cell.day === 6 && cell.hour === 23)).toMatchObject({ value: 4, intensity: 0.5 })
  })

  test('ignores out of range points and keeps empty cells at zero intensity', () => {
    const cells = buildHeatmapCells([
      { day: -1, hour: 9, value: 20 },
      { day: 2, hour: 24, value: 20 },
      { day: 3, hour: 12, value: 0 }
    ])

    expect(cells.every((cell) => cell.intensity >= 0 && cell.intensity <= 1)).toBe(true)
    expect(cells.every((cell) => Number.isFinite(cell.intensity))).toBe(true)
    expect(cells.find((cell) => cell.day === 3 && cell.hour === 12)).toMatchObject({ value: 0, intensity: 0 })
  })

  test('uses shared stack and row primitives for legend and outer shell rhythm', () => {
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("<FilterRow className='justify-end text-[11px] text-[var(--ink-3)]' dataSlot='heatmap-grid-legend'>")
    expect(source).toContain("<div className='flex items-center font-medium text-[11px] text-[var(--ink-3)]'>{label}</div>")
    expect(source).not.toContain("<div className={cn('flex flex-col gap-3', className)}>")
    expect(source).not.toContain("<div className='flex items-center justify-end gap-2 text-[11.5px] text-muted-foreground'>")
    expect(source).not.toContain("<span className='flex gap-1'>")
  })
})
