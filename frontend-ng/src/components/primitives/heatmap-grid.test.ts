import { describe, expect, test } from 'vitest'
import { buildHeatmapCells } from './heatmap-grid'

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
})
